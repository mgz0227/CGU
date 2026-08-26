package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestSMTPSecretIsAuthenticatedAndNotStoredPlaintext(t *testing.T) {
	key := deriveSMTPKey("administrator-password-2026!")
	ciphertext, err := encryptSMTPSecret(key, "relay-password-very-secret")
	if err != nil {
		t.Fatal(err)
	}
	if ciphertext == "" || strings.Contains(ciphertext, "relay-password-very-secret") {
		t.Fatalf("ciphertext exposes the SMTP secret: %q", ciphertext)
	}
	plaintext, err := decryptSMTPSecret(key, ciphertext)
	if err != nil || plaintext != "relay-password-very-secret" {
		t.Fatalf("secret round trip = %q, error = %v", plaintext, err)
	}
	if _, err := decryptSMTPSecret(deriveSMTPKey("another-password-2026!"), ciphertext); err == nil {
		t.Fatal("ciphertext decrypted with the wrong key")
	}
}

func TestSMTPSettingsViewNeverIncludesPassword(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	store.smtpSettings = &SMTPConfig{Enabled: true, Host: "smtp.example.com", Port: 587, Username: "relay-user", Password: "do-not-return", From: "no-reply@example.com", Auth: "auto", TLSMode: "starttls", TimeoutSecond: 15}
	view := store.smtpSettingsView()
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "do-not-return") {
		t.Fatalf("SMTP view leaked password: %s", encoded)
	}
	if !view.PasswordConfigured {
		t.Fatal("passwordConfigured should be true")
	}
}

func TestSMTPSettingsSaveUsesEncryptedDatabaseValue(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	store.db = db
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO cgu_smtp_settings")).
		WithArgs(true, "smtp.example.com", 587, "relay-user", sqlmock.AnyArg(), "no-reply@example.com", "CGU", "auto", "starttls", "", 15, false).
		WillReturnResult(sqlmock.NewResult(1, 1))
	view, apiError := store.saveSMTPSettings(SMTPSettingsInput{
		Enabled: true, Host: "smtp.example.com", Port: 587, Username: "relay-user", Password: "relay-password-2026!",
		From: "no-reply@example.com", FromName: "CGU", Auth: "auto", TLSMode: "starttls", TimeoutSecond: 15,
	})
	if apiError != nil {
		t.Fatalf("save SMTP settings error = %#v", apiError)
	}
	if !view.PasswordConfigured || store.smtpSettings == nil || store.smtpSettings.Password != "relay-password-2026!" {
		t.Fatalf("saved settings lost password state: view=%#v store=%#v", view, store.smtpSettings)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSMTPAdminRouteRequiresAdministrator(t *testing.T) {
	server := httptest.NewServer(NewServer(NewStoreWithAdmin(testAdminUsername, testAdminPassword), "web"))
	defer server.Close()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/api/admin/smtp", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous SMTP settings status = %d", response.StatusCode)
	}
}

func TestSMTPSettingsSaveRejectsMemoryPersistence(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	_, apiError := store.saveSMTPSettings(SMTPSettingsInput{Enabled: false})
	if apiError == nil || apiError.Code != "smtp_requires_mysql" {
		t.Fatalf("memory save error = %#v", apiError)
	}
}

func TestSMTPAdminSaveAndReadRedactsSecret(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	store.db = db
	server := httptest.NewServer(NewServer(store, "web"))
	defer server.Close()
	server.Config.Handler.(*Server).setStorageMode("mysql")
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	login := postJSON(t, client, server.URL+"/api/auth/login", map[string]string{"username": testAdminUsername, "password": testAdminPassword})
	if login.StatusCode != http.StatusOK {
		login.Body.Close()
		t.Fatalf("admin login status = %d", login.StatusCode)
	}
	login.Body.Close()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO cgu_smtp_settings")).
		WithArgs(true, "smtp.example.com", 587, "relay-user", sqlmock.AnyArg(), "no-reply@example.com", "CGU", "auto", "starttls", "", 15, false).
		WillReturnResult(sqlmock.NewResult(1, 1))
	response := doJSON(t, client, http.MethodPut, server.URL+"/api/admin/smtp", map[string]any{
		"enabled": true, "host": "smtp.example.com", "port": 587, "username": "relay-user", "password": "secret-never-returned",
		"from": "no-reply@example.com", "fromName": "CGU", "auth": "auto", "tlsMode": "starttls", "timeoutSecond": 15,
	})
	if response.StatusCode != http.StatusOK {
		body := response.Body
		defer body.Close()
		t.Fatalf("SMTP save status = %d", response.StatusCode)
	}
	var saved map[string]any
	if err := json.NewDecoder(response.Body).Decode(&saved); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	encoded, _ := json.Marshal(saved)
	if strings.Contains(string(encoded), "secret-never-returned") || strings.Contains(string(encoded), "passwordCiphertext") {
		t.Fatalf("SMTP save response leaked secret: %s", encoded)
	}
	read := doJSON(t, client, http.MethodGet, server.URL+"/api/admin/smtp", nil)
	if read.StatusCode != http.StatusOK {
		read.Body.Close()
		t.Fatalf("SMTP read status = %d", read.StatusCode)
	}
	var current map[string]any
	if err := json.NewDecoder(read.Body).Decode(&current); err != nil {
		read.Body.Close()
		t.Fatal(err)
	}
	read.Body.Close()
	encoded, _ = json.Marshal(current)
	if strings.Contains(string(encoded), "secret-never-returned") || !strings.Contains(string(encoded), "passwordConfigured") {
		t.Fatalf("SMTP read response is unsafe: %s", encoded)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
