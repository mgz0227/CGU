package main

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
)

func TestMailboxCreateReplaysDatabaseIdempotencyConflict(t *testing.T) {
	if strings.Contains(strings.ToUpper(mailboxInsertSQL), "ON DUPLICATE KEY UPDATE") {
		t.Fatal("mailbox creation must not use an upsert")
	}

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	store := NewStoreWithAdminAndDomain(testAdminUsername, testAdminPassword, "students.cgu.edu.kg")
	student := &User{
		ID: "db-idempotency-student", Username: "db-idempotency-student", Name: "数据库幂等学生",
		Email: "contact@example.com", Role: "student", StudentID: "CGU-DB-IDEMPOTENCY",
	}
	store.users[student.ID] = student
	store.db = db
	admin := store.users["admin"]
	requestKey := "db-cold-cache-idempotency-001"

	mock.ExpectExec(regexp.QuoteMeta(mailboxInsertSQL)).WillReturnError(&mysql.MySQLError{Number: 1062, Message: "Duplicate entry"})
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, recipient_id, sender_id, sender_name, subject_text, body_text, created_at, read_at, delivery_mode, external_recipient, delivery_status, delivery_error, delivered_at, request_key FROM cgu_mailbox_messages WHERE request_key = ? LIMIT 1")).
		WithArgs(requestKey).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "recipient_id", "sender_id", "sender_name", "subject_text", "body_text", "created_at", "read_at",
			"delivery_mode", "external_recipient", "delivery_status", "delivery_error", "delivered_at", "request_key",
		}).AddRow(
			"mail-existing-db-idempotency", student.ID, admin.ID, admin.Name, "数据库幂等通知", "只允许保存一封",
			"2026-08-25T00:00:00Z", nil, mailboxDeliveryModeSMTP, student.Email, mailboxDeliveryUnknown,
			"SMTP outcome unknown", nil, requestKey,
		))

	item, apiError := store.createMailboxMessage(admin, MailboxInput{
		StudentID: student.ID, Subject: "数据库幂等通知", Body: "只允许保存一封", External: boolPtr(true), IdempotencyKey: requestKey,
	})
	if apiError != nil || item == nil {
		t.Fatalf("database duplicate replay = %#v, error = %#v", item, apiError)
	}
	if !item.Replayed || item.ID != "mail-existing-db-idempotency" {
		t.Fatalf("database duplicate was not replayed: %#v", item)
	}
	if len(store.mailbox) != 1 {
		t.Fatalf("database duplicate created %d in-memory messages", len(store.mailbox))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
