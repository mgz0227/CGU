package main

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestMailboxAdminStudentFlow(t *testing.T) {
	store := NewStoreWithAdminAndDomain(testAdminUsername, testAdminPassword, "students.cgu.edu.kg")
	server := httptest.NewServer(NewServer(store, "web"))
	defer server.Close()

	adminJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	adminClient := &http.Client{Jar: adminJar}
	login := postJSON(t, adminClient, server.URL+"/api/auth/login", map[string]string{"username": testAdminUsername, "password": testAdminPassword})
	if login.StatusCode != http.StatusOK {
		t.Fatalf("administrator login status = %d", login.StatusCode)
	}
	login.Body.Close()

	created := postJSON(t, adminClient, server.URL+"/api/admin/students", map[string]string{
		"username": "mail-student-one", "name": "邮箱学生一号", "studentId": "CGU-MAIL-001", "password": "mailbox-student-password-2026!",
	})
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("student create status = %d", created.StatusCode)
	}
	var createdPayload struct {
		Student AdminStudent `json:"student"`
	}
	if err := json.NewDecoder(created.Body).Decode(&createdPayload); err != nil {
		t.Fatal(err)
	}
	created.Body.Close()
	student := createdPayload.Student
	if student.ID == "" || student.StudentEmail != "cgu-mail-001@students.cgu.edu.kg" {
		t.Fatalf("unexpected student projection: %#v", student)
	}

	messageResponse := postJSON(t, adminClient, server.URL+"/api/admin/mailbox", map[string]string{
		"studentId": student.ID,
		"subject":   "选课确认",
		"body":      "请在门户确认课程。\n<不应被当作 HTML>",
	})
	if messageResponse.StatusCode != http.StatusCreated {
		t.Fatalf("mailbox send status = %d", messageResponse.StatusCode)
	}
	var messagePayload struct {
		Message MailboxMessage `json:"message"`
	}
	if err := json.NewDecoder(messageResponse.Body).Decode(&messagePayload); err != nil {
		t.Fatal(err)
	}
	messageResponse.Body.Close()
	message := messagePayload.Message
	if message.ID == "" || message.RecipientID != student.ID || message.RecipientEmail != student.StudentEmail {
		t.Fatalf("unexpected admin mailbox message: %#v", message)
	}

	studentJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	studentClient := &http.Client{Jar: studentJar}
	studentLogin := postJSON(t, studentClient, server.URL+"/api/auth/login", map[string]string{"username": "mail-student-one", "password": "mailbox-student-password-2026!"})
	if studentLogin.StatusCode != http.StatusOK {
		t.Fatalf("student login status = %d", studentLogin.StatusCode)
	}
	studentLogin.Body.Close()

	inbox, err := studentClient.Get(server.URL + "/api/mailbox")
	if err != nil {
		t.Fatal(err)
	}
	if inbox.StatusCode != http.StatusOK {
		t.Fatalf("student mailbox status = %d", inbox.StatusCode)
	}
	rawInbox, err := io.ReadAll(inbox.Body)
	if err != nil {
		t.Fatal(err)
	}
	inbox.Body.Close()
	if strings.Contains(string(rawInbox), `"recipientId"`) || strings.Contains(string(rawInbox), `"senderId"`) {
		t.Fatalf("student mailbox leaked internal identifiers: %s", rawInbox)
	}
	var inboxPayload struct {
		Email    string           `json:"email"`
		Unread   int              `json:"unread"`
		Messages []MailboxMessage `json:"messages"`
	}
	if err := json.Unmarshal(rawInbox, &inboxPayload); err != nil {
		t.Fatal(err)
	}
	if inboxPayload.Email != student.StudentEmail || inboxPayload.Unread != 1 || len(inboxPayload.Messages) != 1 || inboxPayload.Messages[0].Body != "请在门户确认课程。\n<不应被当作 HTML>" {
		t.Fatalf("unexpected student mailbox: %#v", inboxPayload)
	}

	missingHeader, err := http.NewRequest(http.MethodPatch, server.URL+"/api/mailbox/"+url.PathEscape(message.ID), strings.NewReader(`{"read":true}`))
	if err != nil {
		t.Fatal(err)
	}
	missingHeader.Header.Set("Content-Type", "application/json")
	missingHeaderResponse, err := studentClient.Do(missingHeader)
	if err != nil {
		t.Fatal(err)
	}
	if missingHeaderResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("mailbox CSRF guard status = %d", missingHeaderResponse.StatusCode)
	}
	missingHeaderResponse.Body.Close()

	read := doJSON(t, studentClient, http.MethodPatch, server.URL+"/api/mailbox/"+url.PathEscape(message.ID), map[string]bool{"read": true})
	if read.StatusCode != http.StatusOK {
		t.Fatalf("mailbox read status = %d", read.StatusCode)
	}
	read.Body.Close()
	inbox, err = studentClient.Get(server.URL + "/api/mailbox")
	if err != nil {
		t.Fatal(err)
	}
	if inbox.StatusCode != http.StatusOK {
		t.Fatalf("student mailbox reload status = %d", inbox.StatusCode)
	}
	if err := json.NewDecoder(inbox.Body).Decode(&inboxPayload); err != nil {
		t.Fatal(err)
	}
	inbox.Body.Close()
	if inboxPayload.Unread != 0 || len(inboxPayload.Messages) != 1 || inboxPayload.Messages[0].ReadAt == "" {
		t.Fatalf("mailbox read state was not applied: %#v", inboxPayload)
	}

	adminInbox, err := adminClient.Get(server.URL + "/api/admin/mailbox?student_id=" + url.QueryEscape(student.StudentID))
	if err != nil {
		t.Fatal(err)
	}
	if adminInbox.StatusCode != http.StatusOK {
		t.Fatalf("admin mailbox status = %d", adminInbox.StatusCode)
	}
	var adminInboxPayload struct {
		Messages []MailboxMessage `json:"messages"`
	}
	if err := json.NewDecoder(adminInbox.Body).Decode(&adminInboxPayload); err != nil {
		t.Fatal(err)
	}
	adminInbox.Body.Close()
	if len(adminInboxPayload.Messages) != 1 || adminInboxPayload.Messages[0].RecipientStudentID != student.StudentID || adminInboxPayload.Messages[0].ReadAt == "" {
		t.Fatalf("admin mailbox projection = %#v", adminInboxPayload.Messages)
	}

	studentAdminInbox, err := studentClient.Get(server.URL + "/api/admin/mailbox")
	if err != nil {
		t.Fatal(err)
	}
	if studentAdminInbox.StatusCode != http.StatusForbidden {
		t.Fatalf("student admin mailbox status = %d", studentAdminInbox.StatusCode)
	}
	studentAdminInbox.Body.Close()

	// A second student cannot mark the first student's message as read.
	second := postJSON(t, adminClient, server.URL+"/api/admin/students", map[string]string{
		"username": "mail-student-two", "name": "邮箱学生二号", "studentId": "CGU-MAIL-002", "password": "mailbox-second-password-2026!",
	})
	if second.StatusCode != http.StatusCreated {
		t.Fatalf("second student create status = %d", second.StatusCode)
	}
	second.Body.Close()
	secondJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	secondClient := &http.Client{Jar: secondJar}
	secondLogin := postJSON(t, secondClient, server.URL+"/api/auth/login", map[string]string{"username": "mail-student-two", "password": "mailbox-second-password-2026!"})
	if secondLogin.StatusCode != http.StatusOK {
		t.Fatalf("second student login status = %d", secondLogin.StatusCode)
	}
	secondLogin.Body.Close()
	forbiddenRead := doJSON(t, secondClient, http.MethodPatch, server.URL+"/api/mailbox/"+url.PathEscape(message.ID), map[string]bool{"read": false})
	if forbiddenRead.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-student mailbox update status = %d", forbiddenRead.StatusCode)
	}
	forbiddenRead.Body.Close()
}

func TestMailboxValidationAndRollback(t *testing.T) {
	store := NewStoreWithAdminAndDomain(testAdminUsername, testAdminPassword, "students.cgu.edu.kg")
	student := &User{ID: "mail-rollback-student", Username: "mail-rollback", Name: "回滚学生", Role: "student", StudentID: "CGU-MAIL-ROLLBACK"}
	store.users[student.ID] = student
	admin := store.users["admin"]
	if _, apiError := store.createMailboxMessage(admin, MailboxInput{StudentID: student.ID, Subject: "bad\nsubject", Body: "body"}); apiError == nil || apiError.Status != http.StatusBadRequest {
		t.Fatalf("invalid subject result = %#v", apiError)
	}
	if _, apiError := store.createMailboxMessage(admin, MailboxInput{StudentID: student.ID, Subject: "subject", Body: strings.Repeat("x", mailboxBodyLimit+1)}); apiError == nil || apiError.Status != http.StatusBadRequest {
		t.Fatalf("oversized body result = %#v", apiError)
	}

	closedDB, err := sql.Open("mysql", "cgu:cgu@tcp(127.0.0.1:1)/cgu")
	if err != nil {
		t.Fatal(err)
	}
	if err := closedDB.Close(); err != nil {
		t.Fatal(err)
	}
	store.db = closedDB
	item, apiError := store.createMailboxMessage(admin, MailboxInput{StudentID: student.ID, Subject: "不会保存", Body: "数据库不可用时不应写入内存"})
	if item != nil || apiError == nil || apiError.Status != http.StatusServiceUnavailable {
		t.Fatalf("mailbox rollback result = %#v, error = %#v", item, apiError)
	}
	if len(store.mailbox) != 0 {
		t.Fatalf("failed mailbox write left %d in-memory messages", len(store.mailbox))
	}
}
