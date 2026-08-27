package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

func onboardingPassword(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "初始密码：") {
			return strings.TrimSpace(strings.TrimPrefix(line, "初始密码："))
		}
	}
	return ""
}

func TestAdmissionApprovalProvisionsStudentAndMailbox(t *testing.T) {
	store := NewStoreWithAdminAndDomain(testAdminUsername, testAdminPassword, "students.cgu.edu.kg")
	handler := NewServer(store, "web")
	sender := &recordingExternalSender{}
	handler.setExternalMailSender(sender)
	server := httptest.NewServer(handler)
	defer server.Close()

	publicClient := &http.Client{}
	created := postJSON(t, publicClient, server.URL+"/api/admissions", map[string]string{
		"name": "至冬申请人", "englishName": "Polar Applicant", "email": "applicant@example.com", "school": "至冬与极地研究学院",
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
	rawApproval, err := io.ReadAll(approval.Body)
	approval.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rawApproval), "initialPassword") {
		t.Fatalf("approval response exposed the initial password field: %s", rawApproval)
	}
	var approvalPayload struct {
		Application     AdmissionApplication `json:"application"`
		Student         AdminStudent         `json:"student"`
		AlreadyApproved bool                 `json:"alreadyApproved"`
	}
	if err := json.Unmarshal(rawApproval, &approvalPayload); err != nil {
		t.Fatal(err)
	}
	if approvalPayload.AlreadyApproved {
		t.Fatalf("first approval response = %#v", approvalPayload)
	}
	if approvalPayload.Application.Status != "accepted" || approvalPayload.Application.StudentID == "" || approvalPayload.Application.ApprovedAt == "" {
		t.Fatalf("approval did not update application: %#v", approvalPayload.Application)
	}
	if approvalPayload.Student.Role != "student" || approvalPayload.Student.StudentID != approvalPayload.Application.StudentID || approvalPayload.Student.StudentEmail == "" {
		t.Fatalf("approval did not provision student: %#v", approvalPayload.Student)
	}
	student := store.users[approvalPayload.Student.ID]
	if student == nil || sender.callCount() != 1 {
		t.Fatalf("student or SMTP delivery missing: student=%#v calls=%d", student, sender.callCount())
	}
	sender.mu.Lock()
	initialPassword := onboardingPassword(sender.calls[0].body)
	sender.mu.Unlock()
	if initialPassword == "" || !verifyPassword(initialPassword, student.PasswordHash) {
		t.Fatal("initial password does not authenticate against the stored bcrypt hash")
	}
	if len(store.users) != 2 || len(store.mailbox) != 1 {
		t.Fatalf("approval records: users=%d mailbox=%d", len(store.users), len(store.mailbox))
	}
	if strings.Contains(store.mailbox[0].Body, initialPassword) || store.mailbox[0].RequestKey == "" {
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
	if admissionsResponse.StatusCode != http.StatusOK || !strings.Contains(string(rawAdmissions), approvalPayload.Student.StudentID) || strings.Contains(string(rawAdmissions), initialPassword) || strings.Contains(string(rawAdmissions), "initialPassword") {
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
		"username": approvalPayload.Student.Username, "password": initialPassword,
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
	if strings.Contains(string(mustJSON(t, inboxPayload.Messages[0])), initialPassword) {
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
		"name": "自动通知申请人", "englishName": "Automatic Applicant", "email": "automatic@example.com", "school": "至冬学院",
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
		Application    AdmissionApplication `json:"application"`
		DeliveryStatus string               `json:"deliveryStatus"`
	}
	if err := json.NewDecoder(approval.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	approval.Body.Close()
	if payload.DeliveryStatus != mailboxDeliverySent || payload.Application.DeliveryStatus != mailboxDeliverySent || sender.callCount() != 1 {
		t.Fatalf("automatic onboarding delivery: payload=%#v calls=%d", payload, sender.callCount())
	}
	sender.mu.Lock()
	call := sender.calls[0]
	sender.mu.Unlock()
	initialPassword := onboardingPassword(call.body)
	if call.recipient != "automatic@example.com" || !strings.Contains(call.body, "CGU-") || initialPassword == "" {
		t.Fatalf("unsafe or incorrect onboarding message: %#v", call)
	}
	studentID, _, userID := admissionStudentIdentityForApplication(&AdmissionApplication{ID: createdPayload.Application.ID, Name: "自动通知申请人", EnglishName: "Automatic Applicant", Email: "automatic@example.com", School: "至冬学院", CreatedAt: createdPayload.Application.CreatedAt})
	student := store.users[userID]
	if student == nil || student.StudentID != studentID || !verifyPassword(initialPassword, student.PasswordHash) {
		t.Fatalf("onboarding password did not match provisioned student: student=%#v", student)
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

func TestAdmissionCredentialResendRotatesOnlyThroughSMTP(t *testing.T) {
	store := NewStoreWithAdminAndDomain(testAdminUsername, testAdminPassword, "students.cgu.edu.kg")
	server := httptest.NewServer(NewServer(store, "web"))
	defer server.Close()
	sender := &recordingExternalSender{}
	server.Config.Handler.(*Server).setExternalMailSender(sender)

	created := postJSON(t, &http.Client{}, server.URL+"/api/admissions", map[string]string{
		"name": "重发凭据申请人", "englishName": "Resend Applicant", "email": "resend@example.com", "school": "风与自然科学",
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
	adminClient := &http.Client{Jar: jar}
	login := postJSON(t, adminClient, server.URL+"/api/auth/login", map[string]string{"username": testAdminUsername, "password": testAdminPassword})
	if login.StatusCode != http.StatusOK {
		t.Fatalf("admin login status = %d", login.StatusCode)
	}
	login.Body.Close()
	approval := postJSON(t, adminClient, server.URL+"/api/admin/admissions/"+createdPayload.Application.ID+"/approve", map[string]any{})
	if approval.StatusCode != http.StatusCreated {
		t.Fatalf("approval status = %d", approval.StatusCode)
	}
	approval.Body.Close()
	if sender.callCount() != 1 {
		t.Fatalf("initial credential delivery calls = %d", sender.callCount())
	}
	sender.mu.Lock()
	firstPassword := onboardingPassword(sender.calls[0].body)
	sender.mu.Unlock()
	if firstPassword == "" {
		t.Fatal("initial SMTP message did not contain a password")
	}
	// A reset must invalidate sessions that were issued with the old
	// credential, not only reject a future password login.
	studentSessionJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	studentSessionClient := &http.Client{Jar: studentSessionJar}
	studentRecord := store.users[func() string {
		_, _, generatedID := admissionStudentIdentity(createdPayload.Application.ID)
		return generatedID
	}()]
	if studentRecord == nil {
		t.Fatal("provisioned student record is missing before session test")
	}
	studentSessionLogin := postJSON(t, studentSessionClient, server.URL+"/api/auth/login", map[string]string{
		"username": studentRecord.Username, "password": firstPassword,
	})
	if studentSessionLogin.StatusCode != http.StatusOK {
		t.Fatalf("student session login status = %d", studentSessionLogin.StatusCode)
	}
	studentSessionLogin.Body.Close()

	resend := postJSON(t, adminClient, server.URL+"/api/admin/admissions/"+createdPayload.Application.ID+"/resend-credentials", map[string]any{})
	if resend.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resend.Body)
		resend.Body.Close()
		t.Fatalf("credential resend status = %d body = %s", resend.StatusCode, raw)
	}
	rawResend, err := io.ReadAll(resend.Body)
	resend.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rawResend), "initialPassword") || strings.Contains(string(rawResend), firstPassword) {
		t.Fatalf("credential resend response exposed a secret: %s", rawResend)
	}
	var resendPayload struct {
		DeliveryStatus string `json:"deliveryStatus"`
	}
	if err := json.Unmarshal(rawResend, &resendPayload); err != nil {
		t.Fatal(err)
	}
	if resendPayload.DeliveryStatus != mailboxDeliverySent || sender.callCount() != 2 {
		t.Fatalf("credential resend delivery = %#v calls=%d", resendPayload, sender.callCount())
	}
	sender.mu.Lock()
	secondPassword := onboardingPassword(sender.calls[1].body)
	sender.mu.Unlock()
	if secondPassword == "" || secondPassword == firstPassword {
		t.Fatal("credential resend did not rotate the password")
	}
	_, _, userID := admissionStudentIdentityForApplication(&AdmissionApplication{ID: createdPayload.Application.ID, Name: "重发凭据申请人", Email: "resend@example.com", School: "风与自然科学", CreatedAt: createdPayload.Application.CreatedAt})
	student := store.users[userID]
	if student == nil || !verifyPassword(secondPassword, student.PasswordHash) || verifyPassword(firstPassword, student.PasswordHash) {
		t.Fatal("rotated password hash state is incorrect")
	}
	if strings.Contains(store.mailbox[0].Body, secondPassword) || strings.Contains(store.mailbox[0].Body, firstPassword) {
		t.Fatal("credential password was persisted in the internal mailbox")
	}
	studentSession, err := studentSessionClient.Get(server.URL + "/api/auth/me")
	if err != nil {
		t.Fatal(err)
	}
	if studentSession.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old student session after credential resend status = %d", studentSession.StatusCode)
	}
	studentSession.Body.Close()

	oldLogin := postJSON(t, &http.Client{}, server.URL+"/api/auth/login", map[string]string{"username": student.Username, "password": firstPassword})
	if oldLogin.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old credential login status = %d", oldLogin.StatusCode)
	}
	oldLogin.Body.Close()
	newLogin := postJSON(t, &http.Client{}, server.URL+"/api/auth/login", map[string]string{"username": student.Username, "password": secondPassword})
	if newLogin.StatusCode != http.StatusOK {
		t.Fatalf("rotated credential login status = %d", newLogin.StatusCode)
	}
	newLogin.Body.Close()
}

func TestAdmissionCredentialResendRejectsInvalidPersistedContactBeforeRotation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	store.db = db
	applicationID := "application-resend-invalid-contact"
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(admissionForUpdateSQL)).WithArgs(applicationID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name_text", "english_name", "email", "school_text", "status_name", "notes_text",
			"created_at", "updated_at", "student_id", "approved_at", "approved_by", "initial_password_issued_at",
		}).AddRow(applicationID, "无效邮箱申请", "", "bad recipient", "综合学院", "accepted", "",
			"2026-08-25T00:00:00Z", "2026-08-25T00:00:00Z", "CGU-INVALID-CONTACT-001", "2026-08-25T00:00:01Z", "admin", ""))
	mock.ExpectRollback()
	result, apiError := store.resendAdmissionCredentials(applicationID, "admin")
	if result != nil || apiError == nil || apiError.Status != http.StatusConflict || apiError.Code != "external_recipient_invalid" {
		t.Fatalf("invalid persisted contact resend result = %#v, error = %#v", result, apiError)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
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

func TestAdmissionCredentialRotationBlocksActiveDeliveryLease(t *testing.T) {
	active := time.Now().UTC().Format(time.RFC3339Nano) + "|lease"
	if err := admissionCredentialRotationAllowed(mailboxDeliverySending, active, ""); !errors.Is(err, errAdmissionCredentialDeliveryInProgress) {
		t.Fatalf("active sending lease error = %v", err)
	}
	if err := admissionCredentialRotationAllowed(mailboxDeliveryPending, "", time.Now().UTC().Format(time.RFC3339Nano)); !errors.Is(err, errAdmissionCredentialDeliveryInProgress) {
		t.Fatalf("fresh pending credential error = %v", err)
	}
	if err := admissionCredentialRotationAllowed(mailboxDeliverySent, "", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("sent delivery unexpectedly blocked: %v", err)
	}
}

func TestAdmissionApprovalRepairsIncompleteAcceptedRecord(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	store.admissions = append(store.admissions, &AdmissionApplication{
		ID: "application-incomplete", Name: "待修复申请", Email: "incomplete@example.com",
		School: "综合学院", Status: "accepted", CreatedAt: "2026-08-25T00:00:00Z", UpdatedAt: "2026-08-25T00:00:00Z",
	})
	result, apiError := store.approveAdmission("application-incomplete", "admin")
	if apiError != nil || result == nil || result.Application.StudentID == "" || result.Student.ID == "" || result.InitialPassword != "" || result.initialPasswordForDelivery == "" {
		t.Fatalf("incomplete accepted record repair result=%#v error=%#v", result, apiError)
	}
	if len(store.users) != 2 || len(store.mailbox) != 1 || store.admissions[0].Status != "accepted" || store.admissions[0].StudentID == "" {
		t.Fatalf("incomplete accepted record repair side effects: users=%d mailbox=%d application=%#v", len(store.users), len(store.mailbox), store.admissions[0])
	}
}

func TestAdmissionApprovalReplayRejectsAmbiguousLegacyStudentID(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	application := &AdmissionApplication{
		ID: "application-replay-duplicate", Name: "重复重放申请", Email: "duplicate-replay@example.com",
		School: "综合学院", Status: "accepted", StudentID: "LEGACY-REPLAY-001",
		CreatedAt: "2026-08-25T00:00:00Z", UpdatedAt: "2026-08-25T00:00:00Z",
	}
	store.admissions = append(store.admissions, application)
	store.users["legacy-replay-a"] = &User{ID: "legacy-replay-a", Username: "legacy-replay-a", Name: "重复账号甲", Role: "student", StudentID: application.StudentID}
	store.users["legacy-replay-b"] = &User{ID: "legacy-replay-b", Username: "legacy-replay-b", Name: "重复账号乙", Role: "student", StudentID: application.StudentID}

	result, apiError := store.approveAdmission(application.ID, testAdminUsername)
	if result != nil || apiError == nil || apiError.Status != http.StatusConflict || apiError.Code != "student_id_ambiguous" {
		t.Fatalf("ambiguous replay result=%#v error=%#v", result, apiError)
	}
}

func TestAdmissionApprovalDatabaseReplayRejectsAmbiguousLegacyStudentID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	store.db = db
	applicationID := "application-db-replay-duplicate"
	studentID := "LEGACY-DB-REPLAY-001"
	_, _, deterministicUserID := admissionStudentIdentity(applicationID)
	studentRows := sqlmock.NewRows([]string{"id", "username", "name_text", "email", "role_name", "password_hash", "student_id", "college", "year_text", "disabled_flag"}).
		AddRow("legacy-db-replay-a", "legacy-db-replay-a", "重复账号甲", "legacy-a@example.com", "student", hashPassword("one"), studentID, "综合学院", "2026", false).
		AddRow("legacy-db-replay-b", "legacy-db-replay-b", "重复账号乙", "legacy-b@example.com", "student", hashPassword("two"), studentID, "综合学院", "2026", false)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(admissionForUpdateSQL)).WithArgs(applicationID).WillReturnRows(sqlmock.NewRows([]string{"id", "name_text", "english_name", "email", "school_text", "status_name", "notes_text", "created_at", "updated_at", "student_id", "approved_at", "approved_by", "initial_password_issued_at"}).
		AddRow(applicationID, "数据库重复申请", "", "db-duplicate@example.com", "综合学院", "accepted", "", "2026-08-25T00:00:00Z", "2026-08-25T00:00:00Z", studentID, "2026-08-25T00:00:00Z", "admin", "2026-08-25T00:00:00Z"))
	mock.ExpectQuery(regexp.QuoteMeta(studentForAdmissionDeleteSQL)).WithArgs(deterministicUserID, studentID, studentID, deterministicUserID, studentID).WillReturnRows(studentRows)
	mock.ExpectRollback()

	result, apiError := store.approveAdmission(applicationID, testAdminUsername)
	if result != nil || apiError == nil || apiError.Status != http.StatusConflict || apiError.Code != "student_id_ambiguous" {
		t.Fatalf("database ambiguous replay result=%#v error=%#v", result, apiError)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
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
	studentID, username, userID := admissionStudentIdentityForApplication(&AdmissionApplication{ID: applicationID, Name: "数据库申请人", Email: "db-applicant@example.com", School: "至冬学院", CreatedAt: "2026-08-25T00:00:00Z"})
	row := sqlmock.NewRows([]string{"id", "name_text", "english_name", "email", "school_text", "status_name", "notes_text", "created_at", "updated_at", "student_id", "approved_at", "approved_by", "initial_password_issued_at"}).
		AddRow(applicationID, "数据库申请人", "", "db-applicant@example.com", "至冬学院", "pending", "", "2026-08-25T00:00:00Z", "2026-08-25T00:00:00Z", "", "", "", "")
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(admissionForUpdateSQL)).WithArgs(applicationID).WillReturnRows(row)
	mock.ExpectExec(regexp.QuoteMeta(admissionApprovalUserInsertSQL)).WithArgs(userID, username, "数据库申请人", "db-applicant@example.com", "student", sqlmock.AnyArg(), studentID, "至冬学院", "2026", false).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta(mailboxInsertSQL)).WithArgs(userIDToMailboxID(userID), userID, "admin", "CGU 教务处", "CGU 学生账户已建立", sqlmock.AnyArg(), sqlmock.AnyArg(), nil, mailboxDeliveryModeSMTP, "db-applicant@example.com", mailboxDeliveryPending, "", "", sqlmock.AnyArg(), "").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta(admissionApprovalUpdateSQL)).WithArgs("accepted", studentID, sqlmock.AnyArg(), "admin", sqlmock.AnyArg(), sqlmock.AnyArg(), applicationID).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	result, apiError := store.approveAdmission(applicationID, "admin")
	if apiError != nil || result == nil || result.InitialPassword != "" || result.initialPasswordForDelivery == "" || result.AlreadyApproved {
		t.Fatalf("database approval result=%#v error=%#v", result, apiError)
	}
	initialPassword := result.initialPasswordForDelivery
	if len(store.mailbox) != 1 || strings.Contains(store.mailbox[0].Body, initialPassword) || store.mailbox[0].DeliveryMode != mailboxDeliveryModeSMTP || store.mailbox[0].ExternalRecipient != "db-applicant@example.com" {
		t.Fatalf("database approval mailbox leaked password: %#v", store.mailbox)
	}
	if !verifyPassword(initialPassword, store.users[userID].PasswordHash) {
		t.Fatal("database approval password does not match stored hash")
	}

	// Simulate a fresh process cache: replay reads the durable student by its
	// linked academic ID and must not regenerate or return a password.
	delete(store.users, userID)
	mock.ExpectBegin()
	replayAdmissionRow := sqlmock.NewRows([]string{"id", "name_text", "english_name", "email", "school_text", "status_name", "notes_text", "created_at", "updated_at", "student_id", "approved_at", "approved_by", "initial_password_issued_at"}).
		AddRow(applicationID, "数据库申请人", "", "db-applicant@example.com", "至冬学院", "accepted", "", "2026-08-25T00:00:00Z", "2026-08-25T00:00:01Z", studentID, "2026-08-25T00:00:01Z", "admin", "2026-08-25T00:00:01Z")
	mock.ExpectQuery(regexp.QuoteMeta(admissionForUpdateSQL)).WithArgs(applicationID).WillReturnRows(replayAdmissionRow)
	mock.ExpectQuery(regexp.QuoteMeta(studentForAdmissionDeleteSQL)).WithArgs(userID, studentID, studentID, userID, studentID).WillReturnRows(sqlmock.NewRows([]string{"id", "username", "name_text", "email", "role_name", "password_hash", "student_id", "college", "year_text", "disabled_flag"}).AddRow(userID, username, "数据库申请人", "db-applicant@example.com", "student", hashPassword("not-returned"), studentID, "至冬学院", "2026", false))
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
	studentID, username, userID := admissionStudentIdentityForApplication(&AdmissionApplication{ID: applicationID, Name: "回滚申请人", Email: "rollback-approval@example.com", School: "综合学院", CreatedAt: "2026-08-25T00:00:00Z"})
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(admissionForUpdateSQL)).WithArgs(applicationID).WillReturnRows(sqlmock.NewRows([]string{"id", "name_text", "english_name", "email", "school_text", "status_name", "notes_text", "created_at", "updated_at", "student_id", "approved_at", "approved_by", "initial_password_issued_at"}).
		AddRow(applicationID, "回滚申请人", "", "rollback-approval@example.com", "综合学院", "pending", "", "2026-08-25T00:00:00Z", "2026-08-25T00:00:00Z", "", "", "", ""))
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
