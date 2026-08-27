package main

import (
	"database/sql"
	"net/http"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func admissionDeleteRows(id, name, email, school, status, studentID string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "name_text", "english_name", "email", "school_text", "status_name", "notes_text",
		"created_at", "updated_at", "student_id", "approved_at", "approved_by", "initial_password_issued_at",
	}).AddRow(id, name, "", email, school, status, "", "2026-08-25T00:00:00Z", "2026-08-25T00:00:00Z", studentID, "", "", "")
}

func studentDeleteRows(id, username, name, email, studentID string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "username", "name_text", "email", "role_name", "password_hash", "student_id", "college", "year_text", "disabled_flag",
	}).AddRow(id, username, name, email, "student", "bcrypt$hash", studentID, "综合学院", "2026", false)
}

func expectAcademicStudentDeletes(mock sqlmock.Sqlmock, userID, studentID string) {
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_mailbox_messages WHERE recipient_id = ? OR recipient_id = ?")).WithArgs(userID, studentID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_enrollments WHERE student_id = ? OR student_id = ?")).WithArgs(userID, studentID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_grades WHERE student_id = ? OR student_id = ?")).WithArgs(userID, studentID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_schedule WHERE student_id = ? OR student_id = ?")).WithArgs(userID, studentID).WillReturnResult(sqlmock.NewResult(0, 1))
}

func TestDeleteAdmissionRemovesProvisionedStudentInMemory(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	application, apiError := store.createAdmission(AdmissionApplicationInput{Name: "已录取申请", Email: "provisioned@example.com", School: "至冬学院"})
	if apiError != nil {
		t.Fatalf("create admission: %v", apiError)
	}
	student := &User{ID: "student-delete-memory", Username: "delete-memory", Name: "已录取学生", Email: "provisioned@example.com", Role: "student", StudentID: "CGU-MEMORY-001"}
	store.users[student.ID] = student
	stored := store.admissions[0]
	stored.Status = "accepted"
	stored.StudentID = student.StudentID
	store.enrollments = append(store.enrollments, &Enrollment{ID: "enrollment-delete-memory", StudentID: student.ID})
	store.grades = append(store.grades, &Grade{ID: "grade-delete-memory", StudentID: student.ID})
	store.schedule = append(store.schedule, &ScheduleEntry{ID: "schedule-delete-memory", StudentID: student.ID})
	store.mailbox = append(store.mailbox, &MailboxMessage{ID: "mail-delete-memory", RecipientID: student.ID, RecipientStudentID: student.StudentID, RequestKey: admissionApprovalRequestKey(application.ID)})
	result, apiError := store.deleteAdmissionCascade(application.ID)
	if apiError != nil || result == nil || result.Student == nil || result.Student.ID != student.ID {
		t.Fatalf("provisioned admission delete result = %#v, error = %#v", result, apiError)
	}
	if _, ok := store.users[student.ID]; ok || len(store.admissions) != 0 || len(store.notifications) != 0 || len(store.enrollments) != 0 || len(store.grades) != 0 || len(store.schedule) != 0 || len(store.mailbox) != 0 {
		t.Fatalf("cascade deletion left records: users=%d admissions=%d notifications=%d enrollments=%d grades=%d schedule=%d mailbox=%d", len(store.users), len(store.admissions), len(store.notifications), len(store.enrollments), len(store.grades), len(store.schedule), len(store.mailbox))
	}
}

func TestDeleteAdmissionRejectsGeneratedAdministratorCollisionInMemory(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	applicationID := "application-delete-role-collision"
	studentID, _, generatedUserID := admissionStudentIdentity(applicationID)
	application := &AdmissionApplication{
		ID: applicationID, Name: "角色冲突申请", Email: "role-collision@example.com",
		School: "综合学院", Status: "accepted", StudentID: studentID,
	}
	store.admissions = []*AdmissionApplication{application}
	// A corrupted cache can contain an administrator under the deterministic
	// generated account id. It must never be selected as the student to delete.
	admin := &User{ID: generatedUserID, Username: "collision-admin", Name: "教务管理员", Role: "admin", StudentID: studentID}
	store.users[generatedUserID] = admin
	store.grades = append(store.grades, &Grade{ID: "grade-role-collision", StudentID: generatedUserID})

	deleted, apiError := store.deleteAdmissionCascade(applicationID)
	if deleted != nil || apiError == nil || apiError.Status != http.StatusConflict || apiError.Code != "student_id_ambiguous" {
		t.Fatalf("generated administrator collision delete = %#v, error = %#v", deleted, apiError)
	}
	if len(store.admissions) != 1 || store.users[generatedUserID] != admin || len(store.grades) != 1 {
		t.Fatalf("role collision mutated cache: admissions=%d user=%#v grades=%d", len(store.admissions), store.users[generatedUserID], len(store.grades))
	}
}

func TestDeleteAdmissionPrefersGeneratedAccountWhenStudentIDsDuplicate(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	application, apiError := store.createAdmission(AdmissionApplicationInput{Name: "重复学号申请", Email: "duplicate@example.com", School: "至冬学院"})
	if apiError != nil || application == nil {
		t.Fatalf("create duplicate admission = %#v, error = %#v", application, apiError)
	}
	generatedStudentID, _, generatedUserID := admissionStudentIdentity(application.ID)
	stored := store.admissions[0]
	stored.Status, stored.StudentID = "accepted", generatedStudentID
	generated := &User{ID: generatedUserID, Username: "generated-duplicate", Name: "录取账号", Email: "generated@example.com", Role: "student", StudentID: generatedStudentID}
	legacy := &User{ID: "legacy-duplicate", Username: "legacy-duplicate", Name: "旧账号", Email: "legacy@example.com", Role: "student", StudentID: generatedStudentID}
	store.users[generated.ID], store.users[legacy.ID] = generated, legacy
	deleted, apiError := store.deleteAdmissionCascade(application.ID)
	if apiError != nil || deleted == nil || deleted.Student == nil || deleted.Student.ID != generated.ID {
		t.Fatalf("duplicate admission deletion = %#v, error = %#v", deleted, apiError)
	}
	if _, exists := store.users[generated.ID]; exists {
		t.Fatal("generated account was not removed")
	}
	if _, exists := store.users[legacy.ID]; !exists {
		t.Fatal("legacy duplicate account was removed")
	}
}

func TestDeleteAdmissionPersistsPendingApplicationAtomically(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	store.db = db
	application := &AdmissionApplication{ID: "application-delete-db", Name: "数据库删除申请", Email: "db-delete@example.com", School: "综合学院", Status: "pending"}
	notification := &AdminNotification{ID: "notification-delete-db", RecipientID: "admin", ReferenceID: application.ID}
	store.admissions = []*AdmissionApplication{application}
	store.notifications = []*AdminNotification{notification}
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(admissionForUpdateSQL)).WithArgs(application.ID).WillReturnRows(admissionDeleteRows(application.ID, application.Name, application.Email, application.School, application.Status, ""))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_admin_notifications WHERE reference_id = ?")).WithArgs(application.ID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_mailbox_messages WHERE request_key = ?")).WithArgs(admissionApprovalRequestKey(application.ID)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_admissions WHERE id = ?")).WithArgs(application.ID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	deleted, apiError := store.deleteAdmissionCascade(application.ID)
	if apiError != nil || deleted == nil || deleted.Application.ID != application.ID || deleted.Student != nil {
		t.Fatalf("database delete result = %#v, error = %#v", deleted, apiError)
	}
	if len(store.admissions) != 0 || len(store.notifications) != 0 {
		t.Fatalf("database delete left in-memory rows: admissions=%d notifications=%d", len(store.admissions), len(store.notifications))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteAdmissionCascadePersistsProvisionedRecordsAtomically(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	store.db = db
	application := &AdmissionApplication{ID: "application-delete-provisioned", Name: "已建档申请", Email: "cascade@example.com", School: "综合学院", Status: "accepted", StudentID: "CGU-DELETE-001"}
	student := &User{ID: "student-delete-db", Username: "student-delete-db", Name: application.Name, Email: application.Email, Role: "student", StudentID: application.StudentID}
	store.admissions = []*AdmissionApplication{application}
	store.users[student.ID] = student
	store.notifications = []*AdminNotification{{ID: "notification-delete-provisioned", RecipientID: "admin", ReferenceID: application.ID}}
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(admissionForUpdateSQL)).WithArgs(application.ID).WillReturnRows(admissionDeleteRows(application.ID, application.Name, application.Email, application.School, application.Status, application.StudentID))
	_, _, deterministicUserID := admissionStudentIdentity(application.ID)
	mock.ExpectQuery(regexp.QuoteMeta(studentForAdmissionDeleteSQL)).WithArgs(deterministicUserID, application.StudentID, application.StudentID, deterministicUserID, application.StudentID).WillReturnRows(studentDeleteRows(student.ID, student.Username, student.Name, student.Email, student.StudentID))
	mock.ExpectQuery(regexp.QuoteMeta(usersForStudentIDDeleteSQL)).WithArgs(student.StudentID).WillReturnRows(sqlmock.NewRows([]string{"id", "role_name"}).AddRow(student.ID, "student"))
	mock.ExpectQuery(regexp.QuoteMeta(usersForStudentReferenceWithExternalDeleteSQL)).WithArgs(student.ID, student.StudentID, student.ID, student.StudentID).WillReturnRows(sqlmock.NewRows([]string{"id", "student_id", "role_name"}).AddRow(student.ID, student.StudentID, "student"))
	mock.ExpectQuery(regexp.QuoteMeta(admissionsForStudentByBothRefsSQL)).WithArgs(student.StudentID, student.ID).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(application.ID))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_admin_notifications WHERE reference_id = ?")).WithArgs(application.ID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_mailbox_messages WHERE request_key = ?")).WithArgs(admissionApprovalRequestKey(application.ID)).WillReturnResult(sqlmock.NewResult(0, 1))
	expectAcademicStudentDeletes(mock, student.ID, student.StudentID)
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_admissions WHERE id = ?")).WithArgs(application.ID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_users WHERE id = ? AND role_name = 'student'")).WithArgs(student.ID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	deleted, apiError := store.deleteAdmissionCascade(application.ID)
	if apiError != nil || deleted == nil || deleted.Student == nil || deleted.Student.ID != student.ID {
		t.Fatalf("provisioned database delete result = %#v, error = %#v", deleted, apiError)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteAdmissionWithoutStudentOnlyRemovesApplicationProjections(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	store.db = db
	application := &AdmissionApplication{ID: "application-delete-orphan", Name: "历史孤立申请", Email: "orphan@example.com", School: "综合学院", Status: "accepted", StudentID: "LEGACY-ORPHAN-001"}
	store.admissions = []*AdmissionApplication{application}
	_, _, deterministicUserID := admissionStudentIdentity(application.ID)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(admissionForUpdateSQL)).WithArgs(application.ID).
		WillReturnRows(admissionDeleteRows(application.ID, application.Name, application.Email, application.School, application.Status, application.StudentID))
	mock.ExpectQuery(regexp.QuoteMeta(studentForAdmissionDeleteSQL)).
		WithArgs(deterministicUserID, application.StudentID, application.StudentID, deterministicUserID, application.StudentID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "name_text", "email", "role_name", "password_hash", "student_id", "college", "year_text", "disabled_flag"}))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_admin_notifications WHERE reference_id = ?")).WithArgs(application.ID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_mailbox_messages WHERE request_key = ?")).WithArgs(admissionApprovalRequestKey(application.ID)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_admissions WHERE id = ?")).WithArgs(application.ID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	deleted, apiError := store.deleteAdmissionCascade(application.ID)
	if apiError != nil || deleted == nil || deleted.Student != nil || deleted.Application.ID != application.ID {
		t.Fatalf("orphan admission delete result = %#v, error = %#v", deleted, apiError)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteAdmissionRejectsProvisionedCrossFieldReferenceCollision(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	store.db = db
	application := &AdmissionApplication{
		ID:        "application-delete-cross-field",
		Name:      "交叉引用申请",
		Email:     "cross-field@example.com",
		School:    "综合学院",
		Status:    "accepted",
		StudentID: "CGU-CROSS-FIELD-001",
	}
	store.admissions = []*AdmissionApplication{application}
	_, _, deterministicUserID := admissionStudentIdentity(application.ID)
	student := &User{ID: deterministicUserID, Username: "cross-field-student", Name: application.Name, Email: application.Email, Role: "student", StudentID: application.StudentID}
	store.users[student.ID] = student

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(admissionForUpdateSQL)).WithArgs(application.ID).
		WillReturnRows(admissionDeleteRows(application.ID, application.Name, application.Email, application.School, application.Status, application.StudentID))
	mock.ExpectQuery(regexp.QuoteMeta(studentForAdmissionDeleteSQL)).
		WithArgs(deterministicUserID, application.StudentID, application.StudentID, deterministicUserID, application.StudentID).
		WillReturnRows(studentDeleteRows(student.ID, student.Username, student.Name, student.Email, student.StudentID))
	mock.ExpectQuery(regexp.QuoteMeta(usersForStudentIDDeleteSQL)).WithArgs(student.StudentID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "role_name"}).AddRow(student.ID, "student"))
	// The canonical external number is also another account's primary key.
	// The ownership guard must reject the cascade before any projection DELETE.
	mock.ExpectQuery(regexp.QuoteMeta(usersForStudentReferenceWithExternalDeleteSQL)).
		WithArgs(student.ID, student.StudentID, student.ID, student.StudentID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "student_id", "role_name"}).
			AddRow(student.ID, student.StudentID, "student").
			AddRow(student.StudentID, "legacy-admin-reference", "admin"))
	mock.ExpectRollback()

	deleted, apiError := store.deleteAdmissionCascade(application.ID)
	if deleted != nil || apiError == nil || apiError.Status != http.StatusConflict || apiError.Code != "student_id_ambiguous" {
		t.Fatalf("provisioned cross-field collision delete = %#v, error = %#v", deleted, apiError)
	}
	if len(store.admissions) != 1 || store.users[student.ID] == nil {
		t.Fatalf("cross-field collision mutated cache: admissions=%d student=%v", len(store.admissions), store.users[student.ID])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteAdmissionRollsBackOnDatabaseFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	store.db = db
	application := &AdmissionApplication{ID: "application-delete-rollback", Name: "回滚删除申请", Email: "rollback-delete@example.com", School: "综合学院", Status: "pending"}
	notification := &AdminNotification{ID: "notification-delete-rollback", RecipientID: "admin", ReferenceID: application.ID}
	store.admissions = []*AdmissionApplication{application}
	store.notifications = []*AdminNotification{notification}
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(admissionForUpdateSQL)).WithArgs(application.ID).WillReturnRows(admissionDeleteRows(application.ID, application.Name, application.Email, application.School, application.Status, ""))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_admin_notifications WHERE reference_id = ?")).WithArgs(application.ID).WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()
	deleted, apiError := store.deleteAdmissionCascade(application.ID)
	if deleted != nil || apiError == nil || apiError.Status != http.StatusServiceUnavailable || len(store.admissions) != 1 || len(store.notifications) != 1 {
		t.Fatalf("rollback delete result = %#v, error = %#v, admissions=%d notifications=%d", deleted, apiError, len(store.admissions), len(store.notifications))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteAdmissionRechecksDurableApplication(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	store.db = db
	application := &AdmissionApplication{ID: "application-delete-missing", Name: "缓存申请", Email: "cache@example.com", School: "综合学院", Status: "pending"}
	store.admissions = []*AdmissionApplication{application}
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(admissionForUpdateSQL)).WithArgs(application.ID).WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	deleted, apiError := store.deleteAdmissionCascade(application.ID)
	if deleted != nil || apiError == nil || apiError.Status != http.StatusNotFound || apiError.Code != "admission_not_found" {
		t.Fatalf("durable missing delete result = %#v, error = %#v", deleted, apiError)
	}
	if len(store.admissions) != 1 {
		t.Fatalf("stale cache was mutated: %d admissions", len(store.admissions))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteAdmissionRejectsDuplicateLegacyStudentIDs(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	store.db = db
	application := &AdmissionApplication{ID: "application-delete-duplicate", Name: "重复学号申请", Email: "duplicate-delete@example.com", School: "综合学院", Status: "accepted", StudentID: "LEGACY-STUDENT-001"}
	store.admissions = []*AdmissionApplication{application}
	_, _, deterministicUserID := admissionStudentIdentity(application.ID)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(admissionForUpdateSQL)).WithArgs(application.ID).
		WillReturnRows(admissionDeleteRows(application.ID, application.Name, application.Email, application.School, application.Status, application.StudentID))
	mock.ExpectQuery(regexp.QuoteMeta(studentForAdmissionDeleteSQL)).
		WithArgs(deterministicUserID, application.StudentID, application.StudentID, deterministicUserID, application.StudentID).
		WillReturnRows(studentDeleteRows("legacy-delete-a", "legacy-delete-a", "旧账号甲", "legacy-a@example.com", application.StudentID).
			AddRow("legacy-delete-b", "legacy-delete-b", "旧账号乙", "legacy-b@example.com", "student", "bcrypt$hash", application.StudentID, "综合学院", "2026", false))
	mock.ExpectRollback()
	deleted, apiError := store.deleteAdmissionCascade(application.ID)
	if deleted != nil || apiError == nil || apiError.Status != http.StatusConflict || apiError.Code != "student_id_ambiguous" {
		t.Fatalf("duplicate legacy admission delete = %#v, error = %#v", deleted, apiError)
	}
	if len(store.admissions) != 1 {
		t.Fatalf("ambiguous delete mutated cache: %d admissions", len(store.admissions))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteAdmissionRejectsDeterministicStudentIDMismatch(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	store.db = db
	application := &AdmissionApplication{
		ID:        "application-delete-identity-mismatch",
		Name:      "学号不一致申请",
		Email:     "identity-mismatch@example.com",
		School:    "综合学院",
		Status:    "accepted",
		StudentID: "CGU-APPLICATION-001",
	}
	store.admissions = []*AdmissionApplication{application}
	_, _, deterministicUserID := admissionStudentIdentity(application.ID)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(admissionForUpdateSQL)).WithArgs(application.ID).
		WillReturnRows(admissionDeleteRows(application.ID, application.Name, application.Email, application.School, application.Status, application.StudentID))
	mock.ExpectQuery(regexp.QuoteMeta(studentForAdmissionDeleteSQL)).
		WithArgs(deterministicUserID, application.StudentID, application.StudentID, deterministicUserID, application.StudentID).
		WillReturnRows(studentDeleteRows(deterministicUserID, "generated-mismatch", application.Name, application.Email, "CGU-DIFFERENT-999"))
	mock.ExpectRollback()
	deleted, apiError := store.deleteAdmissionCascade(application.ID)
	if deleted != nil || apiError == nil || apiError.Status != http.StatusConflict || apiError.Code != "student_identity_mismatch" {
		t.Fatalf("identity-mismatch admission delete = %#v, error = %#v", deleted, apiError)
	}
	if len(store.admissions) != 1 {
		t.Fatalf("identity-mismatch delete mutated cache: %d admissions", len(store.admissions))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteAdmissionSupportsLegacyPrimaryKeyAndCanonicalReferences(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	store.db = db
	application := &AdmissionApplication{ID: "application-delete-legacy-key", Name: "旧主键申请", Email: "legacy-key@example.com", School: "综合学院", Status: "accepted", StudentID: "legacy-user-key"}
	student := &User{ID: application.StudentID, Username: "legacy-key-user", Name: application.Name, Email: application.Email, Role: "student", StudentID: "CANONICAL-STUDENT-001"}
	store.admissions = []*AdmissionApplication{application}
	store.users[student.ID] = student
	_, _, deterministicUserID := admissionStudentIdentity(application.ID)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(admissionForUpdateSQL)).WithArgs(application.ID).
		WillReturnRows(admissionDeleteRows(application.ID, application.Name, application.Email, application.School, application.Status, application.StudentID))
	mock.ExpectQuery(regexp.QuoteMeta(studentForAdmissionDeleteSQL)).
		WithArgs(deterministicUserID, application.StudentID, application.StudentID, deterministicUserID, application.StudentID).
		WillReturnRows(studentDeleteRows(student.ID, student.Username, student.Name, student.Email, student.StudentID))
	mock.ExpectQuery(regexp.QuoteMeta(usersForStudentIDDeleteSQL)).WithArgs(student.StudentID).WillReturnRows(sqlmock.NewRows([]string{"id", "role_name"}).AddRow(student.ID, "student"))
	mock.ExpectQuery(regexp.QuoteMeta(usersForStudentReferenceWithExternalDeleteSQL)).WithArgs(student.ID, student.StudentID, student.ID, student.StudentID).WillReturnRows(sqlmock.NewRows([]string{"id", "student_id", "role_name"}).AddRow(student.ID, student.StudentID, "student"))
	mock.ExpectQuery(regexp.QuoteMeta(admissionsForStudentByBothRefsSQL)).
		WithArgs(application.StudentID, student.StudentID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(application.ID))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_admin_notifications WHERE reference_id = ?")).WithArgs(application.ID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_mailbox_messages WHERE request_key = ?")).WithArgs(admissionApprovalRequestKey(application.ID)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_mailbox_messages WHERE recipient_id = ? OR recipient_id = ?")).WithArgs(student.ID, student.StudentID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_enrollments WHERE student_id = ? OR student_id = ?")).WithArgs(student.ID, student.StudentID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_grades WHERE student_id = ? OR student_id = ?")).WithArgs(student.ID, student.StudentID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_schedule WHERE student_id = ? OR student_id = ?")).WithArgs(student.ID, student.StudentID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_admissions WHERE id = ?")).WithArgs(application.ID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_users WHERE id = ? AND role_name = 'student'")).WithArgs(student.ID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	deleted, apiError := store.deleteAdmissionCascade(application.ID)
	if apiError != nil || deleted == nil || deleted.Student == nil || deleted.Student.ID != student.ID {
		t.Fatalf("legacy primary-key admission delete = %#v, error = %#v", deleted, apiError)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
