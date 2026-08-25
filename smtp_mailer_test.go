package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type recordingExternalSender struct {
	mu    sync.Mutex
	calls []externalMailCall
	err   error
}

type externalMailCall struct {
	recipient string
	subject   string
	body      string
}

func (sender *recordingExternalSender) Send(_ context.Context, recipient, subject, body string) error {
	sender.mu.Lock()
	sender.calls = append(sender.calls, externalMailCall{recipient: recipient, subject: subject, body: body})
	err := sender.err
	sender.mu.Unlock()
	return err
}

func (sender *recordingExternalSender) callCount() int {
	sender.mu.Lock()
	defer sender.mu.Unlock()
	return len(sender.calls)
}

func TestSMTPMailboxDeliveryRetryAndStudentIsolation(t *testing.T) {
	store := NewStoreWithAdminAndDomain(testAdminUsername, testAdminPassword, "students.cgu.edu.kg")
	student, apiError := store.createStudent(StudentInput{
		Username: "smtp-student", Name: "SMTP 测试学生", Email: "traveler.contact@example.com",
		StudentID: "CGU-SMTP-001", Password: "smtp-student-password-2026!",
	})
	if apiError != nil || student == nil {
		t.Fatalf("student create = %#v, error = %#v", student, apiError)
	}
	sender := &recordingExternalSender{err: context.DeadlineExceeded}
	server := httptest.NewServer(NewServer(store, "web"))
	defer server.Close()
	server.Config.Handler.(*Server).setExternalMailSender(sender)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	login := postJSON(t, client, server.URL+"/api/auth/login", map[string]string{"username": testAdminUsername, "password": testAdminPassword})
	if login.StatusCode != http.StatusOK {
		t.Fatalf("admin login status = %d", login.StatusCode)
	}
	login.Body.Close()

	response := postJSON(t, client, server.URL+"/api/admin/mailbox", map[string]any{
		"studentId": student.ID, "subject": "SMTP 通知", "body": "请查收外部邮件。", "external": true, "idempotencyKey": "smtp-retry-flow-001",
	})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("external mailbox status = %d", response.StatusCode)
	}
	var payload struct {
		Message MailboxMessage `json:"message"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if payload.Message.DeliveryMode != mailboxDeliveryModeSMTP || payload.Message.DeliveryStatus != mailboxDeliveryUnknown || payload.Message.ExternalRecipient != student.Email || payload.Message.DeliveryError == "" {
		t.Fatalf("failed SMTP message = %#v", payload.Message)
	}
	if sender.callCount() != 1 {
		t.Fatalf("external sender calls = %d, want 1", sender.callCount())
	}
	duplicateCreate := postJSON(t, client, server.URL+"/api/admin/mailbox", map[string]any{
		"studentId": student.ID, "subject": "SMTP 通知", "body": "请查收外部邮件。", "external": true, "idempotencyKey": "smtp-retry-flow-001",
	})
	if duplicateCreate.StatusCode != http.StatusOK {
		t.Fatalf("duplicate external mailbox status = %d", duplicateCreate.StatusCode)
	}
	var duplicatePayload struct {
		Replayed bool           `json:"replayed"`
		Message  MailboxMessage `json:"message"`
	}
	if err := json.NewDecoder(duplicateCreate.Body).Decode(&duplicatePayload); err != nil {
		t.Fatal(err)
	}
	duplicateCreate.Body.Close()
	if !duplicatePayload.Replayed || duplicatePayload.Message.ID != payload.Message.ID || sender.callCount() != 1 {
		t.Fatalf("duplicate external request was not idempotent: %#v calls=%d", duplicatePayload, sender.callCount())
	}
	conflictCreate := postJSON(t, client, server.URL+"/api/admin/mailbox", map[string]any{
		"studentId": student.ID, "subject": "Different subject", "body": "Different body", "external": true, "idempotencyKey": "smtp-retry-flow-001",
	})
	if conflictCreate.StatusCode != http.StatusConflict {
		t.Fatalf("idempotency conflict status = %d", conflictCreate.StatusCode)
	}
	conflictCreate.Body.Close()

	// An interrupted/timeout outcome requires explicit confirmation before a
	// retry, because the relay may have accepted the message already.
	unconfirmed := doJSON(t, client, http.MethodPost, server.URL+"/api/admin/mailbox/"+payload.Message.ID+"/retry", nil)
	if unconfirmed.StatusCode != http.StatusConflict {
		t.Fatalf("unconfirmed mailbox retry status = %d", unconfirmed.StatusCode)
	}
	unconfirmed.Body.Close()

	// Once the relay recovers, a confirmed unknown outcome can be retried.
	sender.mu.Lock()
	sender.err = nil
	sender.mu.Unlock()
	retry := doJSON(t, client, http.MethodPost, server.URL+"/api/admin/mailbox/"+payload.Message.ID+"/retry", map[string]bool{"confirmUnknown": true})
	if retry.StatusCode != http.StatusOK {
		t.Fatalf("mailbox retry status = %d", retry.StatusCode)
	}
	var retryPayload struct {
		Message MailboxMessage `json:"message"`
	}
	if err := json.NewDecoder(retry.Body).Decode(&retryPayload); err != nil {
		t.Fatal(err)
	}
	retry.Body.Close()
	if retryPayload.Message.DeliveryStatus != mailboxDeliverySent || retryPayload.Message.DeliveredAt == "" {
		t.Fatalf("retry result = %#v", retryPayload.Message)
	}
	duplicate := doJSON(t, client, http.MethodPost, server.URL+"/api/admin/mailbox/"+payload.Message.ID+"/retry", nil)
	if duplicate.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate retry status = %d", duplicate.StatusCode)
	}
	duplicate.Body.Close()

	studentJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	studentClient := &http.Client{Jar: studentJar}
	studentLogin := postJSON(t, studentClient, server.URL+"/api/auth/login", map[string]string{"username": student.Username, "password": "smtp-student-password-2026!"})
	if studentLogin.StatusCode != http.StatusOK {
		t.Fatalf("student login status = %d", studentLogin.StatusCode)
	}
	studentLogin.Body.Close()
	inbox, err := studentClient.Get(server.URL + "/api/mailbox")
	if err != nil {
		t.Fatal(err)
	}
	rawInbox, err := io.ReadAll(inbox.Body)
	inbox.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rawInbox), "deliveryStatus") || strings.Contains(string(rawInbox), "externalRecipient") || strings.Contains(string(rawInbox), student.Email) {
		t.Fatalf("student mailbox leaked external delivery fields: %s", rawInbox)
	}
}

func TestSMTPConfigValidation(t *testing.T) {
	tests := []struct {
		name string
		cfg  SMTPConfig
		want bool
	}{
		{name: "secure starttls", cfg: SMTPConfig{Enabled: true, Host: "smtp.example.com", Port: 587, Username: "user", Password: "secret", From: "noreply@example.com", TLSMode: "starttls", Auth: "auto", TimeoutSecond: 10}, want: true},
		{name: "missing host", cfg: SMTPConfig{Enabled: true, From: "noreply@example.com", Username: "user", Password: "secret"}},
		{name: "plaintext requires explicit opt in", cfg: SMTPConfig{Enabled: true, Host: "127.0.0.1", Port: 2525, From: "noreply@example.com", Auth: "none", TLSMode: "none"}},
		{name: "plaintext fixture opt in", cfg: SMTPConfig{Enabled: true, Host: "127.0.0.1", Port: 2525, From: "noreply@example.com", Auth: "none", TLSMode: "none", AllowInsecure: true}, want: true},
		{name: "implicit tls requires port 465", cfg: SMTPConfig{Enabled: true, Host: "smtp.example.com", Port: 587, Username: "user", Password: "secret", From: "noreply@example.com", TLSMode: "ssl", Auth: "auto"}},
		{name: "implicit tls on 465", cfg: SMTPConfig{Enabled: true, Host: "smtp.example.com", Port: 465, Username: "user", Password: "secret", From: "noreply@example.com", TLSMode: "ssl", Auth: "auto"}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewSMTPMailer(test.cfg)
			if (err == nil) != test.want {
				t.Fatalf("NewSMTPMailer error = %v, want success = %t", err, test.want)
			}
		})
	}
}

func TestMailboxPendingDeliveryCanRecoverAfterStaleAttempt(t *testing.T) {
	store := NewStoreWithAdminAndDomain(testAdminUsername, testAdminPassword, "students.cgu.edu.kg")
	student, apiError := store.createStudent(StudentInput{
		Username: "smtp-stale-student", Name: "Stale SMTP Student", Email: "stale@example.com",
		StudentID: "CGU-SMTP-STALE", Password: "smtp-stale-student-password-2026!",
	})
	if apiError != nil || student == nil {
		t.Fatalf("student create = %#v, error = %#v", student, apiError)
	}
	item, apiError := store.createMailboxMessage(store.users["admin"], MailboxInput{
		StudentID: student.ID, Subject: "Pending", Body: "Recoverable", External: boolPtr(true), IdempotencyKey: "smtp-stale-flow-001",
	})
	if apiError != nil || item == nil {
		t.Fatalf("mailbox create = %#v, error = %#v", item, apiError)
	}
	if item.DeliveryStatus != mailboxDeliverySending {
		t.Fatalf("initial delivery status = %q", item.DeliveryStatus)
	}
	if _, apiError := store.beginMailboxDelivery(item.ID); apiError == nil || apiError.Code != "delivery_in_progress" {
		t.Fatalf("active retry without confirmation = %#v", apiError)
	}
	store.mu.Lock()
	for _, message := range store.mailbox {
		if message != nil && message.ID == item.ID {
			message.DeliveryStatus = mailboxDeliveryUnknown
			message.DeliveryError = "SMTP outcome unknown after service restart"
		}
	}
	store.mu.Unlock()
	if _, apiError := store.beginMailboxDelivery(item.ID); apiError == nil || apiError.Code != "delivery_outcome_unknown" {
		t.Fatalf("unknown retry without confirmation = %#v", apiError)
	}
	retry, apiError := store.beginMailboxDeliveryWithConfirmation(item.ID, true)
	if apiError != nil || retry == nil || retry.DeliveryStatus != mailboxDeliverySending {
		t.Fatalf("stale pending retry = %#v, error = %#v", retry, apiError)
	}
}

func TestConcurrentMailboxRetryIsRejectedWhileSending(t *testing.T) {
	store := NewStoreWithAdminAndDomain(testAdminUsername, testAdminPassword, "students.cgu.edu.kg")
	student, apiError := store.createStudent(StudentInput{
		Username: "smtp-concurrent-student", Name: "Concurrent SMTP Student", Email: "concurrent@example.com",
		StudentID: "CGU-SMTP-CONCURRENT", Password: "smtp-concurrent-student-password-2026!",
	})
	if apiError != nil || student == nil {
		t.Fatalf("student create = %#v, error = %#v", student, apiError)
	}
	item, apiError := store.createMailboxMessage(store.users["admin"], MailboxInput{
		StudentID: student.ID, Subject: "Concurrent", Body: "Do not duplicate", External: boolPtr(true), IdempotencyKey: "smtp-concurrent-flow-001",
	})
	if apiError != nil || item == nil {
		t.Fatalf("mailbox create = %#v, error = %#v", item, apiError)
	}
	for _, confirmed := range []bool{false, true} {
		if _, retryError := store.beginMailboxDeliveryWithConfirmation(item.ID, confirmed); retryError == nil || retryError.Code != "delivery_in_progress" {
			t.Fatalf("concurrent retry confirmed=%t error = %#v", confirmed, retryError)
		}
	}
}

func boolPtr(value bool) *bool { return &value }

func TestSMTPMailerRejectsHeaderInjection(t *testing.T) {
	mailer, err := NewSMTPMailer(SMTPConfig{
		Enabled: true, Host: "smtp.example.com", Port: 587, Username: "user", Password: "secret",
		From: "noreply@example.com", Auth: "auto", TLSMode: "starttls", TimeoutSecond: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := mailer.Send(ctx, "recipient@example.com\r\nBcc: attacker@example.com", "subject", "body"); err == nil {
		t.Fatal("recipient header injection was accepted")
	}
	if err := mailer.Send(ctx, "recipient@example.com", "subject\nBcc: attacker@example.com", "body"); err == nil {
		t.Fatal("subject header injection was accepted")
	}
}

func TestSMTPMailerSendsThroughLocalFixture(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	received := make(chan string, 1)
	serverErr := make(chan error, 1)
	go serveSMTPFixture(listener, received, serverErr)

	port := listener.Addr().(*net.TCPAddr).Port
	mailer, err := NewSMTPMailer(SMTPConfig{
		Enabled: true, Host: "127.0.0.1", Port: port, From: "noreply@example.com",
		Auth: "none", TLSMode: "none", AllowInsecure: true, TimeoutSecond: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := mailer.Send(ctx, "student@example.com", "CGU SMTP fixture", "测试邮件正文"); err != nil {
		t.Fatalf("SMTP send failed: %v", err)
	}
	select {
	case message := <-received:
		parts := strings.SplitN(message, "\r\n\r\n", 2)
		decodedBody := ""
		if len(parts) == 2 {
			decoded, decodeErr := io.ReadAll(quotedprintable.NewReader(strings.NewReader(parts[1])))
			if decodeErr == nil {
				decodedBody = string(decoded)
			}
		}
		if !strings.Contains(message, "Subject: CGU SMTP fixture") || !strings.Contains(message, "student@example.com") || !strings.Contains(decodedBody, "测试邮件正文") {
			t.Fatalf("unexpected SMTP message: %s", message)
		}
	case err := <-serverErr:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for SMTP fixture")
	}
}

func serveSMTPFixture(listener net.Listener, received chan<- string, serverErr chan<- error) {
	connection, err := listener.Accept()
	if err != nil {
		serverErr <- err
		return
	}
	defer connection.Close()
	reader := bufio.NewReader(connection)
	writeLine := func(value string) error {
		_, err := io.WriteString(connection, value+"\r\n")
		return err
	}
	if err := writeLine("220 cgu-test ESMTP"); err != nil {
		serverErr <- err
		return
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			serverErr <- err
			return
		}
		command := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(command, "EHLO"), strings.HasPrefix(command, "HELO"):
			if _, err = io.WriteString(connection, "250-cgu-test\r\n250-8BITMIME\r\n250 SMTPUTF8\r\n"); err != nil {
				serverErr <- err
				return
			}
		case strings.HasPrefix(command, "MAIL FROM"), strings.HasPrefix(command, "RCPT TO"), command == "RSET", command == "NOOP":
			if err = writeLine("250 OK"); err != nil {
				serverErr <- err
				return
			}
		case command == "DATA":
			if err = writeLine("354 End data with <CR><LF>.<CR><LF>"); err != nil {
				serverErr <- err
				return
			}
			var message strings.Builder
			for {
				dataLine, readErr := reader.ReadString('\n')
				if readErr != nil {
					serverErr <- readErr
					return
				}
				if strings.TrimRight(dataLine, "\r\n") == "." {
					break
				}
				message.WriteString(dataLine)
			}
			received <- message.String()
			if err = writeLine("250 OK"); err != nil {
				serverErr <- err
				return
			}
		case command == "QUIT":
			_ = writeLine("221 Bye")
			return
		default:
			if err = writeLine("250 OK"); err != nil {
				serverErr <- err
				return
			}
		}
	}
}
