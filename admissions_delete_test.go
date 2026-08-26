package main

import (
	"database/sql"
	"net/http"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestDeleteAdmissionRejectsProvisionedApplication(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	application, apiError := store.createAdmission(AdmissionApplicationInput{
		Name: "已录取申请", Email: "provisioned@example.com", School: "至冬学院",
	})
	if apiError != nil {
		t.Fatalf("create admission: %v", apiError)
	}
	// createAdmission returns a projection copy; mutate the stored record to
	// model the post-provisioning state guarded by the delete operation.
	stored := store.admissions[0]
	stored.Status = "accepted"
	stored.StudentID = "CGU-2026-0001"
	if _, apiError = store.deleteAdmission(application.ID); apiError == nil || apiError.Status != http.StatusConflict || apiError.Code != "admission_already_approved" {
		t.Fatalf("provisioned admission delete error = %#v", apiError)
	}
	if len(store.admissions) != 1 || len(store.notifications) != 1 {
		t.Fatalf("provisioned admission was mutated: admissions=%d notifications=%d", len(store.admissions), len(store.notifications))
	}
}

func TestDeleteAdmissionPersistsNotificationAndAdmissionAtomically(t *testing.T) {
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
	mock.ExpectQuery(regexp.QuoteMeta("SELECT student_id FROM cgu_admissions WHERE id = ? FOR UPDATE")).WithArgs(application.ID).
		WillReturnRows(sqlmock.NewRows([]string{"student_id"}).AddRow(""))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_admin_notifications WHERE reference_id = ?")).WithArgs(application.ID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_admissions WHERE id = ?")).WithArgs(application.ID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	deleted, apiError := store.deleteAdmission(application.ID)
	if apiError != nil || deleted == nil || deleted.ID != application.ID {
		t.Fatalf("database delete result = %#v, error = %#v", deleted, apiError)
	}
	if len(store.admissions) != 0 || len(store.notifications) != 0 {
		t.Fatalf("database delete left in-memory rows: admissions=%d notifications=%d", len(store.admissions), len(store.notifications))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteAdmissionRollsBackMemoryWhenDatabaseDeleteFails(t *testing.T) {
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
	mock.ExpectQuery(regexp.QuoteMeta("SELECT student_id FROM cgu_admissions WHERE id = ? FOR UPDATE")).WithArgs(application.ID).
		WillReturnRows(sqlmock.NewRows([]string{"student_id"}).AddRow(""))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_admin_notifications WHERE reference_id = ?")).WithArgs(application.ID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM cgu_admissions WHERE id = ?")).WithArgs(application.ID).WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()
	deleted, apiError := store.deleteAdmission(application.ID)
	if deleted != nil || apiError == nil || apiError.Status != http.StatusServiceUnavailable || apiError.Code != "admission_persistence_failed" {
		t.Fatalf("rollback delete result = %#v, error = %#v", deleted, apiError)
	}
	if len(store.admissions) != 1 || len(store.notifications) != 1 || store.admissions[0] != application || store.notifications[0] != notification {
		t.Fatalf("database failure did not restore in-memory rows: admissions=%#v notifications=%#v", store.admissions, store.notifications)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteAdmissionRechecksProvisioningInDatabase(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	store.db = db
	application := &AdmissionApplication{ID: "application-delete-race", Name: "并发录取申请", Email: "race@example.com", School: "综合学院", Status: "pending"}
	notification := &AdminNotification{ID: "notification-delete-race", RecipientID: "admin", ReferenceID: application.ID}
	store.admissions = []*AdmissionApplication{application}
	store.notifications = []*AdminNotification{notification}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT student_id FROM cgu_admissions WHERE id = ? FOR UPDATE")).WithArgs(application.ID).
		WillReturnRows(sqlmock.NewRows([]string{"student_id"}).AddRow("CGU-2026-0002"))
	mock.ExpectRollback()
	deleted, apiError := store.deleteAdmission(application.ID)
	if deleted != nil || apiError == nil || apiError.Status != http.StatusConflict || apiError.Code != "admission_already_approved" {
		t.Fatalf("stale provisioning delete result = %#v, error = %#v", deleted, apiError)
	}
	if len(store.admissions) != 1 || len(store.notifications) != 1 {
		t.Fatalf("stale provisioning mutated in-memory rows: admissions=%d notifications=%d", len(store.admissions), len(store.notifications))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
