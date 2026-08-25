package main

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	gomail "github.com/wneessen/go-mail"
)

// ExternalMailSender is deliberately small so HTTP/database tests can inject
// a deterministic sender without opening a network connection.
type ExternalMailSender interface {
	Send(context.Context, string, string, string) error
}

// SMTPDeliveryError preserves the important distinction between a rejected
// SMTP transaction and an error that happened after the relay may have
// accepted the DATA payload. Callers must treat the latter as unknown and
// require explicit confirmation before retrying.
type SMTPDeliveryError struct {
	Err            error
	OutcomeUnknown bool
}

func (e *SMTPDeliveryError) Error() string {
	if e == nil || e.Err == nil {
		return "smtp delivery outcome is unknown"
	}
	return e.Err.Error()
}

func (e *SMTPDeliveryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// SMTPMailer sends one plain-text copy of an internal mailbox message through
// a configured SMTP relay. The relay is opt-in and TLS is mandatory by default.
type SMTPMailer struct {
	cfg SMTPConfig
}

func NewSMTPMailer(cfg SMTPConfig) (*SMTPMailer, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	normalizeSMTPConfig(&cfg)
	if err := validateSMTPConfig(cfg); err != nil {
		return nil, err
	}
	return &SMTPMailer{cfg: cfg}, nil
}

func normalizeSMTPConfig(cfg *SMTPConfig) {
	if cfg == nil {
		return
	}
	cfg.Host = strings.TrimSpace(cfg.Host)
	cfg.Username = strings.TrimSpace(cfg.Username)
	cfg.From = strings.TrimSpace(cfg.From)
	cfg.FromName = strings.TrimSpace(cfg.FromName)
	cfg.Auth = strings.ToLower(strings.TrimSpace(cfg.Auth))
	cfg.TLSMode = strings.ToLower(strings.TrimSpace(cfg.TLSMode))
	if cfg.Auth == "" {
		cfg.Auth = "auto"
	}
	if cfg.TLSMode == "implicit" || cfg.TLSMode == "tls" || cfg.TLSMode == "ssl/tls" {
		cfg.TLSMode = "ssl"
	}
	if cfg.TLSMode == "start_tls" || cfg.TLSMode == "start-tls" {
		cfg.TLSMode = "starttls"
	}
	if cfg.TimeoutSecond <= 0 {
		cfg.TimeoutSecond = 15
	}
	if cfg.Port <= 0 {
		cfg.Port = 587
	}
}

func validateSMTPConfig(cfg SMTPConfig) error {
	if cfg.Host == "" || len([]rune(cfg.Host)) > 253 || strings.ContainsAny(cfg.Host, "\r\n /\\") {
		return fmt.Errorf("smtp host is invalid")
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return fmt.Errorf("smtp port is invalid")
	}
	if cfg.From == "" || len(cfg.From) > 254 || strings.ContainsAny(cfg.From, "\r\n") {
		return fmt.Errorf("smtp from address is invalid")
	}
	parsed, err := mail.ParseAddress(cfg.From)
	if err != nil || parsed.Address != cfg.From || !strings.Contains(cfg.From, "@") {
		return fmt.Errorf("smtp from address is invalid")
	}
	if len([]rune(cfg.FromName)) > 200 || strings.ContainsAny(cfg.FromName, "\r\n") {
		return fmt.Errorf("smtp from name is invalid")
	}
	if cfg.HELO != "" && (len([]rune(cfg.HELO)) > 253 || strings.ContainsAny(cfg.HELO, "\r\n ")) {
		return fmt.Errorf("smtp helo is invalid")
	}
	if cfg.TimeoutSecond < 1 || cfg.TimeoutSecond > 300 {
		return fmt.Errorf("smtp timeout must be between 1 and 300 seconds")
	}
	switch cfg.TLSMode {
	case "starttls":
	case "ssl":
		if cfg.Port != 465 {
			return fmt.Errorf("smtp ssl mode requires port 465; use starttls for port 587")
		}
	case "none":
		if !cfg.AllowInsecure {
			return fmt.Errorf("smtp plaintext mode requires allowInsecure=true")
		}
	default:
		return fmt.Errorf("smtp tls mode must be starttls, ssl, or none")
	}
	switch cfg.Auth {
	case "auto", "none", "plain", "login", "cram-md5", "scram-sha-256", "xoauth2":
	default:
		return fmt.Errorf("smtp auth mode is not supported")
	}
	if cfg.Auth != "none" && (cfg.Username == "" || cfg.Password == "") {
		return fmt.Errorf("smtp username and password are required for authenticated mode")
	}
	if cfg.Auth == "none" && (cfg.Username != "" || cfg.Password != "") {
		return fmt.Errorf("smtp auth none cannot include username or password")
	}
	if cfg.TLSMode == "none" && cfg.Auth != "none" {
		return fmt.Errorf("smtp authenticated mode requires TLS")
	}
	return nil
}

func (m *SMTPMailer) Send(ctx context.Context, recipient, subject, body string) error {
	if m == nil {
		return fmt.Errorf("smtp is not configured")
	}
	recipient = strings.TrimSpace(recipient)
	if recipient == "" || len(recipient) > 254 || strings.ContainsAny(recipient, "\r\n") {
		return fmt.Errorf("recipient address is invalid")
	}
	parsed, err := mail.ParseAddress(recipient)
	if err != nil || parsed.Address != recipient || !strings.Contains(recipient, "@") {
		return fmt.Errorf("recipient address is invalid")
	}
	if strings.TrimSpace(subject) == "" || strings.ContainsAny(subject, "\r\n") {
		return fmt.Errorf("message subject is invalid")
	}
	if strings.ContainsRune(body, '\x00') {
		return fmt.Errorf("message body is invalid")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	opts := []gomail.Option{
		gomail.WithPort(m.cfg.Port),
		gomail.WithTimeout(time.Duration(m.cfg.TimeoutSecond) * time.Second),
	}
	if m.cfg.HELO != "" {
		opts = append(opts, gomail.WithHELO(m.cfg.HELO))
	}
	switch m.cfg.TLSMode {
	case "starttls":
		opts = append(opts, gomail.WithTLSPortPolicy(gomail.TLSMandatory))
	case "ssl":
		opts = append(opts, gomail.WithSSL())
	case "none":
		opts = append(opts, gomail.WithTLSPolicy(gomail.NoTLS))
	}
	if m.cfg.Auth != "none" && m.cfg.Username != "" {
		opts = append(opts, gomail.WithUsername(m.cfg.Username), gomail.WithPassword(m.cfg.Password))
		switch m.cfg.Auth {
		case "auto":
			opts = append(opts, gomail.WithSMTPAuth(gomail.SMTPAuthAutoDiscover))
		case "plain":
			opts = append(opts, gomail.WithSMTPAuth(gomail.SMTPAuthPlain))
		case "login":
			opts = append(opts, gomail.WithSMTPAuth(gomail.SMTPAuthLogin))
		case "cram-md5":
			opts = append(opts, gomail.WithSMTPAuth(gomail.SMTPAuthCramMD5))
		case "scram-sha-256":
			opts = append(opts, gomail.WithSMTPAuth(gomail.SMTPAuthSCRAMSHA256))
		case "xoauth2":
			opts = append(opts, gomail.WithSMTPAuth(gomail.SMTPAuthXOAUTH2))
		}
	}
	client, err := gomail.NewClient(m.cfg.Host, opts...)
	if err != nil {
		return err
	}
	message := gomail.NewMsg(gomail.WithCharset(gomail.CharsetUTF8), gomail.WithEncoding(gomail.EncodingQP))
	if m.cfg.FromName != "" {
		if err := message.FromFormat(m.cfg.FromName, m.cfg.From); err != nil {
			return err
		}
	} else if err := message.From(m.cfg.From); err != nil {
		return err
	}
	if err := message.To(recipient); err != nil {
		return err
	}
	message.Subject(subject)
	message.SetBodyString(gomail.TypeTextPlain, body)
	err = client.DialAndSendWithContext(ctx, message)
	if err == nil {
		return nil
	}
	unknown := message.IsDelivered()
	var sendErr *gomail.SendError
	if errors.As(err, &sendErr) && sendErr != nil {
		// DATA close/reset and content-write failures occur after the relay may
		// already have accepted some or all of the message.
		switch sendErr.Reason {
		case gomail.ErrSMTPDataClose, gomail.ErrSMTPReset, gomail.ErrWriteContent:
			unknown = true
		}
	}
	if unknown {
		return &SMTPDeliveryError{Err: err, OutcomeUnknown: true}
	}
	return err
}
