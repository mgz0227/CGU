package main

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func admissionUpdateRows(id, name, email, school, status, notes, studentID string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "name_text", "english_name", "email", "school_text", "status_name", "notes_text",
		"created_at", "updated_at", "student_id", "approved_at", "approved_by", "initial_password_issued_at",
	}).AddRow(id, name, "", email, school, status, notes, "2026-08-25T00:00:00Z", "2026-08-25T00:00:00Z", studentID, "2026-08-25T00:00:01Z", "admin", "2026-08-25T00:00:01Z")
}

func TestAdmissionUpdateRechecksProvisioningInDatabase(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	store.db = db
	application := &AdmissionApplication{
		ID: "application-update-race", Name: "缓存申请人", Email: "cache@example.com", School: "综合学院", Status: "pending",
	}
	store.admissions = []*AdmissionApplication{application}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(admissionForUpdateSQL)).WithArgs(application.ID).
		WillReturnRows(admissionUpdateRows(application.ID, "数据库申请人", "db@example.com", "综合学院", "accepted", "", "CGU-2026-0007"))
	mock.ExpectRollback()
	updated, apiError := store.updateAdmission(application.ID, AdmissionApplicationInput{
		Name: "旧实例修改", Email: "stale@example.com", School: "综合学院", Notes: "不应保存",
	})
	if updated != nil || apiError == nil || apiError.Status != http.StatusConflict || apiError.Code != "admission_already_approved" {
		t.Fatalf("stale admission update result = %#v, error = %#v", updated, apiError)
	}
	if application.Name != "缓存申请人" || application.Email != "cache@example.com" || application.Notes != "" || application.StudentID != "" {
		t.Fatalf("stale admission update mutated cache: %#v", application)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAdmissionEnglishNameDrivesStableRegistrarNumber(t *testing.T) {
	item := &AdmissionApplication{ID: "application-english-id", Name: "旅行者", EnglishName: "Jean-Luc", Email: "traveler@example.com", School: "至冬学院", CreatedAt: "2026-08-25T00:00:00Z"}
	studentID, _, _ := admissionStudentIdentityForApplication(item)
	want := "CGU-JEANLUC-2026-SPG-POL-"
	if !strings.HasPrefix(studentID, want) {
		t.Fatalf("student id = %q, want prefix %q", studentID, want)
	}
	if len(studentID) > 64 {
		t.Fatalf("student id exceeds registrar column: %d", len(studentID))
	}
	if _, err := normalizeAdmission(AdmissionApplicationInput{Name: item.Name, EnglishName: "Jean/侵入", Email: item.Email, School: item.School}, nil); err == nil || err.Code != "invalid_input" {
		t.Fatal("invalid English name was accepted")
	}
}

func TestAdmissionUpdateUsesDurableProfileForNotesOnlyPatch(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	store.db = db
	application := &AdmissionApplication{
		ID: "application-update-notes", Name: "旧缓存姓名", Email: "old@example.com", School: "综合学院", Status: "pending",
		DeliveryStatus: mailboxDeliverySent,
	}
	store.admissions = []*AdmissionApplication{application}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(admissionForUpdateSQL)).WithArgs(application.ID).
		WillReturnRows(admissionUpdateRows(application.ID, "数据库姓名", "durable@example.com", "综合学院", "accepted", "旧备注", "CGU-2026-0008"))
	mock.ExpectExec(regexp.QuoteMeta(admissionUpdateSQL)).WithArgs(
		"数据库姓名", "", "durable@example.com", "综合学院", "新备注", sqlmock.AnyArg(), application.ID,
	).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	updated, apiError := store.updateAdmission(application.ID, AdmissionApplicationInput{Notes: "新备注"})
	if apiError != nil || updated == nil {
		t.Fatalf("durable notes update result = %#v, error = %#v", updated, apiError)
	}
	if updated.Name != "数据库姓名" || updated.Email != "durable@example.com" || updated.StudentID != "CGU-2026-0008" || updated.Notes != "新备注" || updated.Status != "accepted" {
		t.Fatalf("durable profile was not retained: %#v", updated)
	}
	if application.Name != "数据库姓名" || application.Email != "durable@example.com" || application.StudentID != "CGU-2026-0008" || application.DeliveryStatus != mailboxDeliverySent {
		t.Fatalf("in-memory admission was not refreshed from durable row: %#v", application)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAdmissionUpdateProvisionedEmailSyncsStudentAccount(t *testing.T) {
	store := NewStoreWithAdminAndDomain(testAdminUsername, testAdminPassword, "students.cgu.edu.kg")
	application := &AdmissionApplication{
		ID: "application-contact-sync", Name: "联络申请人", EnglishName: "Contact Applicant",
		Email: "old-contact@example.com", School: "至冬学院", Status: "accepted",
		CreatedAt: "2026-08-25T00:00:00Z", UpdatedAt: "2026-08-25T00:00:00Z",
	}
	studentID, username, userID := admissionStudentIdentityForApplication(application)
	application.StudentID = studentID
	student := &User{ID: userID, Username: username, Name: application.Name, Email: application.Email, Role: "student", StudentID: studentID, College: application.School, Year: "2026", PasswordHash: hashPassword("long-password-for-contact-sync")}
	store.admissions = []*AdmissionApplication{application}
	store.users[userID] = student
	store.mailbox = append(store.mailbox, &MailboxMessage{
		ID: "mail-contact-sync", RequestKey: admissionApprovalRequestKey(application.ID),
		ExternalRecipient: application.Email, DeliveryMode: mailboxDeliveryModeSMTP,
		DeliveryStatus: mailboxDeliveryFailed, DeliveryError: "old recipient rejected",
	})

	updated, apiError := store.updateAdmission(application.ID, AdmissionApplicationInput{Email: "new-contact@example.com", Notes: "邮箱已核验"})
	if apiError != nil || updated == nil {
		t.Fatalf("provisioned contact update result = %#v, error = %#v", updated, apiError)
	}
	if updated.Email != "new-contact@example.com" || store.admissions[0].Email != updated.Email || student.Email != updated.Email {
		t.Fatalf("contact email was not synchronized: updated=%#v application=%#v student=%#v", updated, store.admissions[0], student)
	}
	if store.mailbox[0].ExternalRecipient != updated.Email {
		t.Fatalf("failed onboarding mailbox recipient was not synchronized: %#v", store.mailbox[0])
	}
}

func TestAdmissionUpdateDatabaseSynchronizesPendingMailboxRecipientAtomically(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	store := NewStoreWithAdminAndDomain(testAdminUsername, testAdminPassword, "students.cgu.edu.kg")
	store.db = db
	application := &AdmissionApplication{
		ID: "application-contact-sync-db", Name: "数据库联络申请", Email: "old-db-contact@example.com",
		School: "至冬学院", Status: "accepted", StudentID: "CGU-DB-CONTACT-001",
	}
	store.admissions = []*AdmissionApplication{application}
	_, _, deterministicID := admissionStudentIdentity(application.ID)
	mailboxID := "mail-contact-sync-db"
	store.mailbox = []*MailboxMessage{{
		ID: mailboxID, RequestKey: admissionApprovalRequestKey(application.ID),
		ExternalRecipient: application.Email, DeliveryMode: mailboxDeliveryModeSMTP,
		// Deliberately stale: the durable row below is still pending. A
		// successful transaction must refresh this cache projection too.
		DeliveryStatus: mailboxDeliverySent,
	}}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(admissionForUpdateSQL)).WithArgs(application.ID).
		WillReturnRows(admissionUpdateRows(application.ID, application.Name, application.Email, application.School, application.Status, "", application.StudentID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, delivery_mode, delivery_status, delivery_started_at FROM cgu_mailbox_messages WHERE request_key = ? FOR UPDATE")).
		WithArgs(admissionApprovalRequestKey(application.ID)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "delivery_mode", "delivery_status", "delivery_started_at"}).
			AddRow(mailboxID, mailboxDeliveryModeSMTP, mailboxDeliveryPending, ""))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE cgu_mailbox_messages SET external_recipient = ? WHERE id = ?")).
		WithArgs("new-db-contact@example.com", mailboxID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta(studentForAdmissionDeleteSQL)).
		WithArgs(deterministicID, application.StudentID, application.StudentID, deterministicID, application.StudentID).
		WillReturnRows(studentDeleteRows("student-db-contact", "student-db-contact", application.Name, application.Email, application.StudentID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, username, email, student_id, role_name FROM cgu_users WHERE id <> ? FOR UPDATE")).
		WithArgs("student-db-contact").WillReturnRows(sqlmock.NewRows([]string{"id", "username", "email", "student_id", "role_name"}))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE cgu_users SET email = ? WHERE id = ? AND role_name = 'student'")).
		WithArgs("new-db-contact@example.com", "student-db-contact").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(admissionUpdateSQL)).WithArgs(
		application.Name, "", "new-db-contact@example.com", application.School, "", sqlmock.AnyArg(), application.ID,
	).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	updated, apiError := store.updateAdmission(application.ID, AdmissionApplicationInput{Email: "new-db-contact@example.com"})
	if apiError != nil || updated == nil {
		t.Fatalf("database contact update result = %#v, error = %#v", updated, apiError)
	}
	if store.mailbox[0].ExternalRecipient != "new-db-contact@example.com" {
		t.Fatalf("cached mailbox recipient was not synchronized: %#v", store.mailbox[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAdmissionUpdateProvisionedEmailRejectsLoginIdentifierConflict(t *testing.T) {
	store := NewStoreWithAdminAndDomain(testAdminUsername, testAdminPassword, "students.cgu.edu.kg")
	application := &AdmissionApplication{
		ID: "application-contact-conflict", Name: "冲突申请人", EnglishName: "Conflict Applicant",
		Email: "old-conflict@example.com", School: "至冬学院", Status: "accepted",
		CreatedAt: "2026-08-25T00:00:00Z", UpdatedAt: "2026-08-25T00:00:00Z",
	}
	studentID, username, userID := admissionStudentIdentityForApplication(application)
	application.StudentID = studentID
	student := &User{ID: userID, Username: username, Name: application.Name, Email: application.Email, Role: "student", StudentID: studentID, College: application.School, Year: "2026", PasswordHash: hashPassword("long-password-for-contact-conflict")}
	store.admissions = []*AdmissionApplication{application}
	store.users[userID] = student
	store.users["other-student"] = &User{ID: "other-student", Username: "other-student", Name: "另一个学生", Email: "taken-contact@example.com", Role: "student", StudentID: "CGU-OTHER-2026-GEN-GEN-01-001"}

	updated, apiError := store.updateAdmission(application.ID, AdmissionApplicationInput{Email: "taken-contact@example.com"})
	if updated != nil || apiError == nil || apiError.Status != http.StatusConflict || apiError.Code != "student_exists" {
		t.Fatalf("conflicting contact update result = %#v, error = %#v", updated, apiError)
	}
	if application.Email != "old-conflict@example.com" || student.Email != application.Email {
		t.Fatalf("conflicting contact update mutated state: application=%#v student=%#v", application, student)
	}
}

func TestEnsureStudentContactEmailAvailableTxRejectsLoginIdentifierConflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, username, email, student_id, role_name FROM cgu_users WHERE id <> ? FOR UPDATE")).WithArgs("selected-student").WillReturnRows(sqlmock.NewRows([]string{"id", "username", "email", "student_id", "role_name"}).AddRow("other", "other-login", "taken-contact@example.com", "CGU-OTHER-2026-GEN-GEN-01-001", "student"))
	mock.ExpectRollback()
	if err := ensureStudentContactEmailAvailableTx(context.Background(), tx, "taken-contact@example.com", "selected-student", "students.cgu.edu.kg"); !errors.Is(err, errStudentEmailConflict) {
		t.Fatalf("contact conflict error = %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentAdmissionUpdatesEachRecheckDurableStudentID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.MatchExpectationsInOrder(false)

	const applicationID = "application-update-concurrent"
	provisioned := admissionUpdateRows(applicationID, "已建档申请人", "provisioned@example.com", "综合学院", "accepted", "", "CGU-2026-0009")
	pending := admissionUpdateRows(applicationID, "待建档申请人", "pending@example.com", "综合学院", "pending", "", "")
	for _, rows := range []*sqlmock.Rows{provisioned, pending} {
		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(admissionForUpdateSQL)).WithArgs(applicationID).WillReturnRows(rows)
	}
	mock.ExpectExec(regexp.QuoteMeta(admissionUpdateSQL)).WithArgs(
		"并发修改", "", "concurrent@example.com", "综合学院", "并发备注", sqlmock.AnyArg(), applicationID,
	).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectRollback()

	makeStore := func() *Store {
		store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
		store.db = db
		store.admissions = []*AdmissionApplication{{
			ID: applicationID, Name: "缓存姓名", Email: "cached@example.com", School: "综合学院", Status: "pending",
		}}
		return store
	}
	stores := []*Store{makeStore(), makeStore()}
	input := AdmissionApplicationInput{Name: "并发修改", Email: "concurrent@example.com", School: "综合学院", Notes: "并发备注"}
	start := make(chan struct{})
	results := make(chan *apiError, len(stores))
	var wg sync.WaitGroup
	for _, store := range stores {
		wg.Add(1)
		go func(store *Store) {
			defer wg.Done()
			<-start
			_, apiError := store.updateAdmission(applicationID, input)
			results <- apiError
		}(store)
	}
	close(start)
	wg.Wait()
	close(results)

	conflicts, successes := 0, 0
	for apiError := range results {
		if apiError == nil {
			successes++
			continue
		}
		if apiError.Status == http.StatusConflict && apiError.Code == "admission_already_approved" {
			conflicts++
			continue
		}
		t.Fatalf("unexpected concurrent update error: %#v", apiError)
	}
	if conflicts != 1 || successes != 1 {
		t.Fatalf("concurrent update outcomes = conflicts:%d successes:%d, want one each", conflicts, successes)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
