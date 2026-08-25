package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestAdmissionApprovalProvisionsStudentAndMailbox(t *testing.T) {
	store := NewStoreWithAdminAndDomain(testAdminUsername, testAdminPassword, "students.cgu.edu.kg")
	server := httptest.NewServer(NewServer(store, "web"))
	defer server.Close()

	publicClient := &http.Client{}
	created := postJSON(t, publicClient, server.URL+"/api/admissions", map[string]string{
		"name": "至冬申请人", "email": "applicant@example.com", "school": "至冬与极地研究学院",
	})
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("public admission status = %d", created.StatusCode)
	}
	var createdPayload struct {
		Application AdmissionApplication `json:"application"`
	}
	if err := json.NewDecoder(created.Body).Decode(&createdPayload); err != nil {
		t.Fatal(err)
	}
	created.Body.Close()
	if createdPayload.Application.ID == "" || createdPayload.Application.Status != "pending" {
		t.Fatalf("unexpected public application: %#v", createdPayload.Application)
	}

	adminJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	adminClient := &http.Client{Jar: adminJar}
	login := postJSON(t, adminClient, server.URL+"/api/auth/login", map[string]string{
		"username": testAdminUsername, "password": testAdminPassword,
	})
	if login.StatusCode != http.StatusOK {
		t.Fatalf("admin login status = %d", login.StatusCode)
	}
	login.Body.Close()

	approval := postJSON(t, adminClient, server.URL+"/api/admin/admissions/"+createdPayload.Application.ID+"/approve", map[string]any{})
	if approval.StatusCode != http.StatusCreated {
		t.Fatalf("approval status = %d", approval.StatusCode)
	}
	var approvalPayload struct {
		Application     AdmissionApplication `json:"application"`
		Student         AdminStudent         `json:"student"`
		InitialPassword string               `json:"initialPassword"`
		AlreadyApproved bool                 `json:"alreadyApproved"`
	}
	if err := json.NewDecoder(approval.Body).Decode(&approvalPayload); err != nil {
		t.Fatal(err)
	}
	approval.Body.Close()
	if approvalPayload.AlreadyApproved || approvalPayload.InitialPassword == "" {
		t.Fatalf("first approval response = %#v", approvalPayload)
	}
	if approvalPayload.Application.Status != "accepted" || approvalPayload.Application.StudentID == "" || approvalPayload.Application.ApprovedAt == "" {
		t.Fatalf("approval did not update application: %#v", approvalPayload.Application)
	}
	if approvalPayload.Student.Role != "student" || approvalPayload.Student.StudentID != approvalPayload.Application.StudentID || approvalPayload.Student.StudentEmail == "" {
		t.Fatalf("approval did not provision student: %#v", approvalPayload.Student)
	}
	student := store.users[approvalPayload.Student.ID]
	if student == nil || !verifyPassword(approvalPayload.InitialPassword, student.PasswordHash) {
		t.Fatal("initial password does not authenticate against the stored bcrypt hash")
	}
	if len(store.users) != 2 || len(store.mailbox) != 1 {
		t.Fatalf("approval records: users=%d mailbox=%d", len(store.users), len(store.mailbox))
	}
	if strings.Contains(store.mailbox[0].Body, approvalPayload.InitialPassword) || store.mailbox[0].RequestKey == "" {
		t.Fatal("initial password was persisted in the welcome mailbox")
	}
	if len(store.notifications) != 1 {
		t.Fatalf("admission notification count = %d", len(store.notifications))
	}

	studentsResponse, err := adminClient.Get(server.URL + "/api/admin/students")
	if err != nil {
		t.Fatal(err)
	}
	if studentsResponse.StatusCode != http.StatusOK {
		t.Fatalf("admin students status = %d", studentsResponse.StatusCode)
	}
	var studentsPayload struct {
		Students []AdminStudent `json:"students"`
	}
	if err := json.NewDecoder(studentsResponse.Body).Decode(&studentsPayload); err != nil {
		t.Fatal(err)
	}
	studentsResponse.Body.Close()
	if len(studentsPayload.Students) != 1 || studentsPayload.Students[0].StudentEmail != approvalPayload.Student.StudentEmail {
		t.Fatalf("student directory after approval = %#v", studentsPayload.Students)
	}

	admissionsResponse, err := adminClient.Get(server.URL + "/api/admin/admissions")
	if err != nil {
		t.Fatal(err)
	}
	rawAdmissions, err := io.ReadAll(admissionsResponse.Body)
	admissionsResponse.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if admissionsResponse.StatusCode != http.StatusOK || !strings.Contains(string(rawAdmissions), approvalPayload.Student.StudentID) || strings.Contains(string(rawAdmissions), approvalPayload.InitialPassword) {
		t.Fatalf("admission projection leaked or omitted state: %s", rawAdmissions)
	}

	replay := postJSON(t, adminClient, server.URL+"/api/admin/admissions/"+createdPayload.Application.ID+"/approve", map[string]any{})
	if replay.StatusCode != http.StatusOK {
		t.Fatalf("approval replay status = %d", replay.StatusCode)
	}
	var replayPayload struct {
		Student         AdminStudent `json:"student"`
		InitialPassword string       `json:"initialPassword"`
		AlreadyApproved bool         `json:"alreadyApproved"`
	}
	if err := json.NewDecoder(replay.Body).Decode(&replayPayload); err != nil {
		t.Fatal(err)
	}
	replay.Body.Close()
	if !replayPayload.AlreadyApproved || replayPayload.InitialPassword != "" || replayPayload.Student.ID != approvalPayload.Student.ID || len(store.mailbox) != 1 {
		t.Fatalf("approval replay was not idempotent: %#v mailbox=%d", replayPayload, len(store.mailbox))
	}

	statusChange := doJSON(t, adminClient, http.MethodPatch, server.URL+"/api/admin/admissions/"+createdPayload.Application.ID, map[string]string{"status": "rejected"})
	if statusChange.StatusCode != http.StatusConflict {
		t.Fatalf("legacy status change status = %d", statusChange.StatusCode)
	}
	statusChange.Body.Close()

	studentJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	studentClient := &http.Client{Jar: studentJar}
	studentLogin := postJSON(t, studentClient, server.URL+"/api/auth/login", map[string]string{
		"username": approvalPayload.Student.Username, "password": approvalPayload.InitialPassword,
	})
	if studentLogin.StatusCode != http.StatusOK {
		t.Fatalf("provisioned student login status = %d", studentLogin.StatusCode)
	}
	studentLogin.Body.Close()
	me, err := studentClient.Get(server.URL + "/api/auth/me")
	if err != nil {
		t.Fatal(err)
	}
	var mePayload struct {
		User map[string]any `json:"user"`
	}
	if err := json.NewDecoder(me.Body).Decode(&mePayload); err != nil {
		t.Fatal(err)
	}
	me.Body.Close()
	if mePayload.User["studentEmail"] != approvalPayload.Student.StudentEmail {
		t.Fatalf("student profile mailbox = %#v", mePayload.User["studentEmail"])
	}
	inbox, err := studentClient.Get(server.URL + "/api/mailbox")
	if err != nil {
		t.Fatal(err)
	}
	var inboxPayload struct {
		Email    string           `json:"email"`
		Messages []MailboxMessage `json:"messages"`
	}
	if err := json.NewDecoder(inbox.Body).Decode(&inboxPayload); err != nil {
		t.Fatal(err)
	}
	inbox.Body.Close()
	if inbox.StatusCode != http.StatusOK || inboxPayload.Email != approvalPayload.Student.StudentEmail || len(inboxPayload.Messages) != 1 {
		t.Fatalf("provisioned student mailbox = %#v", inboxPayload)
	}
	if strings.Contains(string(mustJSON(t, inboxPayload.Messages[0])), approvalPayload.InitialPassword) {
		t.Fatal("student mailbox exposed the initial password")
	}
}

func TestAdmissionApprovalAutomaticallySendsOnboardingNotice(t *testing.T) {
	store := NewStoreWithAdminAndDomain(testAdminUsername, testAdminPassword, "students.cgu.edu.kg")
	server := httptest.NewServer(NewServer(store, "web"))
	defer server.Close()
	sender := &recordingExternalSender{}
	server.Config.Handler.(*Server).setExternalMailSender(sender)

	publicClient := &http.Client{}
	created := postJSON(t, publicClient, server.URL+"/api/admissions", map[string]string{
		"name": "自动通知申请人", "email": "automatic@example.com", "school": "至冬学院",
	})
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("public admission status = %d", created.StatusCode)
	}
	var createdPayload struct {
		Application AdmissionApplication `json:"application"`
	}
	if err := json.NewDecoder(created.Body).Decode(&createdPayload); err != nil {
		t.Fatal(err)
	}
	created.Body.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	login := postJSON(t, client, server.URL+"/api/auth/login", map[string]string{"username": testAdminUsername, "password": testAdminPassword})
	if login.StatusCode != http.StatusOK {
		t.Fatalf("admin login status = %d", login.StatusCode)
	}
	login.Body.Close()
	approval := postJSON(t, client, server.URL+"/api/admin/admissions/"+createdPayload.Application.ID+"/approve", map[string]any{})
	if approval.StatusCode != http.StatusCreated {
		t.Fatalf("approval status = %d", approval.StatusCode)
	}
	var payload struct {
		Application     AdmissionApplication `json:"application"`
		InitialPassword string               `json:"initialPassword"`
		DeliveryStatus  string               `json:"deliveryStatus"`
	}
	if err := json.NewDecoder(approval.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	approval.Body.Close()
	if payload.InitialPassword == "" || payload.DeliveryStatus != mailboxDeliverySent || payload.Application.DeliveryStatus != mailboxDeliverySent || sender.callCount() != 1 {
		t.Fatalf("automatic onboarding delivery: payload=%#v calls=%d", payload, sender.callCount())
	}
	sender.mu.Lock()
	call := sender.calls[0]
	sender.mu.Unlock()
	if call.recipient != "automatic@example.com" || !strings.Contains(call.body, "cgu-") || strings.Contains(call.body, payload.InitialPassword) {
		t.Fatalf("unsafe or incorrect onboarding message: %#v", call)
	}
	if len(store.mailbox) != 1 || store.mailbox[0].DeliveryStatus != mailboxDeliverySent {
		t.Fatalf("stored onboarding status = %#v", store.mailbox)
	}
	// The administrator list must reconstruct the delivery state from the
	// mailbox after a fresh GET, not only from the approval response.
	admissionsResponse, err := client.Get(server.URL + "/api/admin/admissions")
	if err != nil {
		t.Fatal(err)
	}
	var admissionsPayload struct {
		Applications []AdmissionApplication `json:"applications"`
	}
	if err := json.NewDecoder(admissionsResponse.Body).Decode(&admissionsPayload); err != nil {
		admissionsResponse.Body.Close()
		t.Fatal(err)
	}
	admissionsResponse.Body.Close()
	if admissionsResponse.StatusCode != http.StatusOK || len(admissionsPayload.Applications) != 1 || admissionsPayload.Applications[0].DeliveryStatus != mailboxDeliverySent {
		t.Fatalf("admission list lost delivery state: status=%d payload=%#v", admissionsResponse.StatusCode, admissionsPayload)
	}

	replay := postJSON(t, client, server.URL+"/api/admin/admissions/"+createdPayload.Application.ID+"/approve", map[string]any{})
	if replay.StatusCode != http.StatusOK {
		t.Fatalf("approval replay status = %d", replay.StatusCode)
	}
	replay.Body.Close()
	if sender.callCount() != 1 {
		t.Fatalf("approval replay sent %d duplicate notices", sender.callCount())
	}
}

func TestAdmissionNoticeDeliveryToleratesMinimalReplayProjection(t *testing.T) {
	store := NewStoreWithAdminAndDomain(testAdminUsername, testAdminPassword, "students.cgu.edu.kg")
	server := NewServer(store, "web")
	store.mailbox = append(store.mailbox, &MailboxMessage{
		ID: "mail-minimal-replay", RecipientID: "missing-student", ExternalRecipient: "replay@example.com",
		Subject: "Replay", Body: "Replay body", CreatedAt: time.Now().UTC().Format(time.RFC3339),
		DeliveryMode: mailboxDeliveryModeSMTP, DeliveryStatus: mailboxDeliveryPending,
	})
	result := &AdmissionApproval{MailboxID: "mail-minimal-replay"}
	server.deliverAdmissionNotice(context.Background(), result)
	if result.DeliveryStatus != mailboxDeliveryNotConfigured {
		t.Fatalf("minimal replay delivery status = %q, want %q", result.DeliveryStatus, mailboxDeliveryNotConfigured)
	}
}

func TestAdmissionApprovalRepairsIncompleteAcceptedRecord(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	store.admissions = append(store.admissions, &AdmissionApplication{
		ID: "application-incomplete", Name: "待修复申请", Email: "incomplete@example.com",
		School: "综合学院", Status: "accepted", CreatedAt: "2026-08-25T00:00:00Z", UpdatedAt: "2026-08-25T00:00:00Z",
	})
	result, apiError := store.approveAdmission("application-incomplete", "admin")
	if apiError != nil || result == nil || result.Application.StudentID == "" || result.Student.ID == "" || result.InitialPassword == "" {
		t.Fatalf("incomplete accepted record repair result=%#v error=%#v", result, apiError)
	}
	if len(store.users) != 2 || len(store.mailbox) != 1 || store.admissions[0].Status != "accepted" || store.admissions[0].StudentID == "" {
		t.Fatalf("incomplete accepted record repair side effects: users=%d mailbox=%d application=%#v", len(store.users), len(store.mailbox), store.admissions[0])
	}
}

func TestAdmissionApprovalDatabaseTransactionAndReplay(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStoreWithAdminAndDomain(testAdminUsername, testAdminPassword, "students.cgu.edu.kg")
	store.db = db
	applicationID := "application-db-approval"
	store.admissions = append(store.admissions, &AdmissionApplication{
		ID: applicationID, Name: "数据库申请人", Email: "db-applicant@example.com", School: "至冬学院",
		Status: "pending", CreatedAt: "2026-08-25T00:00:00Z", UpdatedAt: "2026-08-25T00:00:00Z",
	})
	studentID, username, userID := admissionStudentIdentity(applicationID)
	row := sqlmock.NewRows([]string{"id", "name_text", "email", "school_text", "status_name", "notes_text", "created_at", "updated_at", "student_id", "approved_at", "approved_by", "initial_password_issued_at"}).
		AddRow(applicationID, "数据库申请人", "db-applicant@example.com", "至冬学院", "pending", "", "2026-08-25T00:00:00Z", "2026-08-25T00:00:00Z", "", "", "", "")
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(admissionForUpdateSQL)).WithArgs(applicationID).WillReturnRows(row)
	mock.ExpectExec(regexp.QuoteMeta(admissionApprovalUserInsertSQL)).WithArgs(userID, username, "数据库申请人", "db-applicant@example.com", "student", sqlmock.AnyArg(), studentID, "至冬学院", "2026", false).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta(mailboxInsertSQL)).WithArgs(userIDToMailboxID(userID), userID, "admin", "CGU 教务处", "CGU 学生账户已建立", sqlmock.AnyArg(), sqlmock.AnyArg(), nil, mailboxDeliveryModeSMTP, "db-applicant@example.com", mailboxDeliveryPending, "", "", sqlmock.AnyArg(), "").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta(admissionApprovalUpdateSQL)).WithArgs("accepted", studentID, sqlmock.AnyArg(), "admin", sqlmock.AnyArg(), sqlmock.AnyArg(), applicationID).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	result, apiError := store.approveAdmission(applicationID, "admin")
	if apiError != nil || result == nil || result.InitialPassword == "" || result.AlreadyApproved {
		t.Fatalf("database approval result=%#v error=%#v", result, apiError)
	}
	if len(store.mailbox) != 1 || strings.Contains(store.mailbox[0].Body, result.InitialPassword) || store.mailbox[0].DeliveryMode != mailboxDeliveryModeSMTP || store.mailbox[0].ExternalRecipient != "db-applicant@example.com" {
		t.Fatalf("database approval mailbox leaked password: %#v", store.mailbox)
	}
	if !verifyPassword(result.InitialPassword, store.users[userID].PasswordHash) {
		t.Fatal("database approval password does not match stored hash")
	}

	// Simulate a fresh process cache: replay reads the durable student by its
	// linked academic ID and must not regenerate or return a password.
	delete(store.users, userID)
	mock.ExpectBegin()
	replayAdmissionRow := sqlmock.NewRows([]string{"id", "name_text", "email", "school_text", "status_name", "notes_text", "created_at", "updated_at", "student_id", "approved_at", "approved_by", "initial_password_issued_at"}).
		AddRow(applicationID, "数据库申请人", "db-applicant@example.com", "至冬学院", "accepted", "", "2026-08-25T00:00:00Z", "2026-08-25T00:00:01Z", studentID, "2026-08-25T00:00:01Z", "admin", "2026-08-25T00:00:01Z")
	mock.ExpectQuery(regexp.QuoteMeta(admissionForUpdateSQL)).WithArgs(applicationID).WillReturnRows(replayAdmissionRow)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, username, name_text, email, role_name, password_hash, student_id, college, year_text, disabled_flag FROM cgu_users WHERE student_id = ? AND role_name = 'student' LIMIT 1`)).WithArgs(studentID).WillReturnRows(sqlmock.NewRows([]string{"id", "username", "name_text", "email", "role_name", "password_hash", "student_id", "college", "year_text", "disabled_flag"}).AddRow(userID, username, "数据库申请人", "db-applicant@example.com", "student", hashPassword("not-returned"), studentID, "至冬学院", "2026", false))
	mock.ExpectCommit()
	replay, apiError := store.approveAdmission(applicationID, "admin")
	if apiError != nil || replay == nil || !replay.AlreadyApproved || replay.InitialPassword != "" {
		t.Fatalf("database replay result=%#v error=%#v", replay, apiError)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAdmissionApprovalDatabaseRollbackLeavesNoPartialRecords(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	store.db = db
	applicationID := "application-db-rollback"
	store.admissions = append(store.admissions, &AdmissionApplication{
		ID: applicationID, Name: "回滚申请人", Email: "rollback-approval@example.com", School: "综合学院",
		Status: "pending", CreatedAt: "2026-08-25T00:00:00Z", UpdatedAt: "2026-08-25T00:00:00Z",
	})
	studentID, username, userID := admissionStudentIdentity(applicationID)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(admissionForUpdateSQL)).WithArgs(applicationID).WillReturnRows(sqlmock.NewRows([]string{"id", "name_text", "email", "school_text", "status_name", "notes_text", "created_at", "updated_at", "student_id", "approved_at", "approved_by", "initial_password_issued_at"}).
		AddRow(applicationID, "回滚申请人", "rollback-approval@example.com", "综合学院", "pending", "", "2026-08-25T00:00:00Z", "2026-08-25T00:00:00Z", "", "", "", ""))
	mock.ExpectExec(regexp.QuoteMeta(admissionApprovalUserInsertSQL)).WithArgs(userID, username, "回滚申请人", "rollback-approval@example.com", "student", sqlmock.AnyArg(), studentID, "综合学院", "2026", false).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta(mailboxInsertSQL)).WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	result, apiError := store.approveAdmission(applicationID, "admin")
	if result != nil || apiError == nil || apiError.Status != http.StatusServiceUnavailable {
		t.Fatalf("rollback approval result=%#v error=%#v", result, apiError)
	}
	if len(store.users) != 1 || len(store.mailbox) != 0 || store.admissions[0].Status != "pending" || store.admissions[0].StudentID != "" {
		t.Fatalf("rollback left partial approval state: users=%d mailbox=%d application=%#v", len(store.users), len(store.mailbox), store.admissions[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func userIDToMailboxID(userID string) string {
	return "mail-admission-" + strings.TrimPrefix(userID, "student-")
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
