package main

import (
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	mailboxSubjectLimit = 200
	mailboxBodyLimit    = 10_000
)

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
	return copy
}

func (s *Store) mailboxAdminViewLocked(item *MailboxMessage) MailboxMessage {
	if item == nil {
		return MailboxMessage{}
	}
	copy := *item
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
	if err := s.persistMailboxLocked(item); err != nil {
		return nil, apiErr(http.StatusServiceUnavailable, "mailbox_persistence_failed", "message could not be saved")
	}
	s.mailbox = append(s.mailbox, item)
	copy := s.mailboxAdminViewLocked(item)
	return &copy, nil
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
	w.Header().Set("Location", "/api/admin/mailbox")
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "message": item})
}
