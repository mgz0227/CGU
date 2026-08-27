package main

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
)

func TestEnsureMailboxDeliveryColumnsToleratesConcurrentDDL(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	columnQuery := regexp.QuoteMeta(`SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?`)
	columns := []struct {
		name string
		def  string
	}{
		{name: "delivery_mode", def: "VARCHAR(32) NOT NULL DEFAULT 'internal'"},
		{name: "external_recipient", def: "VARCHAR(255) NOT NULL DEFAULT ''"},
		{name: "delivery_status", def: "VARCHAR(32) NOT NULL DEFAULT 'internal'"},
		{name: "delivery_error", def: "TEXT NULL"},
		{name: "delivered_at", def: "VARCHAR(64) NOT NULL DEFAULT ''"},
		{name: "request_key", def: "VARCHAR(128) NULL"},
		{name: "delivery_started_at", def: "VARCHAR(64) NOT NULL DEFAULT ''"},
	}
	for _, column := range columns {
		mock.ExpectQuery(columnQuery).
			WithArgs("cgu_mailbox_messages", column.name).
			WillReturnRows(sqlmock.NewRows([]string{"COUNT(*)"}).AddRow(0))
		mock.ExpectExec(regexp.QuoteMeta("ALTER TABLE cgu_mailbox_messages ADD COLUMN " + column.name + " " + column.def)).
			WillReturnError(&mysql.MySQLError{Number: 1060, Message: "Duplicate column name"})
	}

	indexQuery := regexp.QuoteMeta(`SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = ?`)
	mock.ExpectQuery(indexQuery).
		WithArgs("cgu_mailbox_messages", "uq_mailbox_request_key").
		WillReturnRows(sqlmock.NewRows([]string{"COUNT(*)"}).AddRow(0))
	mock.ExpectExec(regexp.QuoteMeta("ALTER TABLE cgu_mailbox_messages ADD UNIQUE INDEX uq_mailbox_request_key (request_key)")).
		WillReturnError(&mysql.MySQLError{Number: 1061, Message: "Duplicate key name"})

	if err := ensureMailboxDeliveryColumns(context.Background(), db); err != nil {
		t.Fatalf("concurrent migration should be idempotent: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureAdmissionApprovalColumnsToleratesConcurrentDDL(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	columnQuery := regexp.QuoteMeta(`SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?`)
	columns := []struct {
		name string
		def  string
	}{
		{name: "english_name", def: "VARCHAR(120) NOT NULL DEFAULT ''"},
		{name: "student_id", def: "VARCHAR(128) NOT NULL DEFAULT ''"},
		{name: "approved_at", def: "VARCHAR(64) NOT NULL DEFAULT ''"},
		{name: "approved_by", def: "VARCHAR(64) NOT NULL DEFAULT ''"},
		{name: "initial_password_issued_at", def: "VARCHAR(64) NOT NULL DEFAULT ''"},
	}
	for _, column := range columns {
		mock.ExpectQuery(columnQuery).
			WithArgs("cgu_admissions", column.name).
			WillReturnRows(sqlmock.NewRows([]string{"COUNT(*)"}).AddRow(0))
		mock.ExpectExec(regexp.QuoteMeta("ALTER TABLE cgu_admissions ADD COLUMN " + column.name + " " + column.def)).
			WillReturnError(&mysql.MySQLError{Number: 1060, Message: "Duplicate column name"})
	}
	indexQuery := regexp.QuoteMeta(`SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = ?`)
	mock.ExpectQuery(indexQuery).
		WithArgs("cgu_admissions", "idx_admission_student").
		WillReturnRows(sqlmock.NewRows([]string{"COUNT(*)"}).AddRow(0))
	mock.ExpectExec(regexp.QuoteMeta("ALTER TABLE cgu_admissions ADD INDEX idx_admission_student (student_id)")).
		WillReturnError(&mysql.MySQLError{Number: 1061, Message: "Duplicate key name"})

	if err := ensureAdmissionApprovalColumns(context.Background(), db); err != nil {
		t.Fatalf("concurrent admissions migration should be idempotent: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureUserDisabledColumnToleratesConcurrentDDL(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	columnQuery := regexp.QuoteMeta(`SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?`)
	mock.ExpectQuery(columnQuery).
		WithArgs("cgu_users", "disabled_flag").
		WillReturnRows(sqlmock.NewRows([]string{"COUNT(*)"}).AddRow(0))
	mock.ExpectExec(regexp.QuoteMeta("ALTER TABLE cgu_users ADD COLUMN disabled_flag TINYINT(1) NOT NULL DEFAULT 0")).
		WillReturnError(&mysql.MySQLError{Number: 1060, Message: "Duplicate column name"})

	if err := ensureUserDisabledColumn(context.Background(), db); err != nil {
		t.Fatalf("concurrent users migration should be idempotent: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureCourseBilingualColumnsToleratesConcurrentDDL(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	columnQuery := regexp.QuoteMeta(`SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?`)
	columns := []struct {
		name string
		def  string
	}{
		{name: "department_en", def: "VARCHAR(255) NOT NULL DEFAULT ''"},
		{name: "teacher_en", def: "VARCHAR(255) NOT NULL DEFAULT ''"},
		{name: "description_en", def: "TEXT NOT NULL"},
		{name: "term_name_en", def: "VARCHAR(64) NOT NULL DEFAULT ''"},
	}
	for _, column := range columns {
		mock.ExpectQuery(columnQuery).
			WithArgs("cgu_courses", column.name).
			WillReturnRows(sqlmock.NewRows([]string{"COUNT(*)"}).AddRow(0))
		mock.ExpectExec(regexp.QuoteMeta("ALTER TABLE cgu_courses ADD COLUMN " + column.name + " " + column.def)).
			WillReturnError(&mysql.MySQLError{Number: 1060, Message: "Duplicate column name"})
	}
	if err := ensureCourseBilingualColumns(context.Background(), db); err != nil {
		t.Fatalf("concurrent course migration should be idempotent: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureCourseBilingualColumnsBackfillsOnlyWhenColumnsAreAdded(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	columnQuery := regexp.QuoteMeta(`SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?`)
	columns := []struct {
		name string
		def  string
	}{
		{name: "department_en", def: "VARCHAR(255) NOT NULL DEFAULT ''"},
		{name: "teacher_en", def: "VARCHAR(255) NOT NULL DEFAULT ''"},
		{name: "description_en", def: "TEXT NOT NULL"},
		{name: "term_name_en", def: "VARCHAR(64) NOT NULL DEFAULT ''"},
	}
	for _, column := range columns {
		mock.ExpectQuery(columnQuery).
			WithArgs("cgu_courses", column.name).
			WillReturnRows(sqlmock.NewRows([]string{"COUNT(*)"}).AddRow(0))
		mock.ExpectExec(regexp.QuoteMeta("ALTER TABLE cgu_courses ADD COLUMN " + column.name + " " + column.def)).
			WillReturnResult(sqlmock.NewResult(0, 0))
	}
	for _, field := range []string{"department_en = department", "teacher_en = teacher", "description_en = description", "term_name_en = term_name"} {
		column := strings.TrimSpace(strings.Split(field, "=")[0])
		mock.ExpectExec(regexp.QuoteMeta("UPDATE cgu_courses SET " + field + " WHERE " + column + " = ''")).
			WillReturnResult(sqlmock.NewResult(0, 1))
	}
	if err := ensureCourseBilingualColumns(context.Background(), db); err != nil {
		t.Fatalf("initial course migration failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestIsDuplicateSchemaObjectRejectsOtherMySQLErrors(t *testing.T) {
	if !isDuplicateSchemaObject(&mysql.MySQLError{Number: 1060}) {
		t.Fatal("duplicate column error was not recognized")
	}
	if !isDuplicateSchemaObject(&mysql.MySQLError{Number: 1061}) {
		t.Fatal("duplicate index error was not recognized")
	}
	if isDuplicateSchemaObject(&mysql.MySQLError{Number: 1045}) {
		t.Fatal("unrelated MySQL error was ignored")
	}
}

func TestEnsureDeletionIndexesToleratesConcurrentDDL(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	indexQuery := regexp.QuoteMeta(`SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = ?`)
	indexes := []struct {
		table string
		name  string
		def   string
	}{
		{table: "cgu_users", name: "idx_user_student_id", def: "ALTER TABLE cgu_users ADD INDEX idx_user_student_id (student_id)"},
		{table: "cgu_admin_notifications", name: "idx_admin_notification_reference", def: "ALTER TABLE cgu_admin_notifications ADD INDEX idx_admin_notification_reference (reference_id)"},
	}
	for _, index := range indexes {
		mock.ExpectQuery(indexQuery).WithArgs(index.table, index.name).
			WillReturnRows(sqlmock.NewRows([]string{"COUNT(*)"}).AddRow(0))
		mock.ExpectExec(regexp.QuoteMeta(index.def)).
			WillReturnError(&mysql.MySQLError{Number: 1061, Message: "Duplicate key name"})
	}
	if err := ensureDeletionIndexes(context.Background(), db); err != nil {
		t.Fatalf("concurrent deletion index migration should be idempotent: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
