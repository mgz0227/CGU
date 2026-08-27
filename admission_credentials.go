package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const admissionCredentialUpdateSQL = `UPDATE cgu_admissions SET initial_password_issued_at = ?, updated_at = ? WHERE id = ?`
const admissionCredentialDeliveryStateSQL = `SELECT delivery_status, delivery_started_at FROM cgu_mailbox_messages WHERE request_key = ? FOR UPDATE`

var (
	errAdmissionCredentialDeliveryInProgress = errors.New("admission credential delivery is in progress")
)

// admissionCredentialRotationAllowed prevents a second process from
// rotating the password while the first process is still sending the one-time
// welcome message. The mailbox row is locked by the caller, so this decision
// is durable across application instances sharing MySQL.
func admissionCredentialRotationAllowed(deliveryStatus, deliveryStartedAt, issuedAt string) error {
	deliveryStatus = strings.ToLower(strings.TrimSpace(deliveryStatus))
	switch deliveryStatus {
	case mailboxDeliverySending:
		if !mailboxDeliveryLeaseExpired(deliveryStartedAt) {
			return errAdmissionCredentialDeliveryInProgress
		}
	case mailboxDeliveryPending:
		// A freshly approved application has an issue timestamp but no SMTP
		// lease until the post-commit delivery starts. Keep that small window
		// from being raced by a resend on another instance. If a process died
		// before claiming the message, the lease naturally expires and an
		// administrator can recover it.
		if !admissionCredentialTimestampExpired(issuedAt) {
			return errAdmissionCredentialDeliveryInProgress
		}
	}
	return nil
}

func admissionCredentialTimestampExpired(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		// An unreadable timestamp cannot prove that a delivery lease expired;
		// fail closed and require the normal mailbox recovery path.
		return false
	}
	return time.Since(parsed) > mailboxDeliveryLease
}

func admissionCredentialDeliveryAPIError(err error) *apiError {
	if errors.Is(err, errAdmissionCredentialDeliveryInProgress) {
		return apiErr(http.StatusConflict, "delivery_in_progress", "the previous credential email is still being delivered; try again after it finishes")
	}
	return nil
}

// loadUniqueAdmissionStudentTx resolves an accepted application's student
// without LIMIT 1. Duplicate legacy external ids are an ambiguity, not a
// reason to rotate an arbitrary account's password.
func loadUniqueAdmissionStudentTx(ctx context.Context, tx *sql.Tx, studentID string) (*User, error) {
	studentID = strings.TrimSpace(studentID)
	if studentID == "" {
		return nil, sql.ErrNoRows
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, username, name_text, email, role_name, password_hash, student_id, college, year_text, disabled_flag FROM cgu_users WHERE student_id = ? AND role_name = 'student' ORDER BY id FOR UPDATE`, studentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var student *User
	for rows.Next() {
		candidate, scanErr := scanUserDeleteRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		if student != nil && !strings.EqualFold(student.ID, candidate.ID) {
			return nil, errStudentIDAmbiguous
		}
		student = candidate
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if student == nil {
		return nil, sql.ErrNoRows
	}
	return student, nil
}

func admissionCredentialMailbox(student *User, application *AdmissionApplication, approvedBy, mailboxID, emailDomain string) *MailboxMessage {
	if student == nil || application == nil {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(application.UpdatedAt) != "" {
		now = application.UpdatedAt
	}
	if strings.TrimSpace(mailboxID) == "" {
		_, _, userID := admissionStudentIdentity(application.ID)
		mailboxID = "mail-admission-" + strings.TrimPrefix(userID, "student-")
	}
	approvedBy = strings.TrimSpace(approvedBy)
	if approvedBy == "" {
		approvedBy = first(application.ApprovedBy, "admin")
	}
	return &MailboxMessage{
		ID: mailboxID, RecipientID: student.ID, RecipientName: student.Name,
		RecipientStudentID: student.StudentID,
		RecipientEmail:     studentMailbox(student.StudentID, emailDomain),
		SenderID:           approvedBy, SenderName: "CGU 教务处", Subject: "CGU 学生账户已建立",
		Body:      fmt.Sprintf("你的 CGU 学生档案已建立。校内邮箱：%s。初始密码将通过申请邮箱单独发送。", studentMailbox(student.StudentID, emailDomain)),
		CreatedAt: now, DeliveryMode: mailboxDeliveryModeSMTP,
		ExternalRecipient: application.Email, DeliveryStatus: mailboxDeliveryPending,
		RequestKey: admissionApprovalRequestKey(application.ID),
	}
}

func resetAdmissionMailboxMemoryLocked(s *Store, student *User, application *AdmissionApplication, approvedBy string) *MailboxMessage {
	if s == nil || student == nil || application == nil {
		return nil
	}
	requestKey := admissionApprovalRequestKey(application.ID)
	for _, item := range s.mailbox {
		if item == nil || !strings.EqualFold(item.RequestKey, requestKey) {
			continue
		}
		mailbox := admissionCredentialMailbox(student, application, approvedBy, item.ID, s.studentEmailDomain)
		if mailbox == nil {
			return nil
		}
		mailbox.CreatedAt = item.CreatedAt
		mailbox.ReadAt = item.ReadAt
		*item = *mailbox
		return item
	}
	mailbox := admissionCredentialMailbox(student, application, approvedBy, "", s.studentEmailDomain)
	if mailbox == nil {
		return nil
	}
	s.mailbox = append(s.mailbox, mailbox)
	return mailbox
}

// resendAdmissionCredentials rotates the student's password and queues a new
// one-time SMTP delivery. The plaintext secret is returned only in the
// unexported delivery field of AdmissionApproval and is cleared by the server
// after the send attempt.
func (s *Store) resendAdmissionCredentials(id, approvedBy string) (*AdmissionApproval, *apiError) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, apiErr(http.StatusNotFound, "admission_not_found", "application not found")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return s.resendAdmissionCredentialsDatabaseLocked(id, approvedBy)
	}
	application, _ := s.findAdmissionLocked(id)
	if application == nil {
		return nil, apiErr(http.StatusNotFound, "admission_not_found", "application not found")
	}
	if !strings.EqualFold(strings.TrimSpace(application.Status), "accepted") || strings.TrimSpace(application.StudentID) == "" {
		return nil, apiErr(http.StatusConflict, "approval_incomplete", "approved application has no linked student account")
	}
	if !validateContactEmail(application.Email) {
		return nil, apiErr(http.StatusConflict, "external_recipient_invalid", "applicant email is not eligible for notification")
	}
	// Resolve the same ownership hierarchy used by admission deletion. A
	// generated account is authoritative; legacy primary-key references are
	// accepted directly; an external student id must identify exactly one
	// account. The older resolveStudentLocked helper returns the first map match
	// and could rotate credentials for the wrong account after a legacy import
	// created duplicate student ids.
	if s.generatedAdmissionIdentityMismatchLocked(application.ID, application.StudentID) {
		return nil, studentIdentityMismatchAPIError()
	}
	resolved, ambiguous := s.cachedAdmissionStudentResolutionLocked(application.ID, application.StudentID)
	if ambiguous {
		return nil, studentIDAmbiguousAPIError()
	}
	if resolved == nil || !strings.EqualFold(strings.TrimSpace(resolved.Role), "student") || resolved.Disabled {
		return nil, apiErr(http.StatusConflict, "approval_incomplete", "approved application has no linked student account")
	}
	// The resolver returns a defensive copy. Find the authoritative map entry
	// before rotating its hash so stale map aliases cannot create a second,
	// detached account projection.
	var student *User
	for key, candidate := range s.users {
		if candidate == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(candidate.ID), strings.TrimSpace(resolved.ID)) || strings.EqualFold(strings.TrimSpace(key), strings.TrimSpace(resolved.ID)) {
			student = candidate
			break
		}
	}
	if student == nil {
		return nil, apiErr(http.StatusConflict, "approval_incomplete", "approved application has no linked student account")
	}
	if mailbox := s.admissionApprovalMailboxLocked(application.ID); mailbox != nil {
		if deliveryErr := admissionCredentialRotationAllowed(mailbox.DeliveryStatus, mailbox.DeliveryStartedAt, application.InitialPasswordIssuedAt); deliveryErr != nil {
			return nil, admissionCredentialDeliveryAPIError(deliveryErr)
		}
	}
	password := newAdmissionInitialPassword()
	passwordHash, err := hashPasswordChecked(password)
	if err != nil {
		return nil, apiErr(http.StatusInternalServerError, "password_hash_failed", "student password could not be secured")
	}
	student.PasswordHash = passwordHash
	now := time.Now().UTC().Format(time.RFC3339)
	application.InitialPasswordIssuedAt = now
	application.UpdatedAt = now
	mailbox := resetAdmissionMailboxMemoryLocked(s, student, application, approvedBy)
	if mailbox == nil {
		return nil, apiErr(http.StatusServiceUnavailable, "mailbox_persistence_failed", "student welcome message could not be prepared")
	}
	result := s.admissionApprovalViewLocked(application, student, password, false)
	return result, nil
}

func (s *Store) resendAdmissionCredentialsDatabaseLocked(id, approvedBy string) (*AdmissionApproval, *apiError) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, apiErr(http.StatusServiceUnavailable, "admission_approval_persistence_failed", "credential rotation could not be started")
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	application, err := scanAdmissionApprovalRow(tx.QueryRowContext(ctx, admissionForUpdateSQL, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apiErr(http.StatusNotFound, "admission_not_found", "application not found")
	}
	if err != nil {
		return nil, apiErr(http.StatusServiceUnavailable, "admission_approval_persistence_failed", "application could not be read")
	}
	if !strings.EqualFold(strings.TrimSpace(application.Status), "accepted") || strings.TrimSpace(application.StudentID) == "" {
		return nil, apiErr(http.StatusConflict, "approval_incomplete", "approved application has no linked student account")
	}
	// Legacy/imported rows may bypass the public admission validator. Reject
	// them before rotating the password so an invalid recipient can never leave
	// the student with a credential that cannot be delivered.
	if !validateContactEmail(application.Email) {
		return nil, apiErr(http.StatusConflict, "external_recipient_invalid", "applicant email is not eligible for notification")
	}
	var storedDeliveryStatus, storedDeliveryStartedAt sql.NullString
	deliveryStateErr := tx.QueryRowContext(ctx, admissionCredentialDeliveryStateSQL, admissionApprovalRequestKey(application.ID)).Scan(&storedDeliveryStatus, &storedDeliveryStartedAt)
	if deliveryStateErr != nil && !errors.Is(deliveryStateErr, sql.ErrNoRows) {
		return nil, apiErr(http.StatusServiceUnavailable, "admission_approval_persistence_failed", "credential delivery state could not be read")
	}
	if deliveryStateErr == nil {
		status, startedAt := "", ""
		if storedDeliveryStatus.Valid {
			status = storedDeliveryStatus.String
		}
		if storedDeliveryStartedAt.Valid {
			startedAt = storedDeliveryStartedAt.String
		}
		if rotationErr := admissionCredentialRotationAllowed(status, startedAt, application.InitialPasswordIssuedAt); rotationErr != nil {
			return nil, admissionCredentialDeliveryAPIError(rotationErr)
		}
	}
	// Resolve ownership in the same order as the delete cascade. This supports
	// new deterministic accounts, legacy application rows that stored a user
	// primary key, and old external student ids without ever choosing an
	// arbitrary duplicate.
	_, _, deterministicUserID := admissionStudentIdentity(application.ID)
	candidates, err := loadAdmissionDeleteStudentCandidates(ctx, tx, deterministicUserID, application.StudentID)
	if err != nil {
		return nil, apiErr(http.StatusServiceUnavailable, "admission_approval_persistence_failed", "student account could not be read")
	}
	student, err := chooseAdmissionDeleteStudent(candidates, deterministicUserID, application.StudentID)
	if errors.Is(err, errStudentIDAmbiguous) {
		return nil, studentIDAmbiguousAPIError()
	}
	if errors.Is(err, errStudentIdentityMismatch) {
		return nil, studentIdentityMismatchAPIError()
	}
	if err != nil || student == nil || student.Disabled || !strings.EqualFold(strings.TrimSpace(student.Role), "student") {
		return nil, apiErr(http.StatusConflict, "approval_incomplete", "approved application has no linked student account")
	}
	password := newAdmissionInitialPassword()
	passwordHash, err := hashPasswordChecked(password)
	if err != nil {
		return nil, apiErr(http.StatusInternalServerError, "password_hash_failed", "student password could not be secured")
	}
	result, err := tx.ExecContext(ctx, `UPDATE cgu_users SET password_hash = ? WHERE id = ? AND role_name = 'student'`, passwordHash, student.ID)
	if err != nil {
		return nil, apiErr(http.StatusServiceUnavailable, "admission_approval_persistence_failed", "student password could not be saved")
	}
	if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
		return nil, apiErr(http.StatusConflict, "approval_incomplete", "student account could not be updated")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	application.InitialPasswordIssuedAt = now
	application.UpdatedAt = now
	approvedBy = strings.TrimSpace(approvedBy)
	if approvedBy == "" {
		approvedBy = first(application.ApprovedBy, "admin")
	}
	requestKey := admissionApprovalRequestKey(application.ID)
	var mailboxID string
	mailboxErr := tx.QueryRowContext(ctx, `SELECT id FROM cgu_mailbox_messages WHERE request_key = ? FOR UPDATE`, requestKey).Scan(&mailboxID)
	mailbox := admissionCredentialMailbox(student, application, approvedBy, mailboxID, s.studentEmailDomain)
	if errors.Is(mailboxErr, sql.ErrNoRows) {
		mailbox = admissionCredentialMailbox(student, application, approvedBy, "", s.studentEmailDomain)
		if mailbox == nil {
			return nil, apiErr(http.StatusInternalServerError, "mailbox_persistence_failed", "student welcome message could not be prepared")
		}
		if _, err := tx.ExecContext(ctx, mailboxInsertSQL, mailbox.ID, mailbox.RecipientID, mailbox.SenderID, mailbox.SenderName, mailbox.Subject, mailbox.Body, mailbox.CreatedAt, nil, mailbox.DeliveryMode, mailbox.ExternalRecipient, mailbox.DeliveryStatus, "", "", nullableString(mailbox.RequestKey), ""); err != nil {
			return nil, apiErr(http.StatusServiceUnavailable, "mailbox_persistence_failed", "student welcome message could not be saved")
		}
	} else if mailboxErr != nil {
		return nil, apiErr(http.StatusServiceUnavailable, "mailbox_persistence_failed", "student welcome message could not be read")
	} else {
		if mailbox == nil {
			return nil, apiErr(http.StatusInternalServerError, "mailbox_persistence_failed", "student welcome message could not be prepared")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE cgu_mailbox_messages SET recipient_id = ?, sender_id = ?, sender_name = ?, subject_text = ?, body_text = ?, delivery_mode = ?, external_recipient = ?, delivery_status = ?, delivery_error = NULL, delivered_at = '', delivery_started_at = '' WHERE id = ?`, mailbox.RecipientID, mailbox.SenderID, mailbox.SenderName, mailbox.Subject, mailbox.Body, mailbox.DeliveryMode, mailbox.ExternalRecipient, mailbox.DeliveryStatus, mailbox.ID); err != nil {
			return nil, apiErr(http.StatusServiceUnavailable, "mailbox_persistence_failed", "student welcome message could not be reset")
		}
	}
	if _, err := tx.ExecContext(ctx, admissionCredentialUpdateSQL, application.InitialPasswordIssuedAt, application.UpdatedAt, application.ID); err != nil {
		return nil, apiErr(http.StatusServiceUnavailable, "admission_approval_persistence_failed", "credential issue time could not be saved")
	}
	if err := tx.Commit(); err != nil {
		return nil, apiErr(http.StatusServiceUnavailable, "admission_approval_persistence_failed", "credential rotation could not be finalized")
	}
	committed = true
	student.PasswordHash = passwordHash
	s.users[student.ID] = student
	stored := s.upsertAdmissionMemoryLocked(application)
	updatedMailbox := false
	for _, item := range s.mailbox {
		if item != nil && strings.EqualFold(item.ID, mailbox.ID) {
			*item = *mailbox
			updatedMailbox = true
			break
		}
	}
	if !updatedMailbox {
		s.mailbox = append(s.mailbox, mailbox)
	}
	return s.admissionApprovalViewLocked(stored, student, password, false), nil
}
