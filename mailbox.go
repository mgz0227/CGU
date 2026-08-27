package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	mailboxSubjectLimit          = 200
	mailboxBodyLimit             = 10_000
	mailboxDeliveryModeInternal  = "internal"
	mailboxDeliveryModeSMTP      = "internal+smtp"
	mailboxDeliveryInternal      = "internal"
	mailboxDeliveryPending       = "pending"
	mailboxDeliverySending       = "sending"
	mailboxDeliverySent          = "sent"
	mailboxDeliveryFailed        = "failed"
	mailboxDeliveryNotConfigured = "not_configured"
	mailboxDeliveryUnknown       = "unknown"
)

const mailboxExternalSendTimeout = 20 * time.Second
const mailboxDeliveryLease = 10 * time.Minute

var errMailboxDeliveryClaimed = errors.New("mailbox delivery claim already owned")

func mailboxExternalRequested(input MailboxInput) bool {
	for _, value := range []*bool{input.External, input.SendExternal, input.SendExternalSnake} {
		if value != nil {
			return *value
		}
	}
	return false
}

func (s *Store) normalizeMailboxInputLocked(input MailboxInput, sender *User) (*MailboxMessage, *apiError) {
	if sender == nil || sender.Role != "admin" {
		return nil, apiErr(http.StatusForbidden, "admin_required", "administrator role is required")
	}
	studentRef := first(input.StudentID, input.StudentRef)
	student := s.resolveStudentLocked(studentRef)
	if student == nil {
		return nil, apiErr(http.StatusNotFound, "student_not_found", "student not found")
	}
	subject := first(input.Subject, input.Title)
	body := first(input.Body, input.Message, input.Content)
	if subject == "" || body == "" {
		return nil, apiErr(http.StatusBadRequest, "invalid_input", "subject and body are required")
	}
	if len([]rune(subject)) > mailboxSubjectLimit || strings.ContainsAny(subject, "\r\n") {
		return nil, apiErr(http.StatusBadRequest, "invalid_input", "subject is too long or contains a line break")
	}
	if len([]rune(body)) > mailboxBodyLimit || strings.ContainsRune(body, '\x00') {
		return nil, apiErr(http.StatusBadRequest, "invalid_input", "message body is too long")
	}
	requestKey := first(input.IdempotencyKey, input.RequestKey)
	if requestKey != "" && (len([]rune(requestKey)) > 128 || strings.ContainsAny(requestKey, "\r\n")) {
		return nil, apiErr(http.StatusBadRequest, "invalid_input", "idempotency key is invalid")
	}
	deliveryMode := mailboxDeliveryModeInternal
	deliveryStatus := mailboxDeliveryInternal
	externalRecipient := ""
	deliveryStartedAt := ""
	if mailboxExternalRequested(input) {
		deliveryMode = mailboxDeliveryModeSMTP
		externalRecipient = strings.TrimSpace(student.Email)
		if externalRecipient == "" || strings.EqualFold(externalRecipient, studentMailbox(student.StudentID, s.studentEmailDomain)) || !validateContactEmail(externalRecipient) {
			return nil, apiErr(http.StatusBadRequest, "external_recipient_missing", "student has no valid contact email for external delivery")
		}
		if requestKey == "" {
			return nil, apiErr(http.StatusBadRequest, "idempotency_key_required", "an idempotency key is required for external delivery")
		}
		if len([]rune(requestKey)) > 128 || strings.ContainsAny(requestKey, "\r\n") {
			return nil, apiErr(http.StatusBadRequest, "invalid_input", "idempotency key is invalid")
		}
		// Persist the sending state before opening the SMTP connection. A process
		// restart can then safely convert an interrupted attempt to "unknown".
		deliveryStatus = mailboxDeliverySending
		deliveryStartedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if requestKey != "" {
		for _, existing := range s.mailbox {
			if existing == nil || !strings.EqualFold(existing.RequestKey, requestKey) {
				continue
			}
			if existing.RecipientID != student.ID || existing.Subject != subject || existing.Body != body || existing.DeliveryMode != deliveryMode || existing.ExternalRecipient != externalRecipient {
				return nil, apiErr(http.StatusConflict, "idempotency_key_conflict", "idempotency key is already bound to a different message")
			}
			copy := s.mailboxAdminViewLocked(existing)
			copy.Replayed = true
			return &copy, nil
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	return &MailboxMessage{
		ID:                 "mail-" + randomID(16),
		RecipientID:        student.ID,
		RecipientName:      student.Name,
		RecipientStudentID: student.StudentID,
		RecipientEmail:     studentMailbox(student.StudentID, s.studentEmailDomain),
		SenderID:           sender.ID,
		SenderName:         first(sender.Name, sender.Username, "CGU 教务处"),
		Subject:            subject,
		Body:               body,
		CreatedAt:          now,
		DeliveryMode:       deliveryMode,
		ExternalRecipient:  externalRecipient,
		DeliveryStatus:     deliveryStatus,
		RequestKey:         first(input.IdempotencyKey, input.RequestKey),
		DeliveryStartedAt:  deliveryStartedAt,
	}, nil
}

func (s *Store) mailboxStudentViewLocked(item *MailboxMessage) MailboxMessage {
	if item == nil {
		return MailboxMessage{}
	}
	copy := *item
	// Internal identifiers and recipient directory details are administrator
	// fields only. Students receive the message content and sender identity.
	copy.RecipientID = ""
	copy.RecipientName = ""
	copy.RecipientStudentID = ""
	copy.RecipientEmail = ""
	copy.SenderID = ""
	copy.DeliveryMode = ""
	copy.ExternalRecipient = ""
	copy.DeliveryStatus = ""
	copy.DeliveryError = ""
	copy.DeliveredAt = ""
	return copy
}

func (s *Store) mailboxAdminViewLocked(item *MailboxMessage) MailboxMessage {
	if item == nil {
		return MailboxMessage{}
	}
	copy := *item
	if copy.DeliveryMode == "" {
		copy.DeliveryMode = mailboxDeliveryModeInternal
	}
	if copy.DeliveryStatus == "" {
		copy.DeliveryStatus = mailboxDeliveryInternal
	}
	if recipient := s.users[copy.RecipientID]; recipient != nil && recipient.Role == "student" {
		copy.RecipientName = recipient.Name
		copy.RecipientStudentID = recipient.StudentID
		copy.RecipientEmail = studentMailbox(recipient.StudentID, s.studentEmailDomain)
	} else if copy.RecipientEmail == "" {
		copy.RecipientEmail = studentMailbox(copy.RecipientStudentID, s.studentEmailDomain)
	}
	return copy
}

func sortMailboxMessages(items []MailboxMessage) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].CreatedAt == items[j].CreatedAt {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt > items[j].CreatedAt
	})
}

func (s *Store) mailboxForStudent(studentID string) ([]MailboxMessage, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]MailboxMessage, 0)
	unread := 0
	for _, item := range s.mailbox {
		if item == nil || item.RecipientID != studentID {
			continue
		}
		result = append(result, s.mailboxStudentViewLocked(item))
		if item.ReadAt == "" {
			unread++
		}
	}
	sortMailboxMessages(result)
	return result, unread
}

func (s *Store) mailboxForAdmin(studentRef string) ([]MailboxMessage, *apiError) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	targetID := ""
	if strings.TrimSpace(studentRef) != "" {
		student := s.resolveStudentLocked(studentRef)
		if student == nil {
			return nil, apiErr(http.StatusNotFound, "student_not_found", "student not found")
		}
		targetID = student.ID
	}
	result := make([]MailboxMessage, 0)
	for _, item := range s.mailbox {
		if item == nil || (targetID != "" && item.RecipientID != targetID) {
			continue
		}
		result = append(result, s.mailboxAdminViewLocked(item))
	}
	sortMailboxMessages(result)
	return result, nil
}

func (s *Store) createMailboxMessage(sender *User, input MailboxInput) (*MailboxMessage, *apiError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, apiError := s.normalizeMailboxInputLocked(input, sender)
	if apiError != nil {
		return nil, apiError
	}
	if item.Replayed {
		return item, nil
	}
	if err := s.persistMailboxLocked(item); err != nil {
		if item.RequestKey != "" {
			if existing, lookupErr := s.findMailboxByRequestKeyLocked(item.RequestKey); lookupErr == nil && existing != nil {
				if existing.RecipientID != item.RecipientID || existing.Subject != item.Subject || existing.Body != item.Body || existing.DeliveryMode != item.DeliveryMode || existing.ExternalRecipient != item.ExternalRecipient {
					return nil, apiErr(http.StatusConflict, "idempotency_key_conflict", "idempotency key is already bound to a different message")
				}
				existing.Replayed = true
				s.mailbox = append(s.mailbox, existing)
				copy := s.mailboxAdminViewLocked(existing)
				copy.Replayed = true
				return &copy, nil
			}
		}
		return nil, apiErr(http.StatusServiceUnavailable, "mailbox_persistence_failed", "message could not be saved")
	}
	s.mailbox = append(s.mailbox, item)
	copy := s.mailboxAdminViewLocked(item)
	return &copy, nil
}

func (s *Store) claimMailboxDeliveryLocked(item *MailboxMessage, previousStatus string) error {
	if item == nil {
		return fmt.Errorf("mailbox item is nil")
	}
	if s.db == nil {
		return s.persistMailboxLocked(item)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	startedAt := time.Now().UTC().Format(time.RFC3339Nano) + "|" + randomID(8)
	item.DeliveryStartedAt = startedAt
	result, err := s.db.ExecContext(ctx, `UPDATE cgu_mailbox_messages SET delivery_status = ?, delivery_error = '', delivered_at = '', delivery_started_at = ? WHERE id = ? AND delivery_status = ?`, mailboxDeliverySending, startedAt, item.ID, previousStatus)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return errMailboxDeliveryClaimed
	}
	return nil
}

func (s *Store) findMailboxByRequestKeyLocked(requestKey string) (*MailboxMessage, error) {
	if s.db == nil || strings.TrimSpace(requestKey) == "" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	item := &MailboxMessage{}
	var readAt, deliveryMode, externalRecipient, deliveryStatus, deliveryError, deliveredAt, storedKey sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id, recipient_id, sender_id, sender_name, subject_text, body_text, created_at, read_at, delivery_mode, external_recipient, delivery_status, delivery_error, delivered_at, request_key FROM cgu_mailbox_messages WHERE request_key = ? LIMIT 1`, requestKey).Scan(&item.ID, &item.RecipientID, &item.SenderID, &item.SenderName, &item.Subject, &item.Body, &item.CreatedAt, &readAt, &deliveryMode, &externalRecipient, &deliveryStatus, &deliveryError, &deliveredAt, &storedKey)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if readAt.Valid {
		item.ReadAt = readAt.String
	}
	if deliveryMode.Valid {
		item.DeliveryMode = deliveryMode.String
	}
	if externalRecipient.Valid {
		item.ExternalRecipient = externalRecipient.String
	}
	if deliveryStatus.Valid {
		item.DeliveryStatus = deliveryStatus.String
	}
	if deliveryError.Valid {
		item.DeliveryError = deliveryError.String
	}
	if deliveredAt.Valid {
		item.DeliveredAt = deliveredAt.String
	}
	if storedKey.Valid {
		item.RequestKey = storedKey.String
	}
	return item, nil
}

func (s *Store) updateMailboxDelivery(id, attemptStartedAt, status, deliveryError, deliveredAt string) (*MailboxMessage, *apiError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.mailbox {
		if item == nil || !strings.EqualFold(item.ID, strings.TrimSpace(id)) {
			continue
		}
		if strings.TrimSpace(item.DeliveryStartedAt) != strings.TrimSpace(attemptStartedAt) {
			return nil, apiErr(http.StatusConflict, "delivery_state_changed", "delivery state changed before the result could be saved")
		}
		previous := *item
		item.DeliveryStatus = strings.TrimSpace(status)
		item.DeliveryError = strings.TrimSpace(deliveryError)
		item.DeliveredAt = strings.TrimSpace(deliveredAt)
		if err := s.persistMailboxOutcomeLocked(item, mailboxDeliverySending, attemptStartedAt); err != nil {
			*item = previous
			if errors.Is(err, errMailboxDeliveryClaimed) {
				return nil, apiErr(http.StatusConflict, "delivery_state_changed", "delivery state changed before the result could be saved")
			}
			return nil, apiErr(http.StatusServiceUnavailable, "mailbox_persistence_failed", "delivery status could not be saved")
		}
		item.DeliveryStartedAt = ""
		copy := s.mailboxAdminViewLocked(item)
		return &copy, nil
	}
	return nil, apiErr(http.StatusNotFound, "message_not_found", "message not found")
}

func (s *Store) beginMailboxDelivery(id string) (*MailboxMessage, *apiError) {
	return s.beginMailboxDeliveryWithConfirmationMode(id, false, true)
}

func (s *Store) beginMailboxDeliveryWithConfirmation(id string, confirmUnknown bool) (*MailboxMessage, *apiError) {
	// This is the generic administrator retry entry point. Admission onboarding
	// messages must use the dedicated credential resend workflow instead.
	return s.beginMailboxDeliveryWithConfirmationMode(id, confirmUnknown, false)
}

// beginMailboxDeliveryWithConfirmationMode claims an SMTP delivery after all
// workflow-specific checks. Admission onboarding messages carry a one-time
// credential workflow and must never be retried through the generic mailbox
// endpoint: that path has no fresh password to deliver and can target a stale
// contact address. The approval/resend workflow opts in explicitly.
func (s *Store) beginMailboxDeliveryWithConfirmationMode(id string, confirmUnknown, allowAdmissionOnboarding bool) (*MailboxMessage, *apiError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.mailbox {
		if item == nil || !strings.EqualFold(item.ID, strings.TrimSpace(id)) {
			continue
		}
		if !allowAdmissionOnboarding && isAdmissionOnboardingMailbox(item) {
			return nil, apiErr(http.StatusConflict, "admission_credentials_resend_required", "use the dedicated admission credential resend action for this message")
		}
		if item.DeliveryMode != mailboxDeliveryModeSMTP {
			return nil, apiErr(http.StatusBadRequest, "external_delivery_not_requested", "external delivery was not requested for this message")
		}
		if s.db != nil {
			if err := s.refreshMailboxDeliveryLocked(item); err != nil {
				return nil, apiErr(http.StatusServiceUnavailable, "mailbox_persistence_failed", "delivery status could not be read")
			}
		}
		if item.DeliveryStatus == mailboxDeliverySending && mailboxDeliveryLeaseExpired(item.DeliveryStartedAt) {
			previous := *item
			previousStartedAt := item.DeliveryStartedAt
			item.DeliveryStatus = mailboxDeliveryUnknown
			item.DeliveryError = "SMTP outcome unknown after an expired delivery lease; confirm the relay did not accept it before retrying"
			if s.db != nil {
				if err := s.persistMailboxOutcomeLocked(item, mailboxDeliverySending, previousStartedAt); err != nil {
					*item = previous
					return nil, apiErr(http.StatusServiceUnavailable, "mailbox_persistence_failed", "expired delivery state could not be recovered")
				}
			}
			item.DeliveryStartedAt = ""
		}
		switch item.DeliveryStatus {
		case mailboxDeliverySent:
			return nil, apiErr(http.StatusConflict, "already_delivered", "message has already been delivered")
		case mailboxDeliverySending:
			return nil, apiErr(http.StatusConflict, "delivery_in_progress", "message delivery is still in progress")
		case mailboxDeliveryPending:
			// Pending means no relay attempt has started yet, so it is safe to
			// claim and send automatically. Only an unknown outcome needs
			// explicit operator confirmation before another SMTP attempt.
		case mailboxDeliveryUnknown:
			if !confirmUnknown {
				return nil, apiErr(http.StatusConflict, "delivery_outcome_unknown", "delivery outcome is unknown; confirm the relay did not accept it before retrying")
			}
		case mailboxDeliveryFailed, mailboxDeliveryNotConfigured:
			// A completed SMTP failure is safe to retry normally.
		default:
			return nil, apiErr(http.StatusConflict, "delivery_outcome_unknown", "delivery outcome is unknown; confirm the relay did not accept it before retrying")
		}
		previous := *item
		item.DeliveryStatus = mailboxDeliverySending
		item.DeliveryError = ""
		item.DeliveredAt = ""
		if err := s.claimMailboxDeliveryLocked(item, previous.DeliveryStatus); err != nil {
			*item = previous
			if errors.Is(err, errMailboxDeliveryClaimed) {
				return nil, apiErr(http.StatusConflict, "delivery_in_progress", "message delivery is already being handled")
			}
			return nil, apiErr(http.StatusServiceUnavailable, "mailbox_persistence_failed", "delivery status could not be saved")
		}
		copy := s.mailboxAdminViewLocked(item)
		return &copy, nil
	}
	return nil, apiErr(http.StatusNotFound, "message_not_found", "message not found")
}

func mailboxDeliveryLeaseExpired(startedAt string) bool {
	startedAt = strings.TrimSpace(startedAt)
	if startedAt == "" {
		return true
	}
	if separator := strings.IndexByte(startedAt, '|'); separator >= 0 {
		startedAt = startedAt[:separator]
	}
	value, err := time.Parse(time.RFC3339Nano, startedAt)
	return err != nil || time.Since(value) > mailboxDeliveryLease
}

func isAdmissionOnboardingMailbox(item *MailboxMessage) bool {
	if item == nil {
		return false
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(item.RequestKey)), "admission-approval:")
}

func (s *Store) refreshMailboxDeliveryLocked(item *MailboxMessage) error {
	if s.db == nil || item == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var status, deliveryError, deliveredAt, startedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT delivery_status, delivery_error, delivered_at, delivery_started_at FROM cgu_mailbox_messages WHERE id = ?`, item.ID).Scan(&status, &deliveryError, &deliveredAt, &startedAt)
	if err != nil {
		return err
	}
	if status.Valid {
		item.DeliveryStatus = status.String
	}
	if deliveryError.Valid {
		item.DeliveryError = deliveryError.String
	}
	if deliveredAt.Valid {
		item.DeliveredAt = deliveredAt.String
	}
	if startedAt.Valid {
		item.DeliveryStartedAt = startedAt.String
	}
	return nil
}

func (s *Store) markMailboxDeliveryUnknown(id, message, attemptStartedAt string) (*MailboxMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.mailbox {
		if item == nil || !strings.EqualFold(item.ID, strings.TrimSpace(id)) {
			continue
		}
		if strings.TrimSpace(item.DeliveryStartedAt) != strings.TrimSpace(attemptStartedAt) {
			return nil, errMailboxDeliveryClaimed
		}
		previous := *item
		item.DeliveryStatus = mailboxDeliveryUnknown
		item.DeliveryError = strings.TrimSpace(message)
		item.DeliveredAt = ""
		// Keep the in-memory state conservative even if the database is
		// temporarily unavailable, and make a best-effort durable write.
		persistErr := s.persistMailboxOutcomeLocked(item, mailboxDeliverySending, attemptStartedAt)
		if persistErr != nil {
			*item = previous
			log.Printf("CGU mailbox delivery status durability warning (%T)", persistErr)
		} else {
			item.DeliveryStartedAt = ""
		}
		copy := s.mailboxAdminViewLocked(item)
		return &copy, persistErr
	}
	return nil, nil
}

func (s *Store) persistMailboxOutcomeLocked(item *MailboxMessage, expectedStatus, expectedStartedAt string) error {
	if item == nil || s.db == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var result sql.Result
	var err error
	if strings.TrimSpace(expectedStartedAt) == "" {
		result, err = s.db.ExecContext(ctx, `UPDATE cgu_mailbox_messages SET delivery_status = ?, delivery_error = ?, delivered_at = ?, delivery_started_at = '' WHERE id = ? AND delivery_status = ? AND delivery_started_at = ''`, item.DeliveryStatus, item.DeliveryError, item.DeliveredAt, item.ID, expectedStatus)
	} else {
		result, err = s.db.ExecContext(ctx, `UPDATE cgu_mailbox_messages SET delivery_status = ?, delivery_error = ?, delivered_at = ?, delivery_started_at = '' WHERE id = ? AND delivery_status = ? AND delivery_started_at = ?`, item.DeliveryStatus, item.DeliveryError, item.DeliveredAt, item.ID, expectedStatus, expectedStartedAt)
	}
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return errMailboxDeliveryClaimed
	}
	return nil
}

func safeExternalDeliveryError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.Join(strings.Fields(err.Error()), " ")
	if len([]rune(message)) > 400 {
		message = string([]rune(message)[:400])
	}
	return message
}

func (s *Server) deliverMailboxExternally(ctx context.Context, item *MailboxMessage) (status, deliveryError, deliveredAt string) {
	sender, timeout := s.smtpSender()
	if sender == nil {
		return mailboxDeliveryNotConfigured, "SMTP is not configured", ""
	}
	if s.smtpSlots != nil {
		select {
		case s.smtpSlots <- struct{}{}:
			defer func() { <-s.smtpSlots }()
		default:
			return mailboxDeliveryFailed, "SMTP delivery is temporarily busy; retry later", ""
		}
	}
	sendContext, cancel := context.WithTimeout(ctx, timeout)
	err := sender.Send(sendContext, item.ExternalRecipient, item.Subject, item.Body)
	cancel()
	if err == nil {
		return mailboxDeliverySent, "", time.Now().UTC().Format(time.RFC3339)
	}
	var outcomeErr *SMTPDeliveryError
	if errors.As(err, &outcomeErr) && outcomeErr != nil && outcomeErr.OutcomeUnknown {
		return mailboxDeliveryUnknown, "SMTP outcome unknown after the relay transaction; confirm the relay did not accept it before retrying", ""
	}
	var timeoutError net.Error
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeoutError) && timeoutError.Timeout()) {
		return mailboxDeliveryUnknown, "SMTP outcome unknown after timeout or cancellation; confirm the relay did not accept it before retrying", ""
	}
	return mailboxDeliveryFailed, safeExternalDeliveryError(err), ""
}

func writeMailboxOutcomeUnknown(w http.ResponseWriter, item *MailboxMessage) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{
		"ok":      false,
		"error":   "delivery_outcome_unknown",
		"message": "SMTP delivery outcome is unknown; inspect the record before retrying",
		"details": item,
	})
}

func (s *Store) markMailboxRead(studentID, id string, read bool) (*MailboxMessage, *apiError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.mailbox {
		if item == nil || !strings.EqualFold(item.ID, strings.TrimSpace(id)) {
			continue
		}
		if item.RecipientID != studentID {
			return nil, apiErr(http.StatusForbidden, "forbidden", "students may only update their own mailbox")
		}
		previous := item.ReadAt
		if read {
			item.ReadAt = time.Now().UTC().Format(time.RFC3339)
		} else {
			item.ReadAt = ""
		}
		if err := s.persistMailboxReadLocked(item); err != nil {
			item.ReadAt = previous
			return nil, apiErr(http.StatusServiceUnavailable, "mailbox_persistence_failed", "message could not be updated")
		}
		copy := s.mailboxStudentViewLocked(item)
		return &copy, nil
	}
	return nil, apiErr(http.StatusNotFound, "message_not_found", "message not found")
}

func (s *Server) listMailbox(w http.ResponseWriter, r *http.Request) {
	user := s.currentUser(r)
	if user == nil || user.Role != "student" {
		writeError(w, apiErr(http.StatusForbidden, "student_required", "student role is required"))
		return
	}
	messages, unread := s.store.mailboxForStudent(user.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "email": studentMailbox(user.StudentID, s.store.studentEmailDomain),
		"unread": unread, "messages": messages,
	})
}

func (s *Server) markMailboxRead(w http.ResponseWriter, r *http.Request, id string) {
	user := s.currentUser(r)
	if user == nil || user.Role != "student" {
		writeError(w, apiErr(http.StatusForbidden, "student_required", "student role is required"))
		return
	}
	var input MailboxReadInput
	if err := decodeJSON(w, r, &input); err != nil || input.Read == nil {
		writeError(w, apiErr(http.StatusBadRequest, "invalid_input", "read must be a boolean"))
		return
	}
	item, apiError := s.store.markMailboxRead(user.ID, id, *input.Read)
	if apiError != nil {
		writeError(w, apiError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": item})
}

func (s *Server) listAdminMailbox(w http.ResponseWriter, r *http.Request) {
	studentRef := first(r.URL.Query().Get("student_id"), r.URL.Query().Get("user_id"))
	messages, apiError := s.store.mailboxForAdmin(studentRef)
	if apiError != nil {
		writeError(w, apiError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "messages": messages})
}

func (s *Server) sendAdminMailbox(w http.ResponseWriter, r *http.Request) {
	var input MailboxInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, apiErr(http.StatusBadRequest, "invalid_input", "mailbox message is required"))
		return
	}
	item, apiError := s.store.createMailboxMessage(s.currentUser(r), input)
	if apiError != nil {
		writeError(w, apiError)
		return
	}
	if item.Replayed {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": item, "replayed": true})
		return
	}
	if item.DeliveryMode == mailboxDeliveryModeSMTP {
		status, deliveryError, deliveredAt := s.deliverMailboxExternally(r.Context(), item)
		if updated, statusError := s.store.updateMailboxDelivery(item.ID, item.DeliveryStartedAt, status, deliveryError, deliveredAt); statusError == nil {
			item = updated
		} else {
			// The relay may already have accepted the message. Do not report a
			// retryable failure while the durable outcome is unknown.
			if unknown, persistErr := s.store.markMailboxDeliveryUnknown(item.ID, "SMTP outcome unknown; delivery status could not be saved", item.DeliveryStartedAt); unknown != nil {
				item = unknown
				if persistErr != nil {
					log.Printf("CGU mailbox unknown outcome remains process-local (%T)", persistErr)
				}
			}
			writeMailboxOutcomeUnknown(w, item)
			return
		}
	}
	w.Header().Set("Location", "/api/admin/mailbox")
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "message": item})
}

func (s *Server) retryAdminMailbox(w http.ResponseWriter, r *http.Request, id string) {
	var input struct {
		ConfirmUnknown bool `json:"confirmUnknown"`
	}
	if err := decodeJSON(w, r, &input); err != nil && err != io.EOF {
		writeError(w, apiErr(http.StatusBadRequest, "invalid_input", "retry confirmation is required"))
		return
	}
	item, apiError := s.store.beginMailboxDeliveryWithConfirmation(id, input.ConfirmUnknown)
	if apiError != nil {
		writeError(w, apiError)
		return
	}
	status, deliveryError, deliveredAt := s.deliverMailboxExternally(r.Context(), item)
	updated, statusError := s.store.updateMailboxDelivery(item.ID, item.DeliveryStartedAt, status, deliveryError, deliveredAt)
	if statusError != nil {
		if unknown, persistErr := s.store.markMailboxDeliveryUnknown(item.ID, "SMTP outcome unknown; delivery status could not be saved", item.DeliveryStartedAt); unknown != nil {
			item = unknown
			if persistErr != nil {
				log.Printf("CGU mailbox unknown outcome remains process-local (%T)", persistErr)
			}
		}
		writeMailboxOutcomeUnknown(w, item)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": updated})
}
