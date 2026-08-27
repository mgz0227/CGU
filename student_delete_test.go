package main

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestDeleteStudentCascadeRemovesInMemoryProjections(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	student := &User{ID: "student-memory-delete", Username: "memory-delete", Name: "待删除学生", Email: "memory-delete@example.com", Role: "student", StudentID: "CGU-MEMORY-DELETE"}
	store.users[student.ID] = student
	application := &AdmissionApplication{ID: "admission-memory-delete", Name: student.Name, Email: student.Email, School: "至冬学院", Status: "accepted", StudentID: student.StudentID}
	store.admissions = append(store.admissions, application)
	store.notifications = append(store.notifications, &AdminNotification{ID: "notification-memory-delete", ReferenceID: application.ID})
	store.mailbox = append(store.mailbox,
		&MailboxMessage{ID: "mail-memory-delete", RecipientID: student.ID, RecipientStudentID: student.StudentID},
		&MailboxMessage{ID: "mail-admission-delete", RequestKey: admissionApprovalRequestKey(application.ID)},
	)
	store.enrollments = append(store.enrollments, &Enrollment{ID: "enrollment-memory-delete", StudentID: student.ID})
	store.grades = append(store.grades, &Grade{ID: "grade-memory-delete", StudentID: student.StudentID})
	store.schedule = append(store.schedule, &ScheduleEntry{ID: "schedule-memory-delete", StudentID: student.ID})

	result, apiError := store.deleteStudent(student.ID)
	if apiError != nil || result == nil || result.Student.ID != student.ID {
		t.Fatalf("delete student result = %#v, error = %#v", result, apiError)
	}
	if len(result.AdmissionIDs) != 1 || result.AdmissionIDs[0] != application.ID {
		t.Fatalf("deleted admission ids = %#v", result.AdmissionIDs)
	}
	if _, ok := store.users[student.ID]; ok || len(store.admissions) != 0 || len(store.notifications) != 0 || len(store.mailbox) != 0 || len(store.enrollments) != 0 || len(store.grades) != 0 || len(store.schedule) != 0 {
		t.Fatalf("cascade left records: users=%d admissions=%d notifications=%d mailbox=%d enrollments=%d grades=%d schedule=%d", len(store.users), len(store.admissions), len(store.notifications), len(store.mailbox), len(store.enrollments), len(store.grades), len(store.schedule))
	}
	if _, apiError := store.deleteStudent("admin"); apiError == nil || apiError.Status != http.StatusForbidden {
		t.Fatalf("administrator deletion error = %#v", apiError)
	}
}

func TestDeleteStudentRejectsDuplicateExternalIDsInMemory(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	first := &User{ID: "student-duplicate-a", Username: "duplicate-a", Name: "重复甲", Email: "duplicate-a@example.com", Role: "student", StudentID: "CGU-DUPLICATE-001"}
	second := &User{ID: "student-duplicate-b", Username: "duplicate-b", Name: "重复乙", Email: "duplicate-b@example.com", Role: "student", StudentID: first.StudentID}
	store.users[first.ID], store.users[second.ID] = first, second
	result, apiError := store.deleteStudent(first.ID)
	if result != nil || apiError == nil || apiError.Status != http.StatusConflict || apiError.Code != "student_id_ambiguous" {
		t.Fatalf("duplicate in-memory student delete = %#v, error = %#v", result, apiError)
	}
	if store.users[first.ID] == nil || store.users[second.ID] == nil {
		t.Fatal("ambiguous in-memory delete removed an account")
	}
}

func TestDeleteStudentRejectsCrossFieldIdentifierCollisionInMemory(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	target := &User{ID: "student-cross-target", Username: "cross-target", Name: "目标学生", Role: "student", StudentID: "CGU-CROSS-001"}
	other := &User{ID: target.StudentID, Username: "cross-other", Name: "交叉学生", Role: "student", StudentID: "CGU-CROSS-002"}
	store.users[target.ID] = target
	store.users[other.ID] = other
	result, apiError := store.deleteStudent(target.ID)
	if result != nil || apiError == nil || apiError.Status != http.StatusConflict || apiError.Code != "student_id_ambiguous" {
		t.Fatalf("cross-field collision result=%#v error=%#v", result, apiError)
	}
	if store.users[target.ID] == nil || store.users[other.ID] == nil {
		t.Fatal("cross-field collision removed an account")
	}
}

func TestDeleteStudentRejectsNonStudentReferenceCollisionInMemory(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	target := &User{ID: "student-cross-admin", Username: "cross-admin-target", Name: "目标学生", Role: "student", StudentID: "CGU-CROSS-ADMIN"}
	other := &User{ID: "registrar-cross-admin", Username: "registrar-cross-admin", Name: "教务账号", Role: "admin", StudentID: target.StudentID}
	store.users[target.ID] = target
	store.users[other.ID] = other
	result, apiError := store.deleteStudent(target.ID)
	if result != nil || apiError == nil || apiError.Status != http.StatusConflict || apiError.Code != "student_id_ambiguous" {
		t.Fatalf("non-student collision result=%#v error=%#v", result, apiError)
	}
}

func TestDeleteStudentRejectsCaseFoldedPrimaryIDCollisionInMemory(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	target := &User{ID: "Student-Case-Collision", Username: "case-collision-target", Name: "目标学生", Role: "student", StudentID: "CGU-CASE-COLLISION-001"}
	other := &User{ID: "student-case-collision", Username: "case-collision-other", Name: "冲突学生", Role: "student", StudentID: "CGU-CASE-COLLISION-002"}
	store.users[target.ID] = target
	store.users[other.ID] = other

	result, apiError := store.deleteStudent(target.ID)
	if result != nil || apiError == nil || apiError.Status != http.StatusConflict || apiError.Code != "student_id_ambiguous" {
		t.Fatalf("case-folded primary-id collision result=%#v error=%#v", result, apiError)
	}
	if store.users[target.ID] == nil || store.users[other.ID] == nil {
		t.Fatal("case-folded primary-id collision removed an account")
	}
}

func TestAdmissionDeleteRejectsGeneratedAdministratorCollisionInMemory(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	application := &AdmissionApplication{
		ID: "admission-admin-collision", Name: "冲突申请人", EnglishName: "Admin Collision",
		Email: "admin-collision@example.com", School: "至冬学院", Status: "accepted",
		StudentID: "CGU-ADMIN-COLLISION-2026", CreatedAt: "2026-08-25T00:00:00Z",
	}
	_, _, generatedID := admissionStudentIdentity(application.ID)
	store.admissions = []*AdmissionApplication{application}
	store.users[generatedID] = &User{
		ID: generatedID, Username: "administrator-collision", Name: "教务管理员", Role: "admin",
		StudentID: application.StudentID,
	}
	result, apiError := store.deleteAdmissionCascade(application.ID)
	if result != nil || apiError == nil || apiError.Status != http.StatusConflict || apiError.Code != "student_id_ambiguous" {
		t.Fatalf("administrator collision admission delete = %#v, error = %#v", result, apiError)
	}
	if store.admissions[0] == nil || store.users[generatedID] == nil {
		t.Fatal("administrator collision was not left untouched")
	}
}

func TestDeleteStudentAcceptsCaseInsensitiveStudentRoleInMemory(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	student := &User{ID: "student-case-role", Username: "case-role", Name: "大小写角色", Email: "case-role@example.com", Role: "Student", StudentID: "CGU-CASE-ROLE"}
	store.users[student.ID] = student
	result, apiError := store.deleteStudent(student.ID)
	if apiError != nil || result == nil || result.Student.ID != student.ID {
		t.Fatalf("case-insensitive role delete = %#v, error = %#v", result, apiError)
	}
	if store.users[student.ID] != nil {
		t.Fatal("case-insensitive role student was not removed")
	}
}

func TestDeleteStudentHTTPRevokesSessionsAndRequiresAdmin(t *testing.T) {
	store := NewStoreWithAdminAndDomain(testAdminUsername, testAdminPassword, "students.cgu.edu.kg")
	student, apiError := store.createStudent(StudentInput{
		Username: "http-delete-student", Name: "HTTP 删除学生", Email: "http-delete@example.com", StudentID: "CGU-HTTP-DELETE", Password: "http-delete-password-2026!",
	})
	if apiError != nil || student == nil {
		t.Fatalf("create student = %#v, error = %#v", student, apiError)
	}
	application, apiError := store.createAdmission(AdmissionApplicationInput{Name: student.Name, Email: student.Email, School: "综合学院"})
	if apiError != nil || application == nil {
		t.Fatalf("create application = %#v, error = %#v", application, apiError)
	}
	application.Status, application.StudentID = "accepted", student.StudentID
	store.enrollments = append(store.enrollments, &Enrollment{ID: "http-delete-enrollment", StudentID: student.ID})
	store.grades = append(store.grades, &Grade{ID: "http-delete-grade", StudentID: student.StudentID})
	store.schedule = append(store.schedule, &ScheduleEntry{ID: "http-delete-schedule", StudentID: student.ID})
	store.mailbox = append(store.mailbox, &MailboxMessage{ID: "http-delete-mail", RecipientID: student.ID, RecipientStudentID: student.StudentID, RequestKey: admissionApprovalRequestKey(application.ID)})

	server := httptest.NewServer(NewServer(store, "web"))
	defer server.Close()
	adminJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	adminClient := &http.Client{Jar: adminJar}
	login := postJSON(t, adminClient, server.URL+"/api/auth/login", map[string]string{"username": testAdminUsername, "password": testAdminPassword})
	if login.StatusCode != http.StatusOK {
		t.Fatalf("admin login status = %d", login.StatusCode)
	}
	login.Body.Close()
	studentJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	studentClient := &http.Client{Jar: studentJar}
	studentLogin := postJSON(t, studentClient, server.URL+"/api/auth/login", map[string]string{"username": student.Username, "password": "http-delete-password-2026!"})
	if studentLogin.StatusCode != http.StatusOK {
		t.Fatalf("student login status = %d", studentLogin.StatusCode)
	}
	studentLogin.Body.Close()

	deleted := doJSON(t, adminClient, http.MethodDelete, server.URL+"/api/admin/students/"+url.PathEscape(student.ID), nil)
	if deleted.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(deleted.Body)
		deleted.Body.Close()
		t.Fatalf("student delete status = %d, body=%s", deleted.StatusCode, raw)
	}
	var payload map[string]any
	if err := json.NewDecoder(deleted.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	deleted.Body.Close()
	if payload["ok"] != true || strings.Contains(strings.ToLower(string(mustJSON(t, payload))), "passwordhash") {
		t.Fatalf("unexpected deletion response: %#v", payload)
	}
	me, err := studentClient.Get(server.URL + "/api/auth/me")
	if err != nil {
		t.Fatal(err)
	}
	if me.StatusCode != http.StatusUnauthorized {
		t.Fatalf("deleted student session status = %d", me.StatusCode)
	}
	me.Body.Close()
	newLogin := postJSON(t, &http.Client{}, server.URL+"/api/auth/login", map[string]string{"username": student.Username, "password": "http-delete-password-2026!"})
	if newLogin.StatusCode != http.StatusUnauthorized {
		t.Fatalf("deleted student login status = %d", newLogin.StatusCode)
	}
	newLogin.Body.Close()
	students := doJSON(t, adminClient, http.MethodGet, server.URL+"/api/admin/students", nil)
	if students.StatusCode != http.StatusOK {
		t.Fatalf("student directory after delete status = %d", students.StatusCode)
	}
	students.Body.Close()

	anonymous, err := http.NewRequest(http.MethodDelete, server.URL+"/api/admin/students/"+url.PathEscape(student.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	anonymous.Header.Set("X-CGU-Request", "1")
	response, err := (&http.Client{}).Do(anonymous)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous student delete status = %d", response.StatusCode)
	}
	response.Body.Close()
}

func TestDeleteStudentPersistsAllRowsAtomically(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	store.db = db
	student := &User{ID: "student-db-delete", Username: "db-delete", Name: "数据库删除学生", Email: "db-delete@example.com", Role: "student", StudentID: "CGU-DB-DELETE"}
	store.users[student.ID] = student
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(userForDeleteSQL)).WithArgs(student.ID).WillReturnRows(studentDeleteRows(student.ID, student.Username, student.Name, student.Email, student.StudentID))
	mock.ExpectQuery(regexp.QuoteMeta(usersForStudentReferenceWithExternalDeleteSQL)).WithArgs(student.ID, student.StudentID, student.ID, student.StudentID).WillReturnRows(sqlmock.NewRows([]string{"id", "student_id", "role_name"}).AddRow(student.ID, student.StudentID, "student"))
	mock.ExpectQuery(regexp.QuoteMeta(admissionsForStudentByBothRefsSQL)).WithArgs(student.StudentID, student.ID).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("admission-db-delete"))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_admin_notifications WHERE reference_id = ?")).WithArgs("admission-db-delete").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_mailbox_messages WHERE request_key = ?")).WithArgs(admissionApprovalRequestKey("admission-db-delete")).WillReturnResult(sqlmock.NewResult(0, 1))
	expectAcademicStudentDeletes(mock, student.ID, student.StudentID)
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_admissions WHERE id = ?")).WithArgs("admission-db-delete").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_users WHERE id = ? AND role_name = 'student'")).WithArgs(student.ID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	result, apiError := store.deleteStudent(student.ID)
	if apiError != nil || result == nil || result.Student.ID != student.ID {
		t.Fatalf("database student delete result = %#v, error = %#v", result, apiError)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteStudentDatabaseRejectsCrossFieldIdentifierCollision(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	store.db = db
	target := &User{ID: "student-db-cross", Username: "db-cross", Name: "数据库目标", Role: "student", StudentID: "CGU-DB-CROSS"}
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(userForDeleteSQL)).WithArgs(target.ID).WillReturnRows(studentDeleteRows(target.ID, target.Username, target.Name, "db-cross@example.com", target.StudentID))
	mock.ExpectQuery(regexp.QuoteMeta(usersForStudentReferenceWithExternalDeleteSQL)).WithArgs(target.ID, target.StudentID, target.ID, target.StudentID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "student_id", "role_name"}).
			AddRow(target.ID, target.StudentID, "student").
			AddRow(target.StudentID, "CGU-DB-OTHER", "student"))
	mock.ExpectRollback()
	result, apiError := store.deleteStudent(target.ID)
	if result != nil || apiError == nil || apiError.Status != http.StatusConflict || apiError.Code != "student_id_ambiguous" {
		t.Fatalf("database cross-field collision result=%#v error=%#v", result, apiError)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteStudentDatabaseRejectsCaseFoldedPrimaryCollision(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	store.db = db
	target := &User{ID: "student-db-casefold", Username: "db-casefold", Name: "大小写目标", Role: "student", StudentID: "CGU-DB-CASEFOLD"}
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(userForDeleteSQL)).WithArgs(target.ID).
		WillReturnRows(studentDeleteRows(target.ID, target.Username, target.Name, "casefold@example.com", target.StudentID))
	mock.ExpectQuery(regexp.QuoteMeta(usersForStudentReferenceWithExternalDeleteSQL)).
		WithArgs(target.ID, target.StudentID, target.ID, target.StudentID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "student_id", "role_name"}).
			AddRow(target.ID, target.StudentID, "student").
			AddRow(strings.ToUpper(target.ID), "legacy-casefold", "student"))
	mock.ExpectRollback()
	result, apiError := store.deleteStudent(target.ID)
	if result != nil || apiError == nil || apiError.Status != http.StatusConflict || apiError.Code != "student_id_ambiguous" {
		t.Fatalf("database case-folded primary collision result=%#v error=%#v", result, apiError)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteStudentWithoutExternalIDDoesNotLockPendingAdmissions(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	store.db = db
	student := &User{ID: "student-db-no-external", Username: "db-no-external", Name: "无外部学号", Email: "no-external@example.com", Role: "student"}
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(userForDeleteSQL)).WithArgs(student.ID).WillReturnRows(studentDeleteRows(student.ID, student.Username, student.Name, student.Email, ""))
	mock.ExpectQuery(regexp.QuoteMeta(usersForStudentReferenceDeleteSQL)).WithArgs(student.ID, student.ID, student.ID).WillReturnRows(sqlmock.NewRows([]string{"id", "student_id", "role_name"}).AddRow(student.ID, "", "student"))
	mock.ExpectQuery(regexp.QuoteMeta(admissionsForStudentByOneRefSQL)).WithArgs(student.ID).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_mailbox_messages WHERE recipient_id = ?")).WithArgs(student.ID).WillReturnResult(sqlmock.NewResult(0, 0))
	for _, statement := range []string{"DELETE FROM cgu_enrollments WHERE student_id = ?", "DELETE FROM cgu_grades WHERE student_id = ?", "DELETE FROM cgu_schedule WHERE student_id = ?"} {
		mock.ExpectExec(regexp.QuoteMeta(statement)).WithArgs(student.ID).WillReturnResult(sqlmock.NewResult(0, 0))
	}
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_users WHERE id = ? AND role_name = 'student'")).WithArgs(student.ID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if result, apiError := store.deleteStudent(student.ID); apiError != nil || result == nil {
		t.Fatalf("student without external id delete = %#v, error = %#v", result, apiError)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteStudentDatabaseRollbackAndAdminProtection(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	store.db = db
	student := &User{ID: "student-db-rollback", Username: "db-rollback", Name: "回滚学生", Email: "rollback@example.com", Role: "student", StudentID: "CGU-DB-ROLLBACK"}
	store.users[student.ID] = student
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(userForDeleteSQL)).WithArgs(student.ID).WillReturnRows(studentDeleteRows(student.ID, student.Username, student.Name, student.Email, student.StudentID))
	mock.ExpectQuery(regexp.QuoteMeta(usersForStudentReferenceWithExternalDeleteSQL)).WithArgs(student.ID, student.StudentID, student.ID, student.StudentID).WillReturnRows(sqlmock.NewRows([]string{"id", "student_id", "role_name"}).AddRow(student.ID, student.StudentID, "student"))
	mock.ExpectQuery(regexp.QuoteMeta(admissionsForStudentByBothRefsSQL)).WithArgs(student.StudentID, student.ID).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_mailbox_messages WHERE recipient_id = ? OR recipient_id = ?")).WithArgs(student.ID, student.StudentID).WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()
	if result, apiError := store.deleteStudent(student.ID); result != nil || apiError == nil || apiError.Status != http.StatusServiceUnavailable {
		t.Fatalf("rollback delete result = %#v, error = %#v", result, apiError)
	}
	if _, ok := store.users[student.ID]; !ok {
		t.Fatal("database failure removed the in-memory student")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	db2, mock2, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	protected := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	protected.db = db2
	mock2.ExpectBegin()
	mock2.ExpectQuery(regexp.QuoteMeta(userForDeleteSQL)).WithArgs("admin").WillReturnRows(sqlmock.NewRows([]string{
		"id", "username", "name_text", "email", "role_name", "password_hash", "student_id", "college", "year_text", "disabled_flag",
	}).AddRow("admin", testAdminUsername, "教务处", "admin@cgu.local", "admin", "bcrypt$hash", "", "", "", false))
	mock2.ExpectRollback()
	if result, apiError := protected.deleteStudent("admin"); result != nil || apiError == nil || apiError.Status != http.StatusForbidden {
		t.Fatalf("database administrator protection = %#v, error = %#v", result, apiError)
	}
	if err := mock2.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
