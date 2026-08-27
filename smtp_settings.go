package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"os"
	"strings"
	"time"
)

// SMTPSettingsInput is the administrator-facing write model. Password is
// intentionally optional: an empty value keeps the current secret unless
// clearPassword is explicitly set.
type SMTPSettingsInput struct {
	Enabled       bool   `json:"enabled"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	From          string `json:"from"`
	FromName      string `json:"fromName"`
	Auth          string `json:"auth"`
	TLSMode       string `json:"tlsMode"`
	HELO          string `json:"helo"`
	TimeoutSecond int    `json:"timeoutSecond"`
	AllowInsecure bool   `json:"allowInsecure"`
	ClearPassword bool   `json:"clearPassword"`
}

// SMTPSettingsView never contains a password, ciphertext, or encryption
// material. It is safe to return from the authenticated administrator API.
type SMTPSettingsView struct {
	Enabled            bool   `json:"enabled"`
	Host               string `json:"host"`
	Port               int    `json:"port"`
	Username           string `json:"username"`
	From               string `json:"from"`
	FromName           string `json:"fromName"`
	Auth               string `json:"auth"`
	TLSMode            string `json:"tlsMode"`
	HELO               string `json:"helo"`
	TimeoutSecond      int    `json:"timeoutSecond"`
	AllowInsecure      bool   `json:"allowInsecure"`
	PasswordConfigured bool   `json:"passwordConfigured"`
	UpdatedAt          string `json:"updatedAt,omitempty"`
}

const smtpSettingsKeyContext = "CGU SMTP settings encryption v1\x00"

// deriveSMTPKey uses a deployment-only settings key when supplied. The
// bootstrap administrator password remains a deterministic fallback so an
// existing deployment can migrate without introducing a second SMTP setting.
// The value is never persisted or returned by the service.
func deriveSMTPKey(adminPassword string) []byte {
	seed := strings.TrimSpace(os.Getenv("CGU_SETTINGS_ENCRYPTION_KEY"))
	if seed == "" {
		seed = adminPassword
	}
	digest := sha256.Sum256([]byte(smtpSettingsKeyContext + seed))
	return digest[:]
}

func encryptSMTPSecret(key []byte, secret string) (string, error) {
	if len(key) != 32 {
		return "", errors.New("smtp encryption key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nil, nonce, []byte(secret), []byte("cgu_smtp_settings/1"))
	payload := append(nonce, sealed...)
	return base64.RawStdEncoding.EncodeToString(payload), nil
}

func decryptSMTPSecret(key []byte, encoded string) (string, error) {
	if len(key) != 32 {
		return "", errors.New("smtp encryption key must be 32 bytes")
	}
	if strings.TrimSpace(encoded) == "" {
		return "", nil
	}
	payload, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		// Accept padded/base16 values only for controlled legacy migrations.
		if payload, err = base64.StdEncoding.DecodeString(encoded); err != nil {
			if payload, err = hex.DecodeString(encoded); err != nil {
				return "", errors.New("smtp secret ciphertext is invalid")
			}
		}
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(payload) < gcm.NonceSize() {
		return "", errors.New("smtp secret ciphertext is truncated")
	}
	secret, err := gcm.Open(nil, payload[:gcm.NonceSize()], payload[gcm.NonceSize():], []byte("cgu_smtp_settings/1"))
	if err != nil {
		return "", errors.New("smtp secret ciphertext authentication failed")
	}
	return string(secret), nil
}

func smtpSettingsViewFromConfig(cfg *SMTPConfig, updatedAt string) SMTPSettingsView {
	if cfg == nil {
		return SMTPSettingsView{Port: 587, Auth: "auto", TLSMode: "starttls", TimeoutSecond: 15, UpdatedAt: updatedAt}
	}
	return SMTPSettingsView{
		Enabled: cfg.Enabled, Host: cfg.Host, Port: cfg.Port, Username: cfg.Username,
		From: cfg.From, FromName: cfg.FromName, Auth: cfg.Auth, TLSMode: cfg.TLSMode,
		HELO: cfg.HELO, TimeoutSecond: cfg.TimeoutSecond, AllowInsecure: cfg.AllowInsecure,
		PasswordConfigured: strings.TrimSpace(cfg.Password) != "", UpdatedAt: updatedAt,
	}
}

func (s *Store) smtpConfigSnapshot() *SMTPConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.smtpSettings == nil {
		return nil
	}
	copy := *s.smtpSettings
	return &copy
}

func (s *Store) smtpSettingsView() SMTPSettingsView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return smtpSettingsViewFromConfig(s.smtpSettings, s.smtpSettingsUpdatedAt)
}

func (s *Store) saveSMTPSettings(input SMTPSettingsInput) (SMTPSettingsView, *apiError) {
	cfg := SMTPConfig{
		Enabled: input.Enabled, Host: input.Host, Port: input.Port, Username: input.Username,
		Password: input.Password, From: input.From, FromName: input.FromName, Auth: input.Auth,
		TLSMode: input.TLSMode, HELO: input.HELO, TimeoutSecond: input.TimeoutSecond,
		AllowInsecure: input.AllowInsecure,
	}
	normalizeSMTPConfig(&cfg)
	if cfg.Auth == "none" {
		cfg.Username, cfg.Password = "", ""
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return SMTPSettingsView{}, apiErr(http.StatusServiceUnavailable, "smtp_requires_mysql", "SMTP settings require a connected MySQL store")
	}
	if existing := s.smtpSettings; existing != nil && cfg.Password == "" && !input.ClearPassword && cfg.Auth != "none" {
		cfg.Password = existing.Password
	}
	if err := validateSMTPSettings(cfg); err != nil {
		return SMTPSettingsView{}, apiErr(http.StatusBadRequest, "invalid_smtp_settings", err.Error())
	}
	updatedAt := time.Now().UTC().Format(time.RFC3339)
	if err := s.persistSMTPSettingsLocked(&cfg); err != nil {
		return SMTPSettingsView{}, apiErr(http.StatusServiceUnavailable, "smtp_settings_unavailable", "SMTP settings could not be saved")
	}
	s.smtpSettings = &cfg
	s.smtpSettingsUpdatedAt = updatedAt
	return smtpSettingsViewFromConfig(&cfg, updatedAt), nil
}

func (s *Server) refreshSMTPMailer() error {
	cfg := s.store.smtpConfigSnapshot()
	if cfg == nil || !cfg.Enabled {
		s.setExternalMailSender(nil)
		return nil
	}
	mailer, err := NewSMTPMailer(*cfg)
	if err != nil {
		return err
	}
	s.setExternalMailSender(mailer)
	return nil
}

func (s *Server) adminSMTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		view := s.store.smtpSettingsView()
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "settings": view, "passwordConfigured": view.PasswordConfigured,
			"storage": s.storageStatus(), "available": s.storageStatus() == "mysql",
		})
	case http.MethodPut, http.MethodPatch:
		var input SMTPSettingsInput
		if err := decodeJSON(w, r, &input); err != nil {
			writeError(w, apiErr(http.StatusBadRequest, "invalid_input", "SMTP settings are required"))
			return
		}
		// Serialize settings changes with admission approval and credential
		// resend. This prevents a rotation from passing an availability check,
		// then losing its delivery path while the new hash is being committed.
		s.admissionDeliveryMu.Lock()
		defer s.admissionDeliveryMu.Unlock()
		view, apiError := s.store.saveSMTPSettings(input)
		if apiError != nil {
			writeError(w, apiError)
			return
		}
		if err := s.refreshSMTPMailer(); err != nil {
			writeError(w, apiErr(http.StatusServiceUnavailable, "smtp_settings_unavailable", "SMTP settings could not be activated"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "settings": view, "passwordConfigured": view.PasswordConfigured})
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPut, http.MethodPatch)
	}
}

type smtpTestInput struct {
	Recipient string `json:"recipient"`
}

func (s *Server) testAdminSMTP(w http.ResponseWriter, r *http.Request) {
	var input smtpTestInput
	if err := decodeJSON(w, r, &input); err != nil || !validSMTPRecipient(input.Recipient) {
		writeError(w, apiErr(http.StatusBadRequest, "invalid_recipient", "a valid test recipient is required"))
		return
	}
	cfg := s.store.smtpConfigSnapshot()
	if cfg == nil || !cfg.Enabled {
		writeError(w, apiErr(http.StatusConflict, "smtp_not_configured", "SMTP is not enabled"))
		return
	}
	mailer, err := NewSMTPMailer(*cfg)
	if err != nil {
		writeError(w, apiErr(http.StatusServiceUnavailable, "smtp_not_configured", "SMTP settings are invalid"))
		return
	}
	if s.smtpSlots != nil {
		select {
		case s.smtpSlots <- struct{}{}:
			defer func() { <-s.smtpSlots }()
		default:
			writeError(w, apiErr(http.StatusTooManyRequests, "smtp_test_busy", "SMTP test delivery is temporarily busy"))
			return
		}
	}
	timeout := time.Duration(cfg.TimeoutSecond) * time.Second
	if timeout <= 0 {
		timeout = mailboxExternalSendTimeout
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	err = mailer.Send(ctx, strings.TrimSpace(input.Recipient), "CGU SMTP connection test", "This is a CGU administrator SMTP test message.")
	cancel()
	if err != nil {
		writeError(w, apiErr(http.StatusBadGateway, "smtp_test_failed", safeExternalDeliveryError(err)))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "SMTP test message accepted by the relay"})
}

func validSMTPRecipient(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 254 || strings.ContainsAny(value, "\r\n") {
		return false
	}
	parsed, err := mail.ParseAddress(value)
	return err == nil && parsed.Address == value && strings.Contains(value, "@")
}

func loadSMTPSettings(ctx context.Context, db *sql.DB, key []byte) (*SMTPConfig, string, error) {
	var enabled bool
	var host, username, ciphertext, from, fromName, auth, tlsMode, helo string
	var port, timeout int
	var allowInsecure bool
	var updated any
	err := db.QueryRowContext(ctx, `SELECT enabled_flag, host_name, port_number, username_text, password_ciphertext, from_address, from_name, auth_mode, tls_mode, helo_name, timeout_seconds, allow_insecure_flag, updated_at FROM cgu_smtp_settings WHERE id = 1 LIMIT 1`).Scan(&enabled, &host, &port, &username, &ciphertext, &from, &fromName, &auth, &tlsMode, &helo, &timeout, &allowInsecure, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	password, err := decryptSMTPSecret(key, ciphertext)
	if err != nil {
		return nil, "", fmt.Errorf("load SMTP settings: %w", err)
	}
	cfg := &SMTPConfig{Enabled: enabled, Host: host, Port: port, Username: username, Password: password, From: from, FromName: fromName, Auth: auth, TLSMode: tlsMode, HELO: helo, TimeoutSecond: timeout, AllowInsecure: allowInsecure}
	normalizeSMTPConfig(cfg)
	return cfg, sqlTimeString(updated), nil
}

func sqlTimeString(value any) string {
	switch item := value.(type) {
	case time.Time:
		return item.UTC().Format(time.RFC3339)
	case []byte:
		return string(item)
	case string:
		return item
	default:
		return ""
	}
}

func (s *Store) persistSMTPSettingsLocked(cfg *SMTPConfig) error {
	if s.db == nil || cfg == nil {
		return nil
	}
	ciphertext, err := encryptSMTPSecret(s.smtpKey, cfg.Password)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err = s.db.ExecContext(ctx, `INSERT INTO cgu_smtp_settings (id, enabled_flag, host_name, port_number, username_text, password_ciphertext, from_address, from_name, auth_mode, tls_mode, helo_name, timeout_seconds, allow_insecure_flag) VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE enabled_flag=VALUES(enabled_flag), host_name=VALUES(host_name), port_number=VALUES(port_number), username_text=VALUES(username_text), password_ciphertext=VALUES(password_ciphertext), from_address=VALUES(from_address), from_name=VALUES(from_name), auth_mode=VALUES(auth_mode), tls_mode=VALUES(tls_mode), helo_name=VALUES(helo_name), timeout_seconds=VALUES(timeout_seconds), allow_insecure_flag=VALUES(allow_insecure_flag)`, cfg.Enabled, cfg.Host, cfg.Port, cfg.Username, ciphertext, cfg.From, cfg.FromName, cfg.Auth, cfg.TLSMode, cfg.HELO, cfg.TimeoutSecond, cfg.AllowInsecure)
	return err
}
