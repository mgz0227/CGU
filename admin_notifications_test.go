package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestAdminAdmissionNotificationFlow(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	server := httptest.NewServer(NewServer(store, "web"))
	defer server.Close()

	publicClient := &http.Client{}
	created := postJSON(t, publicClient, server.URL+"/api/admissions", map[string]string{
		"name": "至冬申请人", "englishName": "Snezhnaya Applicant", "email": "snezhnaya@example.com", "school": "至冬与极地研究学院",
	})
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("public admission status = %d", created.StatusCode)
	}
	created.Body.Close()

	anonymous := publicClient.Get
	response, err := anonymous(server.URL + "/api/admin/notifications")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous notification status = %d", response.StatusCode)
	}
	response.Body.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	adminClient := &http.Client{Jar: jar}
	login := postJSON(t, adminClient, server.URL+"/api/auth/login", map[string]string{
		"username": testAdminUsername, "password": testAdminPassword,
	})
	if login.StatusCode != http.StatusOK {
		t.Fatalf("admin login status = %d", login.StatusCode)
	}
	login.Body.Close()
	response, err = adminClient.Get(server.URL + "/api/admin/notifications")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("admin notification list status = %d", response.StatusCode)
	}
	var listed struct {
		Unread        int                 `json:"unread"`
		Notifications []AdminNotification `json:"notifications"`
	}
	if err := json.NewDecoder(response.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if listed.Unread != 1 || len(listed.Notifications) != 1 {
		t.Fatalf("unexpected notification list: %#v", listed)
	}
	item := listed.Notifications[0]
	if item.Type != "ADMISSIONS" || item.ReferenceID == "" || item.TitleZh == "" || item.ReadAt != "" {
		t.Fatalf("unexpected notification: %#v", item)
	}
	if item.RecipientID != "" {
		t.Fatal("notification recipient identifier leaked in JSON projection")
	}

	body := bytes.NewBufferString(`{"read":true}`)
	request, err := http.NewRequest(http.MethodPatch, server.URL+"/api/admin/notifications/"+url.PathEscape(item.ID), body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	withoutHeader, err := adminClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if withoutHeader.StatusCode != http.StatusForbidden {
		t.Fatalf("notification CSRF guard status = %d", withoutHeader.StatusCode)
	}
	withoutHeader.Body.Close()

	request, err = http.NewRequest(http.MethodPatch, server.URL+"/api/admin/notifications/"+url.PathEscape(item.ID), strings.NewReader(`{"read":true}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CGU-Request", "1")
	marked, err := adminClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if marked.StatusCode != http.StatusOK {
		t.Fatalf("notification read status = %d", marked.StatusCode)
	}
	marked.Body.Close()

	response, err = adminClient.Get(server.URL + "/api/admin/notifications")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(response.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if listed.Unread != 0 || len(listed.Notifications) != 1 || listed.Notifications[0].ReadAt == "" {
		t.Fatalf("notification read state was not applied: %#v", listed)
	}
}

func TestAdmissionAndAdminNotificationPersistAtomically(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	store.db = db
	mock.ExpectBegin()
	admissionExec := mock.ExpectExec(regexp.QuoteMeta("INSERT INTO cgu_admissions (id, name_text, english_name, email, school_text, status_name, notes_text, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)"))
	admissionExec.WithArgs(sqlmock.AnyArg(), "事务申请人", "Atomic Applicant", "atomic@example.com", "综合学院", "pending", "", sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	notificationExec := mock.ExpectExec(regexp.QuoteMeta("INSERT INTO cgu_admin_notifications (id, recipient_id, type_name, title_zh, title_en, body_zh, body_en, reference_id, created_at, read_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"))
	notificationExec.WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	item, apiError := store.createAdmission(AdmissionApplicationInput{
		Name: "事务申请人", EnglishName: "Atomic Applicant", Email: "atomic@example.com", School: "综合学院",
	})
	if item != nil || apiError == nil || apiError.Status != http.StatusServiceUnavailable {
		t.Fatalf("atomic admission result = %#v, error = %#v", item, apiError)
	}
	if len(store.admissions) != 0 || len(store.notifications) != 0 {
		t.Fatalf("failed atomic write left admissions=%d notifications=%d", len(store.admissions), len(store.notifications))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
