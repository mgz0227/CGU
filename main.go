// China Genshin University campus service.
//
// The server is a single Go binary. It can run with seeded in-memory data or
// use the optional MySQL adapter configured through environment variables.
package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/mail"
	"net/netip"
	"net/url"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookie           = "cgu_session"
	sessionTTL              = 8 * time.Hour
	bodyLimit               = 1 << 20
	loginWindow             = 5 * time.Minute
	loginBlock              = 15 * time.Minute
	loginMaxFails           = 8
	loginMaxKeys            = 10_000
	admissionWindow         = time.Hour
	admissionMax            = 20
	admissionMaxKeys        = 10_000
	maxSessions             = 50_000
	maxPasswordChecks       = 32
	maxPasswordBytes        = 72
	maxLoginIdentifierBytes = 254
	dummyBcrypt             = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
)

type User struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	Name         string `json:"name"`
	Email        string `json:"email"`
	Role         string `json:"role"`
	PasswordHash string `json:"-"`
	StudentID    string `json:"studentId,omitempty"`
	College      string `json:"college,omitempty"`
	Year         string `json:"year,omitempty"`
	Disabled     bool   `json:"-"`
}

// AdminStudent is the administrator-facing student directory projection. It
// intentionally has no password or password-hash field.
type AdminStudent struct {
	ID                string `json:"id"`
	Username          string `json:"username"`
	Name              string `json:"name"`
	Email             string `json:"email"`
	StudentEmail      string `json:"studentEmail"`
	StudentID         string `json:"studentId"`
	College           string `json:"college"`
	Year              string `json:"year"`
	Role              string `json:"role"`
	AdmissionApproved bool   `json:"admissionApproved,omitempty"`
	Active            bool   `json:"active"`
}

type Course struct {
	ID            string  `json:"id"`
	Code          string  `json:"code"`
	NameZh        string  `json:"nameZh"`
	NameEn        string  `json:"nameEn"`
	Department    string  `json:"department"`
	DepartmentEn  string  `json:"departmentEn"`
	Teacher       string  `json:"teacher"`
	TeacherEn     string  `json:"teacherEn"`
	Credits       float64 `json:"credits"`
	Description   string  `json:"description"`
	DescriptionEn string  `json:"descriptionEn"`
	Capacity      int     `json:"capacity"`
	EnrolledCount int     `json:"enrolledCount"`
	Enrolled      bool    `json:"enrolled"`
	Type          string  `json:"type"`
	Term          string  `json:"term"`
	TermEn        string  `json:"termEn"`
}

type Enrollment struct {
	ID        string `json:"id"`
	StudentID string `json:"studentId"`
	CourseID  string `json:"courseId"`
	Term      string `json:"term"`
	Status    string `json:"status"`
}

type Grade struct {
	ID           string `json:"id"`
	StudentID    string `json:"studentId"`
	CourseID     string `json:"courseId"`
	CourseCode   string `json:"courseCode"`
	CourseNameZh string `json:"courseNameZh"`
	CourseNameEn string `json:"courseNameEn"`
	Score        any    `json:"score"`
	Point        any    `json:"point"`
	Term         string `json:"term"`
	Status       string `json:"status"`
	Credits      int    `json:"credits"`
}

type ScheduleEntry struct {
	ID           string `json:"id"`
	StudentID    string `json:"studentId"`
	CourseID     string `json:"courseId"`
	CourseCode   string `json:"courseCode"`
	CourseNameZh string `json:"courseNameZh"`
	CourseNameEn string `json:"courseNameEn"`
	Day          int    `json:"day"`
	Start        string `json:"start"`
	End          string `json:"end"`
	Location     string `json:"location"`
	Teacher      string `json:"teacher"`
}

type StudentInput struct {
	Username  string `json:"username"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	StudentID string `json:"studentId"`
	College   string `json:"college"`
	Year      string `json:"year"`
	Password  string `json:"password"`
	Active    *bool  `json:"active"`
}

type GradeInput struct {
	ID           string `json:"id"`
	StudentID    string `json:"studentId"`
	CourseID     string `json:"courseId"`
	CourseCode   string `json:"courseCode"`
	CourseNameZh string `json:"courseNameZh"`
	CourseNameEn string `json:"courseNameEn"`
	Score        any    `json:"score"`
	Point        any    `json:"point"`
	Term         string `json:"term"`
	Status       string `json:"status"`
	Credits      *int   `json:"credits"`
}

type ScheduleInput struct {
	ID           string `json:"id"`
	StudentID    string `json:"studentId"`
	CourseID     string `json:"courseId"`
	CourseCode   string `json:"courseCode"`
	CourseNameZh string `json:"courseNameZh"`
	CourseNameEn string `json:"courseNameEn"`
	Day          *int   `json:"day"`
	Start        string `json:"start"`
	End          string `json:"end"`
	Location     string `json:"location"`
	Teacher      string `json:"teacher"`
}

// Mailbox messages are internal CGU academic notices with optional, recorded
// external delivery metadata. SMTP credentials stay in deployment config and
// are never part of this model or any student-facing projection.
type MailboxMessage struct {
	ID                 string `json:"id"`
	RecipientID        string `json:"recipientId,omitempty"`
	RecipientName      string `json:"recipientName,omitempty"`
	RecipientStudentID string `json:"recipientStudentId,omitempty"`
	RecipientEmail     string `json:"recipientEmail,omitempty"`
	SenderID           string `json:"senderId,omitempty"`
	SenderName         string `json:"senderName"`
	Subject            string `json:"subject"`
	Body               string `json:"body"`
	CreatedAt          string `json:"createdAt"`
	ReadAt             string `json:"readAt,omitempty"`
	DeliveryMode       string `json:"deliveryMode,omitempty"`
	ExternalRecipient  string `json:"externalRecipient,omitempty"`
	DeliveryStatus     string `json:"deliveryStatus,omitempty"`
	DeliveryError      string `json:"deliveryError,omitempty"`
	DeliveredAt        string `json:"deliveredAt,omitempty"`
	RequestKey         string `json:"-"`
	Replayed           bool   `json:"-"`
	DeliveryStartedAt  string `json:"-"`
}

type MailboxInput struct {
	StudentID         string `json:"studentId"`
	StudentRef        string `json:"student_id"`
	Subject           string `json:"subject"`
	Title             string `json:"title"`
	Body              string `json:"body"`
	Message           string `json:"message"`
	Content           string `json:"content"`
	External          *bool  `json:"external"`
	SendExternal      *bool  `json:"sendExternal"`
	SendExternalSnake *bool  `json:"send_external"`
	IdempotencyKey    string `json:"idempotencyKey"`
	RequestKey        string `json:"requestKey"`
}

type MailboxReadInput struct {
	Read *bool `json:"read"`
}

type Announcement struct {
	ID          string `json:"id"`
	TitleZh     string `json:"titleZh"`
	TitleEn     string `json:"titleEn"`
	ContentZh   string `json:"contentZh"`
	ContentEn   string `json:"contentEn"`
	Type        string `json:"type"`
	Audience    string `json:"audience"`
	CourseID    string `json:"courseId,omitempty"`
	PublishedAt string `json:"publishedAt"`
	Published   bool   `json:"published"`
	Author      string `json:"author"`
}

// SiteContent is a bilingual, administrator-managed override for frontend
// copy. The key matches the data-i18n keys used by the web clients.
type SiteContent struct {
	Key       string `json:"key"`
	Zh        string `json:"zh"`
	En        string `json:"en"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

type SiteContentInput struct {
	Key string `json:"key"`
	Zh  string `json:"zh"`
	En  string `json:"en"`
}

// AdmissionApplication is a public admissions lead that can be reviewed by
// administrators. Email and other contact details are only returned from the
// administrator endpoint; the public API returns the created record once so
// the client can display a confirmation identifier.
type AdmissionApplication struct {
	ID                      string `json:"id"`
	Name                    string `json:"name"`
	Email                   string `json:"email"`
	School                  string `json:"school"`
	Status                  string `json:"status"`
	Notes                   string `json:"notes,omitempty"`
	CreatedAt               string `json:"createdAt"`
	UpdatedAt               string `json:"updatedAt,omitempty"`
	StudentID               string `json:"studentId,omitempty"`
	ApprovedAt              string `json:"approvedAt,omitempty"`
	ApprovedBy              string `json:"approvedBy,omitempty"`
	DeliveryStatus          string `json:"deliveryStatus,omitempty"`
	DeliveryError           string `json:"deliveryError,omitempty"`
	InitialPasswordIssuedAt string `json:"-"`
}

// AdmissionApproval is returned by the explicit administrator approval
// action. The initial password is deliberately response-only: it is never
// persisted, included in an application projection, or written to a mailbox.
type AdmissionApproval struct {
	Application     AdmissionApplication `json:"application"`
	Student         AdminStudent         `json:"student"`
	InitialPassword string               `json:"initialPassword,omitempty"`
	AlreadyApproved bool                 `json:"alreadyApproved"`
	MailboxID       string               `json:"mailboxId,omitempty"`
	DeliveryStatus  string               `json:"deliveryStatus,omitempty"`
	DeliveryError   string               `json:"deliveryError,omitempty"`
}

type AdmissionApplicationInput struct {
	Name       string `json:"name"`
	Email      string `json:"email"`
	School     string `json:"school"`
	Status     string `json:"status"`
	Notes      string `json:"notes"`
	ClearNotes *bool  `json:"clearNotes"`
}

// AdminNotification is a durable, administrator-only event generated by
// workflows that need attention. Contact details are intentionally confined
// to this authenticated projection; public admissions responses never expose
// the notification body.
type AdminNotification struct {
	ID          string `json:"id"`
	RecipientID string `json:"-"`
	Type        string `json:"type"`
	TitleZh     string `json:"titleZh"`
	TitleEn     string `json:"titleEn"`
	BodyZh      string `json:"bodyZh"`
	BodyEn      string `json:"bodyEn"`
	ReferenceID string `json:"referenceId,omitempty"`
	CreatedAt   string `json:"createdAt"`
	ReadAt      string `json:"readAt,omitempty"`
}

type AdminNotificationReadInput struct {
	Read *bool `json:"read"`
}

// Input structs accept the bilingual field names emitted by portal.js and a
// few common snake_case aliases used by older integrations.
type LoginRequest struct {
	Username string `json:"username"`
	Account  string `json:"account"`
	Password string `json:"password"`
}

// PasswordChangeInput is intentionally separate from LoginRequest so a
// password rotation can never be mistaken for a sign-in payload. The alias
// fields keep integrations written against the earlier portal contract
// compatible without exposing a password in any response model.
type PasswordChangeInput struct {
	CurrentPassword string `json:"currentPassword"`
	Current         string `json:"current"`
	NewPassword     string `json:"newPassword"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirmPassword"`
}

type EnrollmentRequest struct {
	CourseID string `json:"courseId"`
	Action   string `json:"action"`
}

type CourseInput struct {
	ID            string   `json:"id"`
	Code          string   `json:"code"`
	Name          string   `json:"name"`
	NameZh        string   `json:"nameZh"`
	NameEn        string   `json:"nameEn"`
	Department    string   `json:"department"`
	DepartmentEn  string   `json:"departmentEn"`
	Teacher       string   `json:"teacher"`
	TeacherEn     string   `json:"teacherEn"`
	Credits       *float64 `json:"credits"`
	Description   string   `json:"description"`
	DescriptionEn string   `json:"descriptionEn"`
	Capacity      *int     `json:"capacity"`
	Term          string   `json:"term"`
	TermEn        string   `json:"termEn"`
	Type          string   `json:"type"`
	ClearFields   []string `json:"clearFields"`
}

type AnnouncementInput struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	TitleZh      string   `json:"titleZh"`
	TitleEn      string   `json:"titleEn"`
	Body         string   `json:"body"`
	Content      string   `json:"content"`
	ContentZh    string   `json:"contentZh"`
	ContentEn    string   `json:"contentEn"`
	Type         string   `json:"type"`
	Audience     string   `json:"audience"`
	CourseID     string   `json:"courseId"`
	PublishedAt  string   `json:"publishedAt"`
	PublishedAt2 string   `json:"published_at"`
	Published    *bool    `json:"published"`
	ClearFields  []string `json:"clearFields"`
}

type apiError struct {
	Status  int
	Code    string
	Message string
}

func (e *apiError) Error() string { return e.Message }

func apiErr(status int, code, message string) *apiError {
	return &apiError{Status: status, Code: code, Message: message}
}

type session struct {
	UserID  string
	Expires time.Time
}

type Store struct {
	mu                 sync.RWMutex
	db                 *sql.DB
	studentEmailDomain string
	users              map[string]*User
	courses            []*Course
	enrollments        []*Enrollment
	grades             []*Grade
	schedule           []*ScheduleEntry
	announcements      []*Announcement
	admissions         []*AdmissionApplication
	notifications      []*AdminNotification
	mailbox            []*MailboxMessage
	siteContent        map[string]*SiteContent
}

func NewStore() *Store {
	cfg := LoadConfig()
	return NewStoreWithAdminAndDomain(cfg.AdminUsername, cfg.AdminPassword, cfg.StudentEmailDomain)
}

// NewStoreWithAdmin creates the in-memory store and, when a password is
// supplied, exactly one bootstrap administrator. An empty password is useful
// for public-route tests but is rejected by main before serving requests.
func NewStoreWithAdmin(username, password string) *Store {
	return NewStoreWithAdminAndDomain(username, password, "cgu.edu.kg")
}

func NewStoreWithAdminAndDomain(username, password, emailDomain string) *Store {
	username = strings.TrimSpace(username)
	if username == "" {
		username = "admin"
	}
	s := &Store{studentEmailDomain: normalizeStudentEmailDomain(emailDomain), users: make(map[string]*User), siteContent: defaultSiteContent()}
	if strings.TrimSpace(password) != "" {
		if passwordHash, err := hashPasswordChecked(password); err == nil {
			s.users["admin"] = &User{
				ID: "admin", Username: username, Name: "教务处", Email: "admin@cgu.local", Role: "admin",
				PasswordHash: passwordHash,
			}
		}
	}
	s.seed()
	return s
}

func (s *Store) authenticate(identifier, password string) *User {
	// bcrypt rejects passwords longer than 72 bytes. Reject them before the
	// lookup so a legacy PBKDF2 account cannot trigger a failed hash upgrade.
	if len([]byte(password)) > maxPasswordBytes {
		_ = bcrypt.CompareHashAndPassword([]byte(dummyBcrypt), []byte("invalid-password"))
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for _, user := range s.users {
		if user == nil || user.Disabled {
			continue
		}
		studentEmail := user.Role == "student" && strings.EqualFold(studentMailbox(user.StudentID, s.studentEmailDomain), identifier)
		if strings.EqualFold(user.Username, identifier) || strings.EqualFold(user.Email, identifier) || studentEmail {
			found = true
			if !verifyPassword(password, user.PasswordHash) {
				return nil
			}
			// Upgrade hashes created by older builds after a successful login.
			if strings.HasPrefix(user.PasswordHash, "pbkdf2-sha256$") {
				if passwordHash, err := hashPasswordChecked(password); err == nil {
					user.PasswordHash = passwordHash
					s.persistUserLocked(user)
				}
			}
			copy := *user
			return &copy
		}
	}
	if !found {
		// Keep unknown-account timing close to a real password check.
		_ = bcrypt.CompareHashAndPassword([]byte(dummyBcrypt), []byte(password))
	}
	return nil
}

func (s *Store) user(id string) *User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[id]
	if !ok {
		return nil
	}
	copy := *u
	return &copy
}

func (s *Store) publicUser(user *User) map[string]any {
	s.mu.RLock()
	students := 0
	for _, item := range s.users {
		if item != nil && item.Role == "student" && !item.Disabled {
			students++
		}
	}
	s.mu.RUnlock()
	studentEmail := ""
	if user.Role == "student" {
		studentEmail = studentMailbox(user.StudentID, s.studentEmailDomain)
	}
	return map[string]any{
		"id": user.ID, "username": user.Username, "role": user.Role, "name": user.Name,
		"email": user.Email, "studentEmail": studentEmail, "studentId": user.StudentID, "college": user.College, "year": user.Year,
		"stats": map[string]any{"students": students},
	}
}

func (s *Store) adminStudentView(user *User) AdminStudent {
	if user == nil {
		return AdminStudent{}
	}
	return AdminStudent{
		ID: user.ID, Username: user.Username, Name: user.Name, Email: user.Email,
		StudentEmail: studentMailbox(user.StudentID, s.studentEmailDomain), StudentID: user.StudentID,
		College: user.College, Year: user.Year, Role: user.Role, Active: !user.Disabled,
		AdmissionApproved: s.studentHasApprovedAdmissionLocked(user.StudentID),
	}
}

// studentHasApprovedAdmissionLocked identifies accounts provisioned by the
// admissions workflow. Their generated student ID is a durable foreign key in
// the admission and mailbox projections, so changing it would break approval
// replay and orphan those records.
func (s *Store) studentHasApprovedAdmissionLocked(studentID string) bool {
	studentID = strings.TrimSpace(studentID)
	if studentID == "" {
		return false
	}
	for _, admission := range s.admissions {
		if admission != nil && strings.EqualFold(strings.TrimSpace(admission.StudentID), studentID) && strings.EqualFold(strings.TrimSpace(admission.Status), "accepted") {
			return true
		}
	}
	return false
}

func (s *Store) studentsForAdmin() []AdminStudent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]AdminStudent, 0)
	for _, user := range s.users {
		if user == nil || user.Role != "student" {
			continue
		}
		result = append(result, s.adminStudentView(user))
	}
	sort.SliceStable(result, func(i, j int) bool {
		return strings.ToLower(result[i].StudentID) < strings.ToLower(result[j].StudentID)
	})
	return result
}

func (s *Store) resolveStudentLocked(value string) *User {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	for _, user := range s.users {
		if user == nil || user.Role != "student" || user.Disabled {
			continue
		}
		if strings.EqualFold(user.ID, value) || strings.EqualFold(user.StudentID, value) || strings.EqualFold(user.Username, value) || strings.EqualFold(user.Email, value) || strings.EqualFold(studentMailbox(user.StudentID, s.studentEmailDomain), value) {
			return user
		}
	}
	return nil
}

func (s *Store) studentRecordID(value string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if student := s.resolveStudentLocked(value); student != nil {
		return student.ID
	}
	return ""
}

func validAcademicIdentifier(value string, max int) bool {
	value = strings.TrimSpace(value)
	if value == "" || len([]rune(value)) > max {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func validateContactEmail(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 254 || strings.ContainsAny(value, "\r\n") {
		return false
	}
	parsed, err := mail.ParseAddress(value)
	return err == nil && parsed.Address == value && strings.Contains(value, "@")
}

func validateStudentPassword(value string) bool {
	return len([]byte(value)) >= 12 && len([]byte(value)) <= maxPasswordBytes && !strings.ContainsAny(value, "\r\n")
}

func (s *Store) studentLoginIdentifiers(username, email, studentID string) []string {
	values := []string{strings.TrimSpace(username), strings.TrimSpace(email), studentMailbox(studentID, s.studentEmailDomain)}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		duplicate := false
		for _, existing := range result {
			if strings.EqualFold(existing, value) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			result = append(result, value)
		}
	}
	return result
}

func identifiersOverlap(left, right []string) bool {
	for _, first := range left {
		for _, second := range right {
			if strings.EqualFold(first, second) {
				return true
			}
		}
	}
	return false
}

func (s *Store) createStudent(input StudentInput) (*AdminStudent, *apiError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	username := strings.TrimSpace(input.Username)
	name := strings.TrimSpace(input.Name)
	studentID := strings.TrimSpace(input.StudentID)
	college := strings.TrimSpace(input.College)
	year := strings.TrimSpace(input.Year)
	if !validAcademicIdentifier(username, 64) || name == "" || len([]rune(name)) > 120 || !validAcademicIdentifier(studentID, 64) {
		return nil, apiErr(http.StatusBadRequest, "invalid_input", "username, name, and studentId are required")
	}
	if len([]rune(college)) > 255 || len([]rune(year)) > 32 {
		return nil, apiErr(http.StatusBadRequest, "invalid_input", "student profile fields are too long")
	}
	if !validateStudentPassword(input.Password) {
		return nil, apiErr(http.StatusBadRequest, "invalid_input", "student password must contain 12 to 72 bytes")
	}
	email := strings.TrimSpace(input.Email)
	if email != "" && !validateContactEmail(email) {
		return nil, apiErr(http.StatusBadRequest, "invalid_input", "a valid email address is required")
	}
	if email == "" {
		email = studentMailbox(studentID, s.studentEmailDomain)
	}
	proposedIdentifiers := s.studentLoginIdentifiers(username, email, studentID)
	for _, candidate := range s.users {
		if candidate == nil {
			continue
		}
		candidateIdentifiers := s.studentLoginIdentifiers(candidate.Username, candidate.Email, candidate.StudentID)
		if strings.EqualFold(candidate.StudentID, studentID) || identifiersOverlap(proposedIdentifiers, candidateIdentifiers) {
			return nil, apiErr(http.StatusConflict, "student_exists", "username, studentId, or email already exists")
		}
	}
	passwordHash, hashErr := hashPasswordChecked(input.Password)
	if hashErr != nil {
		return nil, apiErr(http.StatusBadRequest, "invalid_input", "student password must contain 12 to 72 bytes")
	}
	item := &User{ID: "student-" + randomID(16), Username: username, Name: name, Email: email, Role: "student", PasswordHash: passwordHash, StudentID: studentID, College: college, Year: year}
	if input.Active != nil {
		item.Disabled = !*input.Active
	}
	if err := s.persistUserLockedErr(item); err != nil {
		return nil, apiErr(http.StatusServiceUnavailable, "student_persistence_failed", "student account could not be saved")
	}
	s.users[item.ID] = item
	view := s.adminStudentView(item)
	return &view, nil
}

func (s *Store) updateStudent(id string, input StudentInput) (*AdminStudent, *apiError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var item *User
	for _, candidate := range s.users {
		if candidate != nil && strings.EqualFold(candidate.ID, strings.TrimSpace(id)) {
			item = candidate
			break
		}
	}
	if item == nil || item.Role != "student" {
		return nil, apiErr(http.StatusNotFound, "student_not_found", "student not found")
	}
	previous := *item
	username, name, studentID := strings.TrimSpace(input.Username), strings.TrimSpace(input.Name), strings.TrimSpace(input.StudentID)
	if username == "" {
		username = item.Username
	}
	if name == "" {
		name = item.Name
	}
	if studentID == "" {
		studentID = item.StudentID
	}
	if !strings.EqualFold(studentID, item.StudentID) && s.studentHasApprovedAdmissionLocked(item.StudentID) {
		return nil, apiErr(http.StatusConflict, "student_identity_immutable", "the student id of an admitted account cannot be changed")
	}
	if !validAcademicIdentifier(username, 64) || name == "" || len([]rune(name)) > 120 || !validAcademicIdentifier(studentID, 64) {
		return nil, apiErr(http.StatusBadRequest, "invalid_input", "username, name, and studentId are required")
	}
	college, year := strings.TrimSpace(input.College), strings.TrimSpace(input.Year)
	if college == "" {
		college = item.College
	}
	if year == "" {
		year = item.Year
	}
	if len([]rune(college)) > 255 || len([]rune(year)) > 32 {
		return nil, apiErr(http.StatusBadRequest, "invalid_input", "student profile fields are too long")
	}
	email := strings.TrimSpace(input.Email)
	if email == "" {
		email = item.Email
	}
	if email == "" {
		email = studentMailbox(studentID, s.studentEmailDomain)
	}
	if !validateContactEmail(email) {
		return nil, apiErr(http.StatusBadRequest, "invalid_input", "a valid email address is required")
	}
	if input.Password != "" && !validateStudentPassword(input.Password) {
		return nil, apiErr(http.StatusBadRequest, "invalid_input", "student password must contain 12 to 72 bytes")
	}
	var passwordHash string
	if input.Password != "" {
		var hashErr error
		passwordHash, hashErr = hashPasswordChecked(input.Password)
		if hashErr != nil {
			return nil, apiErr(http.StatusBadRequest, "invalid_input", "student password must contain 12 to 72 bytes")
		}
	}
	oldGenerated := studentMailbox(item.StudentID, s.studentEmailDomain)
	if item.Email == oldGenerated && studentID != item.StudentID && (strings.TrimSpace(input.Email) == "" || strings.EqualFold(strings.TrimSpace(input.Email), oldGenerated)) {
		email = studentMailbox(studentID, s.studentEmailDomain)
	}
	proposedIdentifiers := s.studentLoginIdentifiers(username, email, studentID)
	for _, candidate := range s.users {
		if candidate == nil || candidate.ID == item.ID {
			continue
		}
		candidateIdentifiers := s.studentLoginIdentifiers(candidate.Username, candidate.Email, candidate.StudentID)
		if strings.EqualFold(candidate.StudentID, studentID) || identifiersOverlap(proposedIdentifiers, candidateIdentifiers) {
			return nil, apiErr(http.StatusConflict, "student_exists", "username, studentId, or email already exists")
		}
	}
	item.Username, item.Name, item.Email, item.StudentID, item.College, item.Year = username, name, email, studentID, college, year
	if input.Active != nil {
		item.Disabled = !*input.Active
	}
	if input.Password != "" {
		item.PasswordHash = passwordHash
	}
	if err := s.persistUserLockedErr(item); err != nil {
		*item = previous
		return nil, apiErr(http.StatusServiceUnavailable, "student_persistence_failed", "student account could not be saved")
	}
	view := s.adminStudentView(item)
	return &view, nil
}

// changeStudentPassword rotates a student credential after verifying the
// current secret. Administrators keep their bootstrap credential in the
// private config/env source of truth, so the admin UI continues to rotate that
// account through the existing protected student editor/config workflow.
func (s *Store) changeStudentPassword(userID, currentPassword, newPassword string) (*User, *apiError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user := s.users[strings.TrimSpace(userID)]
	if user == nil || user.Role != "student" || user.Disabled {
		return nil, apiErr(http.StatusForbidden, "student_required", "only students may change their password here")
	}
	if !verifyPassword(currentPassword, user.PasswordHash) {
		return nil, apiErr(http.StatusUnauthorized, "current_password_invalid", "current password is incorrect")
	}
	if !validateStudentPassword(newPassword) {
		return nil, apiErr(http.StatusBadRequest, "invalid_input", "new password must contain 12 to 72 bytes")
	}
	if newPassword == currentPassword {
		return nil, apiErr(http.StatusBadRequest, "password_unchanged", "new password must be different from the current password")
	}
	previousHash := user.PasswordHash
	passwordHash, hashErr := hashPasswordChecked(newPassword)
	if hashErr != nil {
		return nil, apiErr(http.StatusBadRequest, "invalid_input", "new password must contain 12 to 72 bytes")
	}
	user.PasswordHash = passwordHash
	if err := s.persistUserLockedErr(user); err != nil {
		user.PasswordHash = previousHash
		return nil, apiErr(http.StatusServiceUnavailable, "password_persistence_failed", "password could not be saved")
	}
	copy := *user
	return &copy, nil
}

func normalizeStudentEmailDomain(value string) string {
	domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
	if domain == "" || len(domain) > 253 || strings.ContainsAny(domain, "@/\\ \t\r\n") {
		return "cgu.edu.kg"
	}
	for _, r := range domain {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-' {
			continue
		}
		return "cgu.edu.kg"
	}
	return domain
}

func studentMailbox(studentID, domain string) string {
	local := strings.ToLower(strings.TrimSpace(studentID))
	if local == "" || len(local) > 64 || strings.HasPrefix(local, ".") || strings.HasSuffix(local, ".") || strings.Contains(local, "..") {
		return ""
	}
	for _, r := range local {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return ""
	}
	return local + "@" + normalizeStudentEmailDomain(domain)
}

func (s *Store) coursesFor(viewer *User, adminView bool) []Course {
	s.mu.RLock()
	defer s.mu.RUnlock()
	counts := make(map[string]int)
	selected := make(map[string]bool)
	for _, item := range s.enrollments {
		if item.Status == "enrolled" {
			counts[item.CourseID]++
			if viewer != nil && item.StudentID == viewer.ID {
				selected[item.CourseID] = true
			}
		}
	}
	result := make([]Course, 0, len(s.courses))
	for _, item := range s.courses {
		copy := *item
		copy.EnrolledCount = counts[item.ID]
		copy.Enrolled = !adminView && selected[item.ID]
		result = append(result, copy)
	}
	return result
}

func (s *Store) catalogCSV(locale string) ([]byte, error) {
	locale = strings.ToLower(strings.TrimSpace(locale))
	courses := s.coursesFor(nil, false)
	var buffer strings.Builder
	// Keep the public download UTF-8 friendly for spreadsheet applications.
	buffer.WriteString("\ufeff")
	writer := csv.NewWriter(&buffer)
	if err := writer.Write([]string{"Code", "Name", "Name (Chinese)", "Name (English)", "School", "School (English)", "Teacher", "Teacher (English)", "Credits", "Capacity", "Term", "Term (English)", "Type", "Description", "Description (English)"}); err != nil {
		return nil, err
	}
	for _, course := range courses {
		name := course.NameZh
		if locale == "en" && strings.TrimSpace(course.NameEn) != "" {
			name = course.NameEn
		}
		row := []string{
			course.Code, name, course.NameZh, course.NameEn, course.Department, course.DepartmentEn, course.Teacher, course.TeacherEn,
			strconv.FormatFloat(course.Credits, 'f', -1, 64), strconv.Itoa(course.Capacity), course.Term, course.TermEn,
			course.Type, course.Description, course.DescriptionEn,
		}
		for index := range row {
			row[index] = safeCSVCell(row[index])
		}
		if err := writer.Write(row); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return []byte(buffer.String()), nil
}

// transcriptCSV exports only published grades for one authenticated student.
// The endpoint is intentionally separate from the registrar-wide grade API so
// a downloaded transcript can never include draft or in-progress records.
func (s *Store) transcriptCSV(studentID, locale string) ([]byte, error) {
	studentID = strings.TrimSpace(studentID)
	if studentID == "" {
		return nil, errors.New("student id is required")
	}
	s.mu.RLock()
	student := s.users[studentID]
	s.mu.RUnlock()
	if student == nil || student.Role != "student" {
		return nil, errors.New("student not found")
	}
	grades := s.gradesFor(studentID, false)
	sort.SliceStable(grades, func(i, j int) bool {
		if grades[i].Term != grades[j].Term {
			return grades[i].Term < grades[j].Term
		}
		return grades[i].CourseCode < grades[j].CourseCode
	})
	var buffer strings.Builder
	buffer.WriteString("\ufeff")
	writer := csv.NewWriter(&buffer)
	en := strings.HasPrefix(strings.ToLower(strings.TrimSpace(locale)), "en")
	if en {
		if err := writer.Write([]string{"Student ID", "Student", "Course code", "Course", "Term", "Credits", "Score", "Grade point", "Status"}); err != nil {
			return nil, err
		}
	} else if err := writer.Write([]string{"学号", "学生", "课程代码", "课程", "学期", "学分", "成绩", "绩点", "状态"}); err != nil {
		return nil, err
	}
	statusLabel := func(status string) string {
		if en {
			switch strings.ToLower(strings.TrimSpace(status)) {
			case "published":
				return "Published"
			case "graded":
				return "Graded"
			case "inprogress":
				return "In progress"
			case "withdrawn":
				return "Withdrawn"
			}
			return status
		}
		switch strings.ToLower(strings.TrimSpace(status)) {
		case "published":
			return "已发布"
		case "graded":
			return "已评分"
		case "inprogress":
			return "进行中"
		case "withdrawn":
			return "已撤回"
		}
		return status
	}
	for _, grade := range grades {
		courseName := grade.CourseNameZh
		if en && strings.TrimSpace(grade.CourseNameEn) != "" {
			courseName = grade.CourseNameEn
		}
		row := []string{
			student.StudentID,
			student.Name,
			grade.CourseCode,
			courseName,
			grade.Term,
			strconv.Itoa(grade.Credits),
			fmt.Sprint(grade.Score),
			fmt.Sprint(grade.Point),
			statusLabel(grade.Status),
		}
		for index := range row {
			row[index] = safeCSVCell(row[index])
		}
		if err := writer.Write(row); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return []byte(buffer.String()), nil
}

// safeCSVCell prevents spreadsheet applications from interpreting exported
// values as formulas when a registrar-entered field starts with a formula
// operator. The apostrophe is retained as an explicit text marker by common
// spreadsheet programs and does not alter ordinary cells.
func safeCSVCell(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if trimmed == "" {
		return value
	}
	switch trimmed[0] {
	case '=', '+', '-', '@':
		return "'" + value
	default:
		return value
	}
}

func (s *Store) enrollmentsFor(studentID string) []Enrollment {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Enrollment, 0)
	for _, item := range s.enrollments {
		if item.StudentID == studentID {
			result = append(result, *item)
		}
	}
	return result
}

// gradesFor returns one student's grades (or all grades when studentID is
// empty). Unpublished records are deliberately omitted unless the caller is
// an administrator. This keeps registrar workflow states out of the student
// transcript while preserving them for administrative review.
func (s *Store) gradesFor(studentID string, includeUnpublished bool) []Grade {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Grade, 0)
	for _, item := range s.grades {
		if item == nil {
			continue
		}
		if studentID != "" && item.StudentID != studentID {
			continue
		}
		if !includeUnpublished && !strings.EqualFold(strings.TrimSpace(item.Status), "published") {
			continue
		}
		result = append(result, *item)
	}
	return result
}

func (s *Store) scheduleFor(studentID string, all bool) []ScheduleEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]ScheduleEntry, 0)
	for _, item := range s.schedule {
		if all || item.StudentID == studentID {
			result = append(result, *item)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Day != result[j].Day {
			return result[i].Day < result[j].Day
		}
		return result[i].Start < result[j].Start
	})
	return result
}

func (s *Store) announcementsFor(viewer *User, adminView bool) []Announcement {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Announcement, 0)
	for _, item := range s.announcements {
		visible := adminView || (item.Published && item.Audience == "all")
		if viewer != nil && viewer.Role == "student" && item.Published && item.Audience == "student" {
			visible = true
		}
		if viewer != nil && viewer.Role == "admin" && !adminView {
			visible = item.Published
		}
		if visible {
			result = append(result, *item)
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].PublishedAt > result[j].PublishedAt })
	return result
}

func (s *Store) stats() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	students, pending, sections, pendingAdmissions := 0, 0, 0, 0
	for _, user := range s.users {
		if user != nil && user.Role == "student" && !user.Disabled {
			students++
		}
	}
	for _, item := range s.announcements {
		if !item.Published {
			pending++
		}
	}
	for _, item := range s.admissions {
		if item != nil && (strings.EqualFold(item.Status, "pending") || strings.EqualFold(item.Status, "reviewing")) {
			pendingAdmissions++
		}
	}
	for _, item := range s.courses {
		if item.Capacity > 0 {
			sections++
		}
	}
	return map[string]int{
		"courses":           len(s.courses),
		"students":          students,
		"sections":          sections,
		"admissions":        len(s.admissions),
		"pendingAdmissions": pendingAdmissions,
		"pending":           pending + pendingAdmissions,
	}
}

func (s *Store) admissionsList() []AdmissionApplication {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]AdmissionApplication, 0, len(s.admissions))
	for _, item := range s.admissions {
		if item == nil {
			continue
		}
		copy := *item
		// Delivery state is owned by the durable mailbox record. Join it into
		// the administrator projection so a page reload does not lose the
		// onboarding notice status shown immediately after approval.
		if mailbox := s.admissionApprovalMailboxLocked(copy.ID); mailbox != nil {
			copy.DeliveryStatus = mailbox.DeliveryStatus
			copy.DeliveryError = mailbox.DeliveryError
		}
		result = append(result, copy)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].CreatedAt > result[j].CreatedAt
	})
	return result
}

func (s *Store) createAdmission(input AdmissionApplicationInput) (*AdmissionApplication, *apiError) {
	// Public submissions cannot choose a workflow state or inject internal notes.
	input.Status = ""
	input.Notes = ""
	item, err := normalizeAdmission(input, nil)
	if err != nil {
		return nil, err
	}
	notification := &AdminNotification{
		ID:          "notification-" + randomID(16),
		RecipientID: "admin",
		Type:        "ADMISSIONS",
		TitleZh:     "收到新的招生申请",
		TitleEn:     "New admissions application",
		BodyZh:      fmt.Sprintf("申请人 %s（%s）提交了招生申请，请在招生申请页面查看。", item.Name, item.School),
		BodyEn:      fmt.Sprintf("%s (%s) submitted an admissions application. Review it in Admissions.", item.Name, item.School),
		ReferenceID: item.ID,
		CreatedAt:   item.CreatedAt,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.admissions = append(s.admissions, item)
	s.notifications = append(s.notifications, notification)
	if err := s.persistAdmissionWithNotificationLocked(item, notification); err != nil {
		s.admissions = s.admissions[:len(s.admissions)-1]
		s.notifications = s.notifications[:len(s.notifications)-1]
		return nil, apiErr(http.StatusServiceUnavailable, "admission_persistence_failed", "application could not be saved")
	}
	copy := *item
	return &copy, nil
}

func (s *Store) adminNotificationsFor(recipientID string) ([]AdminNotification, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]AdminNotification, 0, len(s.notifications))
	unread := 0
	for _, item := range s.notifications {
		if item == nil || (recipientID != "" && item.RecipientID != recipientID) {
			continue
		}
		result = append(result, *item)
		if item.ReadAt == "" {
			unread++
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].CreatedAt == result[j].CreatedAt {
			return result[i].ID > result[j].ID
		}
		return result[i].CreatedAt > result[j].CreatedAt
	})
	return result, unread
}

func (s *Store) markAdminNotificationRead(recipientID, id string, read bool) (*AdminNotification, *apiError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.notifications {
		if item == nil || !strings.EqualFold(item.ID, strings.TrimSpace(id)) {
			continue
		}
		if item.RecipientID != recipientID {
			return nil, apiErr(http.StatusForbidden, "forbidden", "notification does not belong to this administrator")
		}
		previous := item.ReadAt
		if read {
			item.ReadAt = time.Now().UTC().Format(time.RFC3339)
		} else {
			item.ReadAt = ""
		}
		if err := s.persistAdminNotificationReadLocked(item); err != nil {
			item.ReadAt = previous
			return nil, apiErr(http.StatusServiceUnavailable, "notification_persistence_failed", "notification could not be updated")
		}
		copy := *item
		return &copy, nil
	}
	return nil, apiErr(http.StatusNotFound, "notification_not_found", "notification not found")
}

func (s *Store) updateAdmission(id string, input AdmissionApplicationInput) (*AdmissionApplication, *apiError) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, apiErr(http.StatusNotFound, "admission_not_found", "application not found")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.admissions {
		if item == nil || !strings.EqualFold(item.ID, id) {
			continue
		}
		previous := *item
		normalized, err := normalizeAdmission(input, item)
		if err != nil {
			return nil, err
		}
		// A status is a workflow state, not editable content. The explicit
		// approval action is the sole transition that creates the student
		// account, mailbox, and notification as one durable unit. PATCH remains
		// available for administrative notes on the current state only.
		if !strings.EqualFold(normalized.Status, item.Status) {
			return nil, apiErr(http.StatusConflict, "approval_required", "use the approve action to change an application decision")
		}
		if strings.EqualFold(item.Status, "accepted") && strings.TrimSpace(item.StudentID) == "" {
			return nil, apiErr(http.StatusConflict, "approval_incomplete", "approved applications must be completed through the approve action")
		}
		if strings.EqualFold(item.Status, "accepted") && (normalized.Name != item.Name || normalized.Email != item.Email || normalized.School != item.School) {
			return nil, apiErr(http.StatusConflict, "admission_already_approved", "an approved applicant profile cannot be changed here")
		}
		*item = *normalized
		if err := s.persistAdmissionLocked(item); err != nil {
			*item = previous
			return nil, apiErr(http.StatusServiceUnavailable, "admission_persistence_failed", "application could not be saved")
		}
		copy := *item
		return &copy, nil
	}
	return nil, apiErr(http.StatusNotFound, "admission_not_found", "application not found")
}

func admissionStudentIdentity(applicationID string) (studentID, username, userID string) {
	digest := sha256.Sum256([]byte("cgu/admission/student/" + strings.TrimSpace(applicationID)))
	suffix := strings.ToLower(fmt.Sprintf("%x", digest[:10]))
	studentID = "CGU-" + strings.ToUpper(suffix)
	username = "student-" + suffix
	userID = "student-" + suffix
	return studentID, username, userID
}

func newAdmissionInitialPassword() string {
	// randomID uses crypto/rand and is long enough for the student password
	// policy. The value is held only in the approval response while its bcrypt
	// hash is persisted with the user record.
	return randomID(18) + "!"
}

func (s *Store) findAdmissionLocked(id string) (*AdmissionApplication, int) {
	for index, item := range s.admissions {
		if item != nil && strings.EqualFold(item.ID, strings.TrimSpace(id)) {
			return item, index
		}
	}
	return nil, -1
}

func admissionApprovalRequestKey(applicationID string) string {
	return "admission-approval:" + strings.TrimSpace(applicationID)
}

func (s *Store) admissionApprovalMailboxLocked(applicationID string) *MailboxMessage {
	requestKey := admissionApprovalRequestKey(applicationID)
	for _, item := range s.mailbox {
		if item == nil || !strings.EqualFold(item.RequestKey, requestKey) {
			continue
		}
		copy := *item
		return &copy
	}
	return nil
}

func (s *Store) admissionApprovalViewLocked(application *AdmissionApplication, student *User, password string, alreadyApproved bool) *AdmissionApproval {
	result := &AdmissionApproval{Student: s.adminStudentView(student), InitialPassword: password, AlreadyApproved: alreadyApproved}
	if application != nil {
		result.Application = *application
	}
	if mailbox := s.admissionApprovalMailboxLocked(result.Application.ID); mailbox != nil {
		result.MailboxID = mailbox.ID
		result.DeliveryStatus = mailbox.DeliveryStatus
		result.DeliveryError = strings.TrimSpace(mailbox.DeliveryError)
		if len([]rune(result.DeliveryError)) > 400 {
			result.DeliveryError = string([]rune(result.DeliveryError)[:400])
		}
		result.Application.DeliveryStatus = result.DeliveryStatus
		result.Application.DeliveryError = result.DeliveryError
	}
	return result
}

func (s *Store) admissionApprovalReplayLocked(item *AdmissionApplication) (*AdmissionApproval, *apiError) {
	if item == nil || !strings.EqualFold(item.Status, "accepted") || strings.TrimSpace(item.StudentID) == "" {
		return nil, apiErr(http.StatusConflict, "approval_incomplete", "approved application has no linked student account")
	}
	student := s.resolveStudentLocked(item.StudentID)
	if student == nil || student.Role != "student" {
		return nil, apiErr(http.StatusConflict, "approval_incomplete", "approved application has no linked student account")
	}
	return s.admissionApprovalViewLocked(item, student, "", true), nil
}

func (s *Store) buildAdmissionApprovalLocked(item *AdmissionApplication, approvedBy string) (*AdmissionApplication, *User, *MailboxMessage, string, *apiError) {
	if item == nil {
		return nil, nil, nil, "", apiErr(http.StatusNotFound, "admission_not_found", "application not found")
	}
	status := strings.ToLower(strings.TrimSpace(item.Status))
	if status == "rejected" || status == "withdrawn" {
		return nil, nil, nil, "", apiErr(http.StatusConflict, "admission_not_approvable", "this application is not eligible for approval")
	}
	studentID, username, userID := admissionStudentIdentity(item.ID)
	for _, candidate := range s.users {
		if candidate == nil {
			continue
		}
		candidateIdentifiers := s.studentLoginIdentifiers(candidate.Username, candidate.Email, candidate.StudentID)
		proposedIdentifiers := s.studentLoginIdentifiers(username, item.Email, studentID)
		if candidate.ID == userID && candidate.Role == "student" && strings.EqualFold(candidate.StudentID, studentID) {
			return nil, nil, nil, "", apiErr(http.StatusConflict, "approval_incomplete", "student account already exists without a completed approval")
		}
		if strings.EqualFold(candidate.StudentID, studentID) || identifiersOverlap(proposedIdentifiers, candidateIdentifiers) {
			return nil, nil, nil, "", apiErr(http.StatusConflict, "student_exists", "generated student identifiers already belong to another account")
		}
	}
	studentEmail := studentMailbox(studentID, s.studentEmailDomain)
	if studentEmail == "" {
		return nil, nil, nil, "", apiErr(http.StatusInternalServerError, "student_email_failed", "student mailbox could not be generated")
	}
	// Approval queues an external SMTP notification. Revalidate the persisted
	// applicant address at this trust boundary in case an operator imported or
	// edited a legacy row outside the normal admission validator.
	if !validateContactEmail(item.Email) {
		return nil, nil, nil, "", apiErr(http.StatusConflict, "external_recipient_invalid", "applicant email is not eligible for notification")
	}
	password := newAdmissionInitialPassword()
	passwordHash, hashErr := hashPasswordChecked(password)
	if hashErr != nil {
		return nil, nil, nil, "", apiErr(http.StatusInternalServerError, "password_hash_failed", "student password could not be secured")
	}
	approvedBy = strings.TrimSpace(approvedBy)
	if approvedBy == "" {
		approvedBy = "admin"
	}
	approvedAt := time.Now().UTC().Format(time.RFC3339)
	student := &User{
		ID: userID, Username: username, Name: item.Name, Email: item.Email, Role: "student",
		PasswordHash: passwordHash, StudentID: studentID, College: item.School,
		Year: time.Now().UTC().Format("2006"),
	}
	mailbox := &MailboxMessage{
		ID:                 "mail-admission-" + strings.TrimPrefix(userID, "student-"),
		RecipientID:        student.ID,
		RecipientName:      student.Name,
		RecipientStudentID: student.StudentID,
		RecipientEmail:     studentEmail,
		SenderID:           approvedBy,
		SenderName:         "CGU 教务处",
		Subject:            "CGU 学生账户已建立",
		Body:               fmt.Sprintf("你的 CGU 学生档案已建立。校内邮箱：%s。请使用教务处安全转交给你的初始密码登录；出于安全原因，初始密码不会写入校内邮箱。", studentEmail),
		CreatedAt:          approvedAt,
		// Queue the applicant notice as an external delivery. The transaction
		// still stores the internal copy first; the server performs the SMTP
		// attempt only after the student account has committed.
		DeliveryMode:      mailboxDeliveryModeSMTP,
		ExternalRecipient: item.Email,
		DeliveryStatus:    mailboxDeliveryPending,
		RequestKey:        admissionApprovalRequestKey(item.ID),
	}
	updated := *item
	updated.Status = "accepted"
	updated.StudentID = student.StudentID
	updated.ApprovedAt = approvedAt
	updated.ApprovedBy = approvedBy
	updated.InitialPasswordIssuedAt = approvedAt
	updated.UpdatedAt = approvedAt
	return &updated, student, mailbox, password, nil
}

// approveAdmission is the only path that transitions an application to
// accepted. It creates the student account and internal welcome message as a
// single durable unit when MySQL is configured; the in-memory path applies the
// same idempotent state transition under the store lock.
func (s *Store) approveAdmission(id, approvedBy string) (*AdmissionApproval, *apiError) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, apiErr(http.StatusNotFound, "admission_not_found", "application not found")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return s.approveAdmissionDatabaseLocked(id, approvedBy)
	}
	item, _ := s.findAdmissionLocked(id)
	if item == nil {
		return nil, apiErr(http.StatusNotFound, "admission_not_found", "application not found")
	}
	if strings.EqualFold(item.Status, "accepted") && strings.TrimSpace(item.StudentID) != "" {
		return s.admissionApprovalReplayLocked(item)
	}
	updated, student, mailbox, password, apiError := s.buildAdmissionApprovalLocked(item, approvedBy)
	if apiError != nil {
		return nil, apiError
	}
	// No persistence can fail in this branch. Apply all related records before
	// publishing the response so a retry sees a complete approval.
	s.users[student.ID] = student
	s.mailbox = append(s.mailbox, mailbox)
	*item = *updated
	return s.admissionApprovalViewLocked(item, student, password, false), nil
}

type admissionRowScanner interface {
	Scan(dest ...any) error
}

func scanAdmissionApprovalRow(scanner admissionRowScanner) (*AdmissionApplication, error) {
	item := &AdmissionApplication{}
	var studentID, approvedAt, approvedBy, passwordIssuedAt sql.NullString
	if err := scanner.Scan(&item.ID, &item.Name, &item.Email, &item.School, &item.Status, &item.Notes, &item.CreatedAt, &item.UpdatedAt, &studentID, &approvedAt, &approvedBy, &passwordIssuedAt); err != nil {
		return nil, err
	}
	if studentID.Valid {
		item.StudentID = studentID.String
	}
	if approvedAt.Valid {
		item.ApprovedAt = approvedAt.String
	}
	if approvedBy.Valid {
		item.ApprovedBy = approvedBy.String
	}
	if passwordIssuedAt.Valid {
		item.InitialPasswordIssuedAt = passwordIssuedAt.String
	}
	return item, nil
}

func loadAdmissionStudentTx(ctx context.Context, tx *sql.Tx, studentID string) (*User, error) {
	student := &User{}
	err := tx.QueryRowContext(ctx, `SELECT id, username, name_text, email, role_name, password_hash, student_id, college, year_text, disabled_flag FROM cgu_users WHERE student_id = ? AND role_name = 'student' LIMIT 1`, studentID).Scan(&student.ID, &student.Username, &student.Name, &student.Email, &student.Role, &student.PasswordHash, &student.StudentID, &student.College, &student.Year, &student.Disabled)
	if err != nil {
		return nil, err
	}
	return student, nil
}

func (s *Store) upsertAdmissionMemoryLocked(item *AdmissionApplication) *AdmissionApplication {
	if item == nil {
		return nil
	}
	if existing, _ := s.findAdmissionLocked(item.ID); existing != nil {
		*existing = *item
		return existing
	}
	copy := *item
	s.admissions = append(s.admissions, &copy)
	return &copy
}

func (s *Store) approveAdmissionDatabaseLocked(id, approvedBy string) (*AdmissionApproval, *apiError) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, apiErr(http.StatusServiceUnavailable, "admission_approval_persistence_failed", "application approval could not be started")
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	item, err := scanAdmissionApprovalRow(tx.QueryRowContext(ctx, admissionForUpdateSQL, id))
	if err == sql.ErrNoRows {
		return nil, apiErr(http.StatusNotFound, "admission_not_found", "application not found")
	}
	if err != nil {
		return nil, apiErr(http.StatusServiceUnavailable, "admission_approval_persistence_failed", "application could not be read")
	}
	if strings.EqualFold(item.Status, "accepted") && strings.TrimSpace(item.StudentID) != "" {
		// Read the linked account from the same durable store even when this
		// process has a cached copy. This prevents a stale in-memory profile from
		// being returned after an administrator edits the account elsewhere.
		student, err := loadAdmissionStudentTx(ctx, tx, item.StudentID)
		if err == sql.ErrNoRows {
			return nil, apiErr(http.StatusConflict, "approval_incomplete", "approved application has no linked student account")
		}
		if err != nil {
			return nil, apiErr(http.StatusServiceUnavailable, "admission_approval_persistence_failed", "student account could not be read")
		}
		if err := tx.Commit(); err != nil {
			return nil, apiErr(http.StatusServiceUnavailable, "admission_approval_persistence_failed", "application approval could not be finalized")
		}
		committed = true
		s.users[student.ID] = student
		stored := s.upsertAdmissionMemoryLocked(item)
		return s.admissionApprovalViewLocked(stored, student, "", true), nil
	}
	if strings.EqualFold(item.Status, "rejected") || strings.EqualFold(item.Status, "withdrawn") {
		return nil, apiErr(http.StatusConflict, "admission_not_approvable", "this application is not eligible for approval")
	}
	updated, student, mailbox, password, apiError := s.buildAdmissionApprovalLocked(item, approvedBy)
	if apiError != nil {
		return nil, apiError
	}
	if err := persistAdmissionApprovalTx(ctx, tx, updated, student, mailbox); err != nil {
		if isDuplicateKeyError(err) {
			return nil, apiErr(http.StatusConflict, "student_exists", "generated student identifiers already belong to another account")
		}
		return nil, apiErr(http.StatusServiceUnavailable, "admission_approval_persistence_failed", "student account could not be saved")
	}
	if err := tx.Commit(); err != nil {
		return nil, apiErr(http.StatusServiceUnavailable, "admission_approval_persistence_failed", "application approval could not be finalized")
	}
	committed = true
	s.users[student.ID] = student
	foundMailbox := false
	for _, existing := range s.mailbox {
		if existing != nil && strings.EqualFold(existing.RequestKey, mailbox.RequestKey) {
			foundMailbox = true
			break
		}
	}
	if !foundMailbox {
		s.mailbox = append(s.mailbox, mailbox)
	}
	stored := s.upsertAdmissionMemoryLocked(updated)
	return s.admissionApprovalViewLocked(stored, student, password, false), nil
}

func (s *Store) deleteAdmission(id string) (*AdmissionApplication, *apiError) {
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, item := range s.admissions {
		if item == nil || !strings.EqualFold(item.ID, id) {
			continue
		}
		if strings.EqualFold(item.Status, "accepted") && strings.TrimSpace(item.StudentID) != "" {
			return nil, apiErr(http.StatusConflict, "admission_already_approved", "approved applications cannot be deleted after student provisioning")
		}
		copy := *item
		s.admissions = append(s.admissions[:i], s.admissions[i+1:]...)
		if err := s.deleteAdmissionPersistedLocked(id); err != nil {
			s.admissions = append(s.admissions[:i], append([]*AdmissionApplication{item}, s.admissions[i:]...)...)
			return nil, apiErr(http.StatusServiceUnavailable, "admission_persistence_failed", "application could not be deleted")
		}
		return &copy, nil
	}
	return nil, apiErr(http.StatusNotFound, "admission_not_found", "application not found")
}

func defaultSiteContent() map[string]*SiteContent {
	items := []SiteContent{
		{Key: "brand.name", Zh: "原神大学", En: "China Genshin University"},
		{Key: "brand.short", Zh: "CGU", En: "CGU"},
		{Key: "brand.full", Zh: "China Genshin University", En: "China Genshin University"},
		{Key: "nav.home", Zh: "返回首页", En: "University home"},
		{Key: "nav.portal", Zh: "学生教务", En: "Student portal"},
		{Key: "nav.admin", Zh: "后台管理", En: "Administration"},
		{Key: "login.title", Zh: "登录 CGU 门户", En: "Sign in to CGU"},
		{Key: "login.metaDescription", Zh: "CGU 原神大学校园访问入口", En: "China Genshin University campus access"},
		{Key: "login.subtitle", Zh: "进入课程、成绩与校园服务。", En: "Access courses, grades, and campus services."},
		{Key: "login.submit", Zh: "登录门户", En: "Sign in"},
		{Key: "login.footerCopyright", Zh: "© 2026 CGU", En: "© 2026 CGU"},
		{Key: "login.footerServices", Zh: "教务服务", En: "Academic services"},
		{Key: "home.heroTitleLead", Zh: "在提瓦特，", En: "In Teyvat,"},
		{Key: "home.heroTitleEm", Zh: "成为你想成为的人", En: "become who you are meant to be"},
		{Key: "home.heroLede", Zh: "一所为旅行者而设的大学。把元素力化为方法，把每一次相遇写进你的学术旅程。", En: "A university for travelers. Turn elemental power into method, and every encounter into part of your academic journey."},
		{Key: "home.statSchoolsValue", Zh: "07", En: "07"},
		{Key: "home.statCoursesValue", Zh: "42", En: "42"},
		{Key: "home.statJourneysValue", Zh: "∞", En: "∞"},
		{Key: "home.aboutSectionNumber", Zh: "01", En: "01"},
		{Key: "home.programWindNumber", Zh: "01", En: "01"},
		{Key: "home.programContractNumber", Zh: "02", En: "02"},
		{Key: "home.programDesignNumber", Zh: "03", En: "03"},
		{Key: "home.programWisdomNumber", Zh: "04", En: "04"},
		{Key: "home.programJusticeNumber", Zh: "05", En: "05"},
		{Key: "home.programFlameNumber", Zh: "06", En: "06"},
		{Key: "home.programPolarNumber", Zh: "07", En: "07"},
		{Key: "home.featureDate", Zh: "08.23", En: "08.23"},
		{Key: "home.featureYear", Zh: "2026", En: "2026"},
		{Key: "home.newsSnezhnayaDate", Zh: "08.23", En: "08.23"},
		{Key: "home.newsSnezhnayaYear", Zh: "2026", En: "2026"},
		{Key: "home.newsCampusDate", Zh: "08.05", En: "08.05"},
		{Key: "home.newsCampusYear", Zh: "2026", En: "2026"},
		{Key: "home.newsResearchDate", Zh: "07.24", En: "07.24"},
		{Key: "home.newsResearchYear", Zh: "2026", En: "2026"},
		{Key: "home.footerEmail", Zh: "hello@cgu-university.example", En: "hello@cgu-university.example"},
		{Key: "home.programsTitle", Zh: "学院与专业", En: "Schools and programs"},
		{Key: "home.lifeTitle", Zh: "校园动态", En: "Campus life"},
		{Key: "home.admissionsTitle", Zh: "准备好出发了吗？", En: "Ready to set out?"},
		{Key: "home.footerAddress", Zh: "璃月港 · 玉京台 7 号", En: "Liyue Harbor · Yujing Terrace 7"},
		{Key: "portal.title", Zh: "我的教务空间", En: "My academic space"},
		{Key: "portal.metaDescription", Zh: "CGU 原神大学学生教务门户", En: "China Genshin University student portal"},
		{Key: "catalog.title", Zh: "CGU 课程目录 | China Genshin University", En: "CGU Course Catalog | China Genshin University"},
		{Key: "catalog.metaDescription", Zh: "CGU 原神大学公开课程目录", En: "China Genshin University public course catalog"},
		{Key: "catalog.titleShort", Zh: "公开课程目录", En: "Public course catalog"},
		{Key: "catalog.intro", Zh: "查看 CGU 当前开放课程、学院、教师与学期信息；完整双语目录可下载。", En: "Review current CGU courses, schools, instructors, and terms. Download the complete bilingual catalog."},
		{Key: "catalog.handbookTitle", Zh: "从录取到毕业", En: "From admission to graduation"},
		{Key: "catalog.handbookBody", Zh: "录取后，教务处会建立学生档案与学校邮箱。登录学生门户即可完成选课、查看课表、阅读公告、查看已发布成绩并修改密码。课程、成绩和招生记录由教务处持续维护；如需帮助，请通过招生办公室联系 CGU。", En: "After admission, the registrar creates your student record and university mailbox. Use the student portal to enroll in courses, review your schedule, read announcements, view published grades, and change your password. CGU maintains course, grade, and admissions records through the registrar; contact the admissions office when you need help."},
		{Key: "portal.welcome", Zh: "欢迎回来，{name}", En: "Welcome back, {name}"},
		{Key: "portal.welcomeFallback", Zh: "欢迎回来", En: "Welcome back"},
		{Key: "portal.academicsKicker", Zh: "ACADEMICS", En: "ACADEMICS"},
		{Key: "portal.campusKicker", Zh: "CAMPUS LIFE", En: "CAMPUS LIFE"},
		{Key: "portal.accountKicker", Zh: "ACCOUNT", En: "ACCOUNT"},
		{Key: "portal.mailbox", Zh: "校内邮箱", En: "Student mail"},
		{Key: "portal.mailboxKicker", Zh: "STUDENT MAIL", En: "STUDENT MAIL"},
		{Key: "portal.mailboxAddress", Zh: "你的学校邮箱", En: "Your university mailbox"},
		{Key: "portal.unreadCount", Zh: "{count} 封未读邮件", En: "{count} unread messages"},
		{Key: "portal.allRead", Zh: "全部已读", En: "All messages read"},
		{Key: "portal.sender", Zh: "发件人", En: "From"},
		{Key: "portal.read", Zh: "已读", En: "Read"},
		{Key: "portal.markRead", Zh: "标记为已读", En: "Mark as read"},
		{Key: "portal.noMailboxMessages", Zh: "暂无校内邮件。", En: "No university messages yet."},
		{Key: "portal.mailboxMarkedRead", Zh: "邮件已标记为已读。", En: "Message marked as read."},
		{Key: "portal.noticeType", Zh: "NOTICE", En: "NOTICE"},
		{Key: "portal.creditsTarget", Zh: "本科阶段目标 120", En: "Undergraduate target 120"},
		{Key: "portal.gradedCourses", Zh: "{count} 门已出分", En: "{count} graded courses"},
		{Key: "portal.currentTerm", Zh: "本学期", En: "This term"},
		{Key: "portal.termFallback", Zh: "2026", En: "2026"},
		{Key: "admin.title", Zh: "教务管理台", En: "Academic administration"},
		{Key: "admin.metaDescription", Zh: "CGU 原神大学教务管理后台", En: "China Genshin University administration portal"},
		{Key: "admin.subtitle", Zh: "维护课程、公告与校园学术信息。", En: "Maintain courses, announcements, and academic information."},
		{Key: "admin.mailbox", Zh: "校内邮箱", En: "Student mail"},
		{Key: "admin.mailboxKicker", Zh: "STUDENT SERVICES", En: "STUDENT SERVICES"},
		{Key: "admin.mailboxHelp", Zh: "向学生学校邮箱发送教务通知，学生登录后可在收件箱查看。", En: "Send academic notices to a student mailbox for review in the student portal."},
		{Key: "admin.composeMessage", Zh: "撰写邮件", En: "Compose message"},
		{Key: "admin.recipient", Zh: "收件学生", En: "Recipient"},
		{Key: "admin.subject", Zh: "主题", En: "Subject"},
		{Key: "admin.messageBody", Zh: "正文", En: "Message body"},
		{Key: "admin.externalDelivery", Zh: "同时发送到联系邮箱", En: "Also send to contact email"},
		{Key: "admin.externalDeliveryHelp", Zh: "需要在服务端配置 SMTP；校内邮箱记录仍会保留。", En: "Requires SMTP configuration on the server; the internal mailbox copy is always retained."},
		{Key: "admin.sendMessage", Zh: "发送邮件", En: "Send message"},
		{Key: "admin.sendingMessage", Zh: "发送中…", En: "Sending…"},
		{Key: "admin.sentMessages", Zh: "发送记录", En: "Sent messages"},
		{Key: "admin.read", Zh: "已读", En: "Read"},
		{Key: "admin.unread", Zh: "未读", En: "Unread"},
		{Key: "admin.noMailboxMessages", Zh: "暂无发送记录。", En: "No sent messages yet."},
		{Key: "admin.messageSent", Zh: "邮件已发送。", En: "Message sent."},
		{Key: "admin.smtpSent", Zh: "校内邮件已保存，SMTP 中继已接受外发。", En: "Internal copy saved; the SMTP relay accepted the message."},
		{Key: "admin.smtpFailed", Zh: "校内邮件已保存，但 SMTP 外发失败。", En: "Internal copy saved, but SMTP delivery failed."},
		{Key: "admin.smtpNotConfigured", Zh: "校内邮件已保存，SMTP 尚未配置。", En: "Internal copy saved; SMTP is not configured."},
		{Key: "admin.smtpUnknown", Zh: "校内邮件已保存，但外发结果未知，请确认后再重试。", En: "Internal copy saved; external delivery outcome is unknown. Confirm before retrying."},
		{Key: "admin.messageAlreadyRecorded", Zh: "已返回之前保存的邮件记录。", En: "The previously recorded message was returned."},
		{Key: "admin.deliveryInternal", Zh: "仅校内邮箱", En: "Internal mailbox only"},
		{Key: "admin.deliveryPending", Zh: "外发处理中", En: "External delivery pending"},
		{Key: "admin.deliverySending", Zh: "正在外发", En: "External delivery in progress"},
		{Key: "admin.deliverySent", Zh: "SMTP 中继已接受", En: "SMTP relay accepted"},
		{Key: "admin.deliveryFailed", Zh: "SMTP 外发失败", En: "SMTP delivery failed"},
		{Key: "admin.deliveryNotConfigured", Zh: "SMTP 未配置", En: "SMTP not configured"},
		{Key: "admin.deliveryUnknown", Zh: "投递结果未知", En: "Delivery outcome unknown"},
		{Key: "admin.confirmUnknownDelivery", Zh: "我确认中继未接受，继续重试", En: "I confirm the relay did not accept it; retry"},
		{Key: "admin.deliveryTarget", Zh: "外发地址", En: "External address"},
		{Key: "admin.retryDelivery", Zh: "重试外发", En: "Retry delivery"},
		{Key: "admin.admissionApprove", Zh: "同意录取", En: "Approve admission"},
		{Key: "admin.admissionApproved", Zh: "已自动录取", En: "Automatically admitted"},
		{Key: "admin.admissionProvisioned", Zh: "学生档案与校内邮箱已建立。", En: "Student record and university mailbox created."},
		{Key: "admin.admissionCredentials", Zh: "初始登录凭据（仅显示一次）", En: "Initial sign-in details (shown once)"},
		{Key: "admin.admissionUsername", Zh: "登录账号", En: "Login account"},
		{Key: "admin.admissionPassword", Zh: "初始密码", En: "Initial password"},
		{Key: "admin.admissionEmailDelivery", Zh: "入学通知已发送到申请邮箱。", En: "Onboarding notice sent to the applicant email."},
		{Key: "admin.admissionEmailPending", Zh: "申请邮箱通知尚未配置 SMTP，请安全转交初始凭据。", En: "SMTP is not configured; transfer the initial details securely."},
		{Key: "admin.admissionCopyPassword", Zh: "复制初始密码", En: "Copy initial password"},
		{Key: "admin.admissionCopied", Zh: "初始密码已复制。", En: "Initial password copied."},
		{Key: "admin.admissionCopyFailed", Zh: "浏览器拒绝访问剪贴板，请使用密码旁的复制功能或安全地手动转交。", En: "The browser blocked clipboard access. Transfer the password securely by another method."},
		{Key: "admin.admissionAlreadyApproved", Zh: "该申请已处理，系统不会重复创建账号。", En: "This application is already processed; no duplicate account was created."},
		{Key: "admin.coursesUnit", Zh: "COURSES", En: "COURSES"},
		{Key: "admin.studentsUnit", Zh: "STUDENTS", En: "STUDENTS"},
		{Key: "admin.sectionsUnit", Zh: "OPEN SECTIONS", En: "OPEN SECTIONS"},
		{Key: "admin.pendingUnit", Zh: "PENDING", En: "PENDING"},
		{Key: "admin.courseQuickNumber", Zh: "01", En: "01"},
		{Key: "admin.announcementQuickNumber", Zh: "02", En: "02"},
		{Key: "admin.contentKicker", Zh: "CONTENT MANAGEMENT", En: "CONTENT MANAGEMENT"},
		{Key: "asset.heroImage", Zh: "https://images.unsplash.com/photo-1500534623283-312aade485b7?auto=format&fit=crop&w=2000&q=88", En: "https://images.unsplash.com/photo-1500534623283-312aade485b7?auto=format&fit=crop&w=2000&q=88"},
		{Key: "asset.aboutImage", Zh: "https://images.unsplash.com/photo-1511497584788-876760111969?auto=format&fit=crop&w=1000&q=85", En: "https://images.unsplash.com/photo-1511497584788-876760111969?auto=format&fit=crop&w=1000&q=85"},
		{Key: "asset.featureImage", Zh: "https://images.unsplash.com/photo-1500534314209-a25ddb2bd429?auto=format&fit=crop&w=1400&q=85", En: "https://images.unsplash.com/photo-1500534314209-a25ddb2bd429?auto=format&fit=crop&w=1400&q=85"},
		{Key: "asset.programWindImage", Zh: "https://images.unsplash.com/photo-1500534623283-312aade485b7?auto=format&fit=crop&w=900&q=80", En: "https://images.unsplash.com/photo-1500534623283-312aade485b7?auto=format&fit=crop&w=900&q=80"},
		{Key: "asset.programContractImage", Zh: "https://images.unsplash.com/photo-1548013146-72479768bada?auto=format&fit=crop&w=900&q=80", En: "https://images.unsplash.com/photo-1548013146-72479768bada?auto=format&fit=crop&w=900&q=80"},
		{Key: "asset.programDesignImage", Zh: "https://images.unsplash.com/photo-1518709268805-4e9042af9f23?auto=format&fit=crop&w=900&q=80", En: "https://images.unsplash.com/photo-1518709268805-4e9042af9f23?auto=format&fit=crop&w=900&q=80"},
		{Key: "asset.programWisdomImage", Zh: "https://images.unsplash.com/photo-1534447677768-be436bb09401?auto=format&fit=crop&w=900&q=80", En: "https://images.unsplash.com/photo-1534447677768-be436bb09401?auto=format&fit=crop&w=900&q=80"},
		{Key: "asset.programJusticeImage", Zh: "https://images.unsplash.com/photo-1494526585095-c41746248156?auto=format&fit=crop&w=900&q=80", En: "https://images.unsplash.com/photo-1494526585095-c41746248156?auto=format&fit=crop&w=900&q=80"},
		{Key: "asset.programFlameImage", Zh: "https://images.unsplash.com/photo-1500534314209-a25ddb2bd429?auto=format&fit=crop&w=900&q=80", En: "https://images.unsplash.com/photo-1500534314209-a25ddb2bd429?auto=format&fit=crop&w=900&q=80"},
		{Key: "asset.programPolarImage", Zh: "https://images.unsplash.com/photo-1519681393784-d120267933ba?auto=format&fit=crop&w=900&q=80", En: "https://images.unsplash.com/photo-1519681393784-d120267933ba?auto=format&fit=crop&w=900&q=80"},
		{Key: "link.officialNews", Zh: "https://genshin.hoyoverse.com/zh-tw/news", En: "https://genshin.hoyoverse.com/en/news"},
		{Key: "link.featureNews", Zh: "https://genshin.hoyoverse.com/zh-tw/news", En: "https://genshin.hoyoverse.com/en/news"},
		{Key: "link.newsSnezhnaya", Zh: "https://genshin.hoyoverse.com/zh-tw/news", En: "https://genshin.hoyoverse.com/en/news"},
		{Key: "link.newsCampus", Zh: "#contact", En: "#contact"},
		{Key: "link.newsResearch", Zh: "#programs", En: "#programs"},
		{Key: "link.calendar", Zh: "/calendar", En: "/calendar"},
		{Key: "link.footerEmail", Zh: "mailto:hello@cgu-university.example", En: "mailto:hello@cgu-university.example"},
	}
	result := make(map[string]*SiteContent, len(items))
	for _, item := range items {
		copy := item
		result[item.Key] = &copy
	}
	return result
}

func (s *Store) siteContentList() []SiteContent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]SiteContent, 0, len(s.siteContent))
	for _, item := range s.siteContent {
		if item == nil {
			continue
		}
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result
}

func (s *Store) updateSiteContent(input SiteContentInput) (*SiteContent, *apiError) {
	key := strings.TrimSpace(input.Key)
	if key == "" || len(key) > 160 || !validSiteContentKey(key) {
		return nil, apiErr(400, "invalid_input", "content key must contain letters, numbers, dots, dashes, or underscores")
	}
	zh, en := strings.TrimSpace(input.Zh), strings.TrimSpace(input.En)
	// An empty pair is an intentional reset. Keeping the key with empty values
	// makes the operation durable and observable in the admin editor, while the
	// frontend i18n layer falls back to its bundled copy for that key.
	if len([]rune(zh)) > 8000 || len([]rune(en)) > 8000 {
		return nil, apiErr(400, "invalid_input", "content value is too long")
	}
	item := &SiteContent{Key: key, Zh: zh, En: en, UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.siteContent == nil {
		s.siteContent = make(map[string]*SiteContent)
	}
	previous, existed := s.siteContent[key]
	s.siteContent[key] = item
	if err := s.persistSiteContentLocked(item); err != nil {
		if existed {
			s.siteContent[key] = previous
		} else {
			delete(s.siteContent, key)
		}
		return nil, apiErr(http.StatusServiceUnavailable, "content_persistence_failed", "site content could not be saved")
	}
	copy := *item
	return &copy, nil
}

func validSiteContentKey(key string) bool {
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func (s *Store) changeEnrollment(studentID, courseID, action string) (*Enrollment, *apiError) {
	action = strings.ToLower(strings.TrimSpace(action))
	if action == "" {
		action = "enroll"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if action != "enroll" && action != "drop" && action != "delete" {
		return nil, apiErr(400, "invalid_input", "action must be enroll or drop")
	}
	var course *Course
	for _, item := range s.courses {
		if strings.EqualFold(item.ID, courseID) {
			course = item
			break
		}
	}
	if course == nil {
		return nil, apiErr(404, "course_not_found", "course not found")
	}
	var current *Enrollment
	for _, item := range s.enrollments {
		if item.StudentID == studentID && item.CourseID == course.ID && item.Status == "enrolled" {
			current = item
			break
		}
	}
	if action == "enroll" {
		if current != nil {
			copy := *current
			if err := s.persistEnrollmentLocked(current); err != nil {
				return nil, apiErr(http.StatusServiceUnavailable, "enrollment_persistence_failed", "enrollment could not be saved")
			}
			return &copy, nil
		}
		count := 0
		for _, item := range s.enrollments {
			if item.CourseID == course.ID && item.Status == "enrolled" {
				count++
			}
		}
		if course.Capacity > 0 && count >= course.Capacity {
			return nil, apiErr(409, "course_full", "course is full")
		}
		enrollment := &Enrollment{ID: "enrollment-" + randomID(12), StudentID: studentID, CourseID: course.ID, Term: course.Term, Status: "enrolled"}
		s.enrollments = append(s.enrollments, enrollment)
		if err := s.persistEnrollmentLocked(enrollment); err != nil {
			s.enrollments = s.enrollments[:len(s.enrollments)-1]
			return nil, apiErr(http.StatusServiceUnavailable, "enrollment_persistence_failed", "enrollment could not be saved")
		}
		copy := *enrollment
		return &copy, nil
	}
	if current == nil {
		enrollment := &Enrollment{ID: "enrollment-" + randomID(12), StudentID: studentID, CourseID: course.ID, Term: course.Term, Status: "dropped"}
		s.enrollments = append(s.enrollments, enrollment)
		if err := s.persistEnrollmentLocked(enrollment); err != nil {
			s.enrollments = s.enrollments[:len(s.enrollments)-1]
			return nil, apiErr(http.StatusServiceUnavailable, "enrollment_persistence_failed", "enrollment could not be saved")
		}
		copy := *enrollment
		return &copy, nil
	}
	previousStatus := current.Status
	current.Status = "dropped"
	if err := s.persistEnrollmentLocked(current); err != nil {
		current.Status = previousStatus
		return nil, apiErr(http.StatusServiceUnavailable, "enrollment_persistence_failed", "enrollment could not be saved")
	}
	copy := *current
	return &copy, nil
}

func academicBoundedValue(value any, minimum, maximum float64, field string) (string, *apiError) {
	result, err := academicValue(value)
	if err != nil || result == "" {
		return result, err
	}
	parsed, parseErr := strconv.ParseFloat(result, 64)
	if parseErr != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return "", apiErr(http.StatusBadRequest, "invalid_input", field+" must be a finite number")
	}
	if parsed < minimum || parsed > maximum {
		return "", apiErr(http.StatusBadRequest, "invalid_input", fmt.Sprintf("%s must be between %g and %g", field, minimum, maximum))
	}
	return result, nil
}

func academicValue(value any) (string, *apiError) {
	if value == nil {
		return "", nil
	}
	var result string
	switch typed := value.(type) {
	case string:
		result = strings.TrimSpace(typed)
	case json.Number:
		result = string(typed)
	case float64:
		result = strconv.FormatFloat(typed, 'f', -1, 64)
	case float32:
		result = strconv.FormatFloat(float64(typed), 'f', -1, 32)
	case int:
		result = strconv.Itoa(typed)
	case int64:
		result = strconv.FormatInt(typed, 10)
	case uint64:
		result = strconv.FormatUint(typed, 10)
	case int8:
		result = strconv.FormatInt(int64(typed), 10)
	case int16:
		result = strconv.FormatInt(int64(typed), 10)
	case int32:
		result = strconv.FormatInt(int64(typed), 10)
	case uint:
		result = strconv.FormatUint(uint64(typed), 10)
	case uint8:
		result = strconv.FormatUint(uint64(typed), 10)
	case uint16:
		result = strconv.FormatUint(uint64(typed), 10)
	case uint32:
		result = strconv.FormatUint(uint64(typed), 10)
	default:
		return "", apiErr(http.StatusBadRequest, "invalid_input", "academic values must be text or numbers")
	}
	if len(result) > 32 || strings.ContainsAny(result, "\r\n") {
		return "", apiErr(http.StatusBadRequest, "invalid_input", "academic values are too long")
	}
	return result, nil
}

func (s *Store) normalizeGradeLocked(input GradeInput, existing *Grade) (*Grade, *apiError) {
	studentRef := strings.TrimSpace(input.StudentID)
	if studentRef == "" && existing != nil {
		studentRef = existing.StudentID
	}
	student := s.resolveStudentLocked(studentRef)
	if student == nil {
		return nil, apiErr(http.StatusBadRequest, "student_not_found", "student not found")
	}
	courseRef := strings.TrimSpace(input.CourseID)
	if courseRef == "" {
		courseRef = strings.TrimSpace(input.CourseCode)
	}
	if courseRef == "" && existing != nil {
		courseRef = existing.CourseID
	}
	var course *Course
	for _, candidate := range s.courses {
		if candidate != nil && (strings.EqualFold(candidate.ID, courseRef) || strings.EqualFold(candidate.Code, courseRef)) {
			course = candidate
			break
		}
	}
	if course == nil {
		return nil, apiErr(http.StatusBadRequest, "course_not_found", "course not found")
	}
	score := ""
	point := ""
	if existing != nil {
		score, point = fmt.Sprint(existing.Score), fmt.Sprint(existing.Point)
		if existing.Score == nil {
			score = ""
		}
		if existing.Point == nil {
			point = ""
		}
	}
	if input.Score != nil {
		var err *apiError
		score, err = academicBoundedValue(input.Score, 0, 100, "score")
		if err != nil {
			return nil, err
		}
	}
	if input.Point != nil {
		var err *apiError
		point, err = academicBoundedValue(input.Point, 0, 4, "point")
		if err != nil {
			return nil, err
		}
	}
	// Revalidate preserved values as well as newly supplied values. This keeps
	// edits from carrying forward malformed legacy rows loaded from MySQL.
	var valueErr *apiError
	score, valueErr = academicBoundedValue(score, 0, 100, "score")
	if valueErr != nil {
		return nil, valueErr
	}
	point, valueErr = academicBoundedValue(point, 0, 4, "point")
	if valueErr != nil {
		return nil, valueErr
	}
	term := strings.TrimSpace(input.Term)
	if term == "" && existing != nil {
		term = existing.Term
	}
	if term == "" {
		term = course.Term
	}
	status := strings.ToLower(strings.TrimSpace(input.Status))
	if status == "" && existing != nil {
		status = strings.ToLower(strings.TrimSpace(existing.Status))
	}
	if status == "" {
		status = "graded"
	}
	if status == "in_progress" {
		status = "inprogress"
	}
	switch status {
	case "inprogress", "graded", "published", "withdrawn":
	default:
		return nil, apiErr(http.StatusBadRequest, "invalid_input", "grade status is not supported")
	}
	credits := int(course.Credits)
	if existing != nil {
		credits = existing.Credits
	}
	if input.Credits != nil {
		credits = *input.Credits
	}
	if credits < 0 || credits > 100 {
		return nil, apiErr(http.StatusBadRequest, "invalid_input", "grade credits are out of range")
	}
	id := strings.TrimSpace(input.ID)
	if id == "" && existing != nil {
		id = existing.ID
	}
	if id == "" {
		id = "grade-" + randomID(16)
	}
	return &Grade{ID: id, StudentID: student.ID, CourseID: course.ID, CourseCode: course.Code, CourseNameZh: course.NameZh, CourseNameEn: course.NameEn, Score: score, Point: point, Term: term, Status: status, Credits: credits}, nil
}

func (s *Store) createGrade(input GradeInput) (*Grade, *apiError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := s.normalizeGradeLocked(input, nil)
	if err != nil {
		return nil, err
	}
	for _, candidate := range s.grades {
		if candidate != nil && strings.EqualFold(candidate.ID, item.ID) {
			return nil, apiErr(http.StatusConflict, "grade_exists", "grade already exists")
		}
	}
	if err := s.persistGradeLocked(item); err != nil {
		return nil, apiErr(http.StatusServiceUnavailable, "grade_persistence_failed", "grade could not be saved")
	}
	s.grades = append(s.grades, item)
	copy := *item
	return &copy, nil
}

func (s *Store) updateGrade(id string, input GradeInput) (*Grade, *apiError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index, candidate := range s.grades {
		if candidate == nil || !strings.EqualFold(candidate.ID, strings.TrimSpace(id)) {
			continue
		}
		input.ID = candidate.ID
		item, err := s.normalizeGradeLocked(input, candidate)
		if err != nil {
			return nil, err
		}
		if persistErr := s.persistGradeLocked(item); persistErr != nil {
			return nil, apiErr(http.StatusServiceUnavailable, "grade_persistence_failed", "grade could not be saved")
		}
		s.grades[index] = item
		copy := *item
		return &copy, nil
	}
	return nil, apiErr(http.StatusNotFound, "grade_not_found", "grade not found")
}

func (s *Store) deleteGrade(id string) (*Grade, *apiError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index, candidate := range s.grades {
		if candidate == nil || !strings.EqualFold(candidate.ID, strings.TrimSpace(id)) {
			continue
		}
		copy := *candidate
		if err := s.deleteGradePersistedLocked(candidate.ID); err != nil {
			return nil, apiErr(http.StatusServiceUnavailable, "grade_persistence_failed", "grade could not be deleted")
		}
		s.grades = append(s.grades[:index], s.grades[index+1:]...)
		return &copy, nil
	}
	return nil, apiErr(http.StatusNotFound, "grade_not_found", "grade not found")
}

func normalizeScheduleClock(value string) (string, bool) {
	parsed, err := time.Parse("15:04", strings.TrimSpace(value))
	if err != nil {
		return "", false
	}
	return parsed.Format("15:04"), true
}

func (s *Store) normalizeScheduleLocked(input ScheduleInput, existing *ScheduleEntry) (*ScheduleEntry, *apiError) {
	studentRef := strings.TrimSpace(input.StudentID)
	if studentRef == "" && existing != nil {
		studentRef = existing.StudentID
	}
	student := s.resolveStudentLocked(studentRef)
	if student == nil {
		return nil, apiErr(http.StatusBadRequest, "student_not_found", "student not found")
	}
	courseRef := strings.TrimSpace(input.CourseID)
	if courseRef == "" {
		courseRef = strings.TrimSpace(input.CourseCode)
	}
	if courseRef == "" && existing != nil {
		courseRef = existing.CourseID
	}
	var course *Course
	for _, candidate := range s.courses {
		if candidate != nil && (strings.EqualFold(candidate.ID, courseRef) || strings.EqualFold(candidate.Code, courseRef)) {
			course = candidate
			break
		}
	}
	if course == nil {
		return nil, apiErr(http.StatusBadRequest, "course_not_found", "course not found")
	}
	day := 0
	if existing != nil {
		day = existing.Day
	}
	if input.Day != nil {
		day = *input.Day
	}
	if day < 1 || day > 7 {
		return nil, apiErr(http.StatusBadRequest, "invalid_input", "schedule day must be between 1 and 7")
	}
	start, end := strings.TrimSpace(input.Start), strings.TrimSpace(input.End)
	if start == "" && existing != nil {
		start = existing.Start
	}
	if end == "" && existing != nil {
		end = existing.End
	}
	start, startOK := normalizeScheduleClock(start)
	end, endOK := normalizeScheduleClock(end)
	if !startOK || !endOK {
		return nil, apiErr(http.StatusBadRequest, "invalid_input", "schedule times must use HH:MM")
	}
	startTime, _ := time.Parse("15:04", start)
	endTime, _ := time.Parse("15:04", end)
	if !endTime.After(startTime) {
		return nil, apiErr(http.StatusBadRequest, "invalid_input", "schedule end time must be after start time")
	}
	location := strings.TrimSpace(input.Location)
	if location == "" && existing != nil {
		location = existing.Location
	}
	if location == "" {
		location = "待定"
	}
	teacher := strings.TrimSpace(input.Teacher)
	if teacher == "" && existing != nil {
		teacher = existing.Teacher
	}
	if teacher == "" {
		teacher = course.Teacher
	}
	if len([]rune(location)) > 255 || len([]rune(teacher)) > 255 {
		return nil, apiErr(http.StatusBadRequest, "invalid_input", "schedule fields are too long")
	}
	id := strings.TrimSpace(input.ID)
	if id == "" && existing != nil {
		id = existing.ID
	}
	if id == "" {
		id = "schedule-" + randomID(16)
	}
	return &ScheduleEntry{ID: id, StudentID: student.ID, CourseID: course.ID, CourseCode: course.Code, CourseNameZh: course.NameZh, CourseNameEn: course.NameEn, Day: day, Start: start, End: end, Location: location, Teacher: teacher}, nil
}

func schedulesOverlap(left, right *ScheduleEntry) bool {
	if left == nil || right == nil || left.Day != right.Day || !strings.EqualFold(left.StudentID, right.StudentID) {
		return false
	}
	// Times are normalized to zero-padded HH:MM before this check, so lexical
	// ordering is equivalent to ordering minutes. Equal end/start boundaries
	// are adjacent classes and are intentionally allowed.
	return left.Start < right.End && right.Start < left.End
}

func (s *Store) scheduleConflictLocked(candidate *ScheduleEntry, excludeID string) bool {
	for _, existing := range s.schedule {
		if existing == nil || strings.EqualFold(existing.ID, excludeID) {
			continue
		}
		if schedulesOverlap(candidate, existing) {
			return true
		}
	}
	return false
}

func (s *Store) createSchedule(input ScheduleInput) (*ScheduleEntry, *apiError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := s.normalizeScheduleLocked(input, nil)
	if err != nil {
		return nil, err
	}
	for _, candidate := range s.schedule {
		if candidate != nil && strings.EqualFold(candidate.ID, item.ID) {
			return nil, apiErr(http.StatusConflict, "schedule_exists", "schedule entry already exists")
		}
	}
	if s.scheduleConflictLocked(item, "") {
		return nil, apiErr(http.StatusConflict, "schedule_overlap", "the student already has an overlapping class at this time")
	}
	if err := s.persistScheduleLocked(item); err != nil {
		return nil, apiErr(http.StatusServiceUnavailable, "schedule_persistence_failed", "schedule entry could not be saved")
	}
	s.schedule = append(s.schedule, item)
	copy := *item
	return &copy, nil
}

func (s *Store) updateSchedule(id string, input ScheduleInput) (*ScheduleEntry, *apiError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index, candidate := range s.schedule {
		if candidate == nil || !strings.EqualFold(candidate.ID, strings.TrimSpace(id)) {
			continue
		}
		input.ID = candidate.ID
		item, err := s.normalizeScheduleLocked(input, candidate)
		if err != nil {
			return nil, err
		}
		if s.scheduleConflictLocked(item, candidate.ID) {
			return nil, apiErr(http.StatusConflict, "schedule_overlap", "the student already has an overlapping class at this time")
		}
		if persistErr := s.persistScheduleLocked(item); persistErr != nil {
			return nil, apiErr(http.StatusServiceUnavailable, "schedule_persistence_failed", "schedule entry could not be saved")
		}
		s.schedule[index] = item
		copy := *item
		return &copy, nil
	}
	return nil, apiErr(http.StatusNotFound, "schedule_not_found", "schedule entry not found")
}

func (s *Store) deleteSchedule(id string) (*ScheduleEntry, *apiError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index, candidate := range s.schedule {
		if candidate == nil || !strings.EqualFold(candidate.ID, strings.TrimSpace(id)) {
			continue
		}
		copy := *candidate
		if err := s.deleteSchedulePersistedLocked(candidate.ID); err != nil {
			return nil, apiErr(http.StatusServiceUnavailable, "schedule_persistence_failed", "schedule entry could not be deleted")
		}
		s.schedule = append(s.schedule[:index], s.schedule[index+1:]...)
		return &copy, nil
	}
	return nil, apiErr(http.StatusNotFound, "schedule_not_found", "schedule entry not found")
}

func (s *Store) createCourse(input CourseInput) (*Course, *apiError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	course, err := normalizeCourse(input, nil)
	if err != nil {
		return nil, err
	}
	for _, item := range s.courses {
		if strings.EqualFold(item.ID, course.ID) || strings.EqualFold(item.Code, course.Code) {
			return nil, apiErr(409, "course_exists", "course id or code already exists")
		}
	}
	if err := s.persistCourseLocked(course); err != nil {
		return nil, apiErr(http.StatusServiceUnavailable, "course_persistence_failed", "course could not be saved")
	}
	s.courses = append(s.courses, course)
	copy := *course
	return &copy, nil
}

func (s *Store) updateCourse(id string, input CourseInput) (*Course, *apiError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := -1
	for i, item := range s.courses {
		if strings.EqualFold(item.ID, id) {
			index = i
			break
		}
	}
	if index < 0 {
		return nil, apiErr(404, "course_not_found", "course not found")
	}
	normalized, err := normalizeCourse(input, s.courses[index])
	if err != nil {
		return nil, err
	}
	for i, item := range s.courses {
		if i != index && (strings.EqualFold(item.ID, normalized.ID) || strings.EqualFold(item.Code, normalized.Code)) {
			return nil, apiErr(409, "course_exists", "course id or code already exists")
		}
	}
	if err := s.persistCourseLocked(normalized); err != nil {
		return nil, apiErr(http.StatusServiceUnavailable, "course_persistence_failed", "course could not be saved")
	}
	*s.courses[index] = *normalized
	copy := *normalized
	return &copy, nil
}

func (s *Store) deleteCourse(id string) (*Course, *apiError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, item := range s.courses {
		if strings.EqualFold(item.ID, id) {
			copy := *item
			for _, enrollment := range s.enrollments {
				if enrollment != nil && strings.EqualFold(enrollment.CourseID, item.ID) {
					return nil, apiErr(http.StatusConflict, "course_in_use", "courses with enrollment history cannot be deleted")
				}
			}
			for _, grade := range s.grades {
				if grade != nil && strings.EqualFold(grade.CourseID, item.ID) {
					return nil, apiErr(http.StatusConflict, "course_in_use", "courses with academic records cannot be deleted")
				}
			}
			for _, entry := range s.schedule {
				if entry != nil && strings.EqualFold(entry.CourseID, item.ID) {
					return nil, apiErr(http.StatusConflict, "course_in_use", "courses with schedule records cannot be deleted")
				}
			}
			if err := s.deleteCoursePersistedLocked(item.ID); err != nil {
				return nil, apiErr(http.StatusServiceUnavailable, "course_persistence_failed", "course could not be deleted")
			}
			s.courses = append(s.courses[:i], s.courses[i+1:]...)
			return &copy, nil
		}
	}
	return nil, apiErr(404, "course_not_found", "course not found")
}

func (s *Store) createAnnouncement(input AnnouncementInput) (*Announcement, *apiError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := normalizeAnnouncement(input, nil)
	if err != nil {
		return nil, err
	}
	if err := s.persistAnnouncementLocked(item); err != nil {
		return nil, apiErr(http.StatusServiceUnavailable, "announcement_persistence_failed", "announcement could not be saved")
	}
	s.announcements = append(s.announcements, item)
	copy := *item
	return &copy, nil
}

func (s *Store) updateAnnouncement(id string, input AnnouncementInput) (*Announcement, *apiError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, item := range s.announcements {
		if strings.EqualFold(item.ID, id) {
			normalized, err := normalizeAnnouncement(input, item)
			if err != nil {
				return nil, err
			}
			if err := s.persistAnnouncementLocked(normalized); err != nil {
				return nil, apiErr(http.StatusServiceUnavailable, "announcement_persistence_failed", "announcement could not be saved")
			}
			s.announcements[i] = normalized
			copy := *normalized
			return &copy, nil
		}
	}
	return nil, apiErr(404, "announcement_not_found", "announcement not found")
}

func (s *Store) deleteAnnouncement(id string) (*Announcement, *apiError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, item := range s.announcements {
		if strings.EqualFold(item.ID, id) {
			copy := *item
			if err := s.deleteAnnouncementPersistedLocked(item.ID); err != nil {
				return nil, apiErr(http.StatusServiceUnavailable, "announcement_persistence_failed", "announcement could not be deleted")
			}
			s.announcements = append(s.announcements[:i], s.announcements[i+1:]...)
			return &copy, nil
		}
	}
	return nil, apiErr(404, "announcement_not_found", "announcement not found")
}

func fieldCleared(fields []string, names ...string) bool {
	wanted := make(map[string]struct{}, len(names)*2)
	for _, name := range names {
		normalized := strings.ToLower(strings.TrimSpace(name))
		if normalized == "" {
			continue
		}
		wanted[normalized] = struct{}{}
		wanted[strings.ReplaceAll(normalized, "_", "")] = struct{}{}
	}
	for _, field := range fields {
		normalized := strings.ToLower(strings.TrimSpace(field))
		if _, ok := wanted[normalized]; ok {
			return true
		}
		if _, ok := wanted[strings.ReplaceAll(normalized, "_", "")]; ok {
			return true
		}
	}
	return false
}

func normalizeCourse(input CourseInput, existing *Course) (*Course, *apiError) {
	nameZh := first(input.NameZh, input.Name)
	nameEn := strings.TrimSpace(input.NameEn)
	code := strings.TrimSpace(input.Code)
	id := strings.TrimSpace(input.ID)
	clearNameEn := fieldCleared(input.ClearFields, "nameEn", "name_en")
	clearDepartment := fieldCleared(input.ClearFields, "department")
	clearDepartmentEn := fieldCleared(input.ClearFields, "departmentEn", "department_en")
	clearTeacher := fieldCleared(input.ClearFields, "teacher")
	clearTeacherEn := fieldCleared(input.ClearFields, "teacherEn", "teacher_en")
	clearDescription := fieldCleared(input.ClearFields, "description")
	clearDescriptionEn := fieldCleared(input.ClearFields, "descriptionEn", "description_en")
	clearTerm := fieldCleared(input.ClearFields, "term")
	clearTermEn := fieldCleared(input.ClearFields, "termEn", "term_en")
	clearType := fieldCleared(input.ClearFields, "type", "courseType", "course_type")
	if existing != nil {
		if nameZh == "" {
			nameZh = existing.NameZh
		}
		if nameEn == "" && !clearNameEn {
			nameEn = existing.NameEn
		}
		if code == "" {
			code = existing.Code
		}
		if id == "" {
			id = existing.ID
		}
	}
	if nameZh == "" || code == "" {
		return nil, apiErr(400, "invalid_input", "code and name are required")
	}
	if nameEn == "" && !clearNameEn {
		nameEn = nameZh
	}
	department := strings.TrimSpace(input.Department)
	departmentEn := strings.TrimSpace(input.DepartmentEn)
	teacher := strings.TrimSpace(input.Teacher)
	teacherEn := strings.TrimSpace(input.TeacherEn)
	description := strings.TrimSpace(input.Description)
	descriptionEn := strings.TrimSpace(input.DescriptionEn)
	term := strings.TrimSpace(input.Term)
	termEn := strings.TrimSpace(input.TermEn)
	courseType := strings.TrimSpace(input.Type)
	if existing != nil {
		if department == "" && !clearDepartment {
			department = existing.Department
		}
		if departmentEn == "" && !clearDepartmentEn {
			departmentEn = existing.DepartmentEn
		}
		if teacher == "" && !clearTeacher {
			teacher = existing.Teacher
		}
		if teacherEn == "" && !clearTeacherEn {
			teacherEn = existing.TeacherEn
		}
		if description == "" && !clearDescription {
			description = existing.Description
		}
		if descriptionEn == "" && !clearDescriptionEn {
			descriptionEn = existing.DescriptionEn
		}
		if term == "" && !clearTerm {
			term = existing.Term
		}
		if termEn == "" && !clearTermEn {
			termEn = existing.TermEn
		}
		if courseType == "" && !clearType {
			courseType = existing.Type
		}
	}
	if department == "" && !clearDepartment {
		department = "综合学院"
	}
	if departmentEn == "" && !clearDepartmentEn {
		departmentEn = department
	}
	if teacher == "" && !clearTeacher {
		teacher = "待定"
	}
	if teacherEn == "" && !clearTeacherEn {
		teacherEn = teacher
	}
	if descriptionEn == "" && !clearDescriptionEn {
		descriptionEn = description
	}
	if term == "" && !clearTerm {
		term = "2026-秋"
	}
	if termEn == "" && !clearTermEn {
		termEn = term
	}
	if courseType == "" && !clearType {
		courseType = "elective"
	}
	if len([]rune(department)) > 255 || len([]rune(departmentEn)) > 255 || len([]rune(teacher)) > 255 || len([]rune(teacherEn)) > 255 || len([]rune(description)) > 8000 || len([]rune(descriptionEn)) > 8000 || len([]rune(term)) > 64 || len([]rune(termEn)) > 64 {
		return nil, apiErr(http.StatusBadRequest, "invalid_input", "course text fields are too long")
	}
	credits := float64(3)
	capacity := 40
	enrolledCount := 0
	if existing != nil {
		credits, capacity, enrolledCount = existing.Credits, existing.Capacity, existing.EnrolledCount
	}
	if input.Credits != nil {
		credits = *input.Credits
	}
	if input.Capacity != nil {
		capacity = *input.Capacity
	}
	if credits < 0 {
		credits = 0
	}
	if capacity < 1 {
		capacity = 1
	}
	if id == "" {
		id = "course-" + randomID(12)
	}
	return &Course{ID: id, Code: code, NameZh: nameZh, NameEn: nameEn, Department: department, DepartmentEn: departmentEn, Teacher: teacher, TeacherEn: teacherEn, Credits: credits, Description: description, DescriptionEn: descriptionEn, Capacity: capacity, EnrolledCount: enrolledCount, Type: courseType, Term: term, TermEn: termEn}, nil
}

func normalizeAnnouncement(input AnnouncementInput, existing *Announcement) (*Announcement, *apiError) {
	titleZh := first(input.TitleZh, input.Title)
	titleEn := strings.TrimSpace(input.TitleEn)
	contentZh := first(input.ContentZh, input.Content, input.Body)
	contentEn := strings.TrimSpace(input.ContentEn)
	id := strings.TrimSpace(input.ID)
	typ, audience, courseID, publishedAt := strings.TrimSpace(input.Type), strings.TrimSpace(input.Audience), strings.TrimSpace(input.CourseID), strings.TrimSpace(input.PublishedAt)
	clearTitleEn := fieldCleared(input.ClearFields, "titleEn", "title_en")
	clearContentEn := fieldCleared(input.ClearFields, "contentEn", "content_en")
	clearType := fieldCleared(input.ClearFields, "type")
	clearAudience := fieldCleared(input.ClearFields, "audience")
	clearCourseID := fieldCleared(input.ClearFields, "courseId", "course_id")
	clearPublishedAt := fieldCleared(input.ClearFields, "publishedAt", "published_at")
	published := true
	if existing != nil {
		if titleZh == "" {
			titleZh = existing.TitleZh
		}
		if titleEn == "" && !clearTitleEn {
			titleEn = existing.TitleEn
		}
		if contentZh == "" {
			contentZh = existing.ContentZh
		}
		if contentEn == "" && !clearContentEn {
			contentEn = existing.ContentEn
		}
		if id == "" {
			id = existing.ID
		}
		if typ == "" && !clearType {
			typ = existing.Type
		}
		if audience == "" && !clearAudience {
			audience = existing.Audience
		}
		if courseID == "" && !clearCourseID {
			courseID = existing.CourseID
		}
		if publishedAt == "" && !clearPublishedAt {
			publishedAt = existing.PublishedAt
		}
		published = existing.Published
	}
	if input.PublishedAt2 != "" && publishedAt == "" {
		publishedAt = input.PublishedAt2
	}
	if input.Published != nil {
		published = *input.Published
	}
	if titleZh == "" || contentZh == "" {
		return nil, apiErr(400, "invalid_input", "title and content are required")
	}
	if titleEn == "" && !clearTitleEn {
		titleEn = titleZh
	}
	if contentEn == "" && !clearContentEn {
		contentEn = contentZh
	}
	if typ == "" && !clearType {
		typ = "CAMPUS"
	}
	if audience == "" && !clearAudience {
		audience = "all"
	}
	if publishedAt == "" && !clearPublishedAt {
		publishedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if id == "" {
		id = "announcement-" + randomID(16)
	}
	author := "admin"
	if existing != nil && existing.Author != "" {
		author = existing.Author
	}
	return &Announcement{ID: id, TitleZh: titleZh, TitleEn: titleEn, ContentZh: contentZh, ContentEn: contentEn, Type: typ, Audience: audience, CourseID: courseID, PublishedAt: publishedAt, Published: published, Author: author}, nil
}

func normalizeAdmission(input AdmissionApplicationInput, existing *AdmissionApplication) (*AdmissionApplication, *apiError) {
	name := strings.TrimSpace(input.Name)
	email := strings.TrimSpace(input.Email)
	school := canonicalAdmissionSchool(input.School)
	status := strings.ToLower(strings.TrimSpace(input.Status))
	notes := strings.TrimSpace(input.Notes)
	if existing != nil {
		if name == "" {
			name = existing.Name
		}
		if email == "" {
			email = existing.Email
		}
		if school == "" {
			school = existing.School
		}
		if status == "" {
			status = existing.Status
		}
		if input.Notes == "" && !(input.ClearNotes != nil && *input.ClearNotes) {
			notes = existing.Notes
		}
	}
	if name == "" || email == "" || school == "" {
		return nil, apiErr(http.StatusBadRequest, "invalid_input", "name, email, and school are required")
	}
	if len([]rune(name)) > 120 || len([]rune(school)) > 160 || len([]rune(notes)) > 2000 {
		return nil, apiErr(http.StatusBadRequest, "invalid_input", "application fields are too long")
	}
	if len(email) > 254 || strings.ContainsAny(email, "\r\n") {
		return nil, apiErr(http.StatusBadRequest, "invalid_input", "a valid email address is required")
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || !strings.Contains(email, "@") {
		return nil, apiErr(http.StatusBadRequest, "invalid_input", "a valid email address is required")
	}
	if status == "" {
		status = "pending"
	}
	switch status {
	case "pending", "reviewing", "contacted", "accepted", "rejected", "withdrawn":
	default:
		return nil, apiErr(http.StatusBadRequest, "invalid_input", "status is not supported")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	id := ""
	createdAt := now
	updatedAt := now
	if existing != nil {
		id = existing.ID
		createdAt = existing.CreatedAt
	}
	if id == "" {
		id = "application-" + randomID(16)
	}
	result := &AdmissionApplication{ID: id, Name: name, Email: email, School: school, Status: status, Notes: notes, CreatedAt: createdAt, UpdatedAt: updatedAt}
	if existing != nil {
		result.StudentID = existing.StudentID
		result.ApprovedAt = existing.ApprovedAt
		result.ApprovedBy = existing.ApprovedBy
		result.InitialPasswordIssuedAt = existing.InitialPasswordIssuedAt
		// Delivery state belongs to the linked mailbox record, but preserving the
		// current projection here prevents a notes-only PATCH from briefly
		// clearing the status returned to the administrator.
		result.DeliveryStatus = existing.DeliveryStatus
		result.DeliveryError = existing.DeliveryError
	}
	return result, nil
}

// canonicalAdmissionSchool keeps public form submissions stable across
// browser languages. Older clients may still send a localized label, so the
// aliases are accepted and normalized to one Chinese record value.
func canonicalAdmissionSchool(value string) string {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "undecided", "还在探索中", "still exploring", "to be decided":
		return "还在探索中"
	case "wind", "风与自然科学", "wind & natural sciences":
		return "风与自然科学"
	case "contract", "契约与商业文明", "contracts & commercial civilization":
		return "契约与商业文明"
	case "design", "永恒与设计实践", "eternity & design practice":
		return "永恒与设计实践"
	case "wisdom", "智慧与生命研究", "wisdom & life studies":
		return "智慧与生命研究"
	case "justice", "审判与机械文明", "justice & mechanical civilization":
		return "审判与机械文明"
	case "flame", "火与竞技生态", "fire & competitive ecology":
		return "火与竞技生态"
	case "polar", "至冬研究与极地治理", "snezhnaya studies & polar governance":
		return "至冬研究与极地治理"
	default:
		return value
	}
}

func first(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

type Server struct {
	store             *Store
	mailer            ExternalMailSender
	smtpSlots         chan struct{}
	smtpTimeout       time.Duration
	staticDir         string
	publicOrigin      string
	trustedProxies    []netip.Prefix
	sessions          map[string]session
	sessionsMu        sync.Mutex
	loginMu           sync.Mutex
	loginAttempts     map[string]*loginAttempt
	passwordSlots     chan struct{}
	admissionMu       sync.Mutex
	admissionAttempts map[string]*admissionAttempt
	cookieSecure      bool
	storageMode       string
	storageMu         sync.RWMutex
}

type loginAttempt struct {
	failures   int
	windowFrom time.Time
	blockedTo  time.Time
}

type admissionAttempt struct {
	count      int
	windowFrom time.Time
}

func NewServer(store *Store, staticDir string) *Server {
	if strings.TrimSpace(staticDir) == "" {
		staticDir = "web"
	}
	secure := strings.EqualFold(os.Getenv("CGU_COOKIE_SECURE"), "1") || strings.EqualFold(os.Getenv("CGU_COOKIE_SECURE"), "true")
	return &Server{store: store, smtpSlots: make(chan struct{}, 4), smtpTimeout: mailboxExternalSendTimeout, staticDir: staticDir, sessions: make(map[string]session), loginAttempts: make(map[string]*loginAttempt), passwordSlots: make(chan struct{}, maxPasswordChecks), admissionAttempts: make(map[string]*admissionAttempt), cookieSecure: secure, storageMode: "memory"}
}

func (s *Server) setExternalMailSender(sender ExternalMailSender) {
	s.mailer = sender
	s.smtpTimeout = mailboxExternalSendTimeout
	if mailer, ok := sender.(*SMTPMailer); ok && mailer != nil && mailer.cfg.TimeoutSecond > 0 {
		s.smtpTimeout = time.Duration(mailer.cfg.TimeoutSecond) * time.Second
	}
}

func (s *Server) setTrustedProxies(values []string) error {
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			address, addressErr := netip.ParseAddr(value)
			if addressErr != nil {
				return fmt.Errorf("invalid trusted proxy %q", value)
			}
			prefix = netip.PrefixFrom(address, address.BitLen())
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	s.trustedProxies = prefixes
	return nil
}

func (s *Server) setStorageMode(mode string) {
	s.storageMu.Lock()
	s.storageMode = mode
	s.storageMu.Unlock()
}

func (s *Server) storageStatus() string {
	s.storageMu.RLock()
	defer s.storageMu.RUnlock()
	return s.storageMode
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if r.TLS != nil {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	}
	if r.Method == http.MethodOptions && strings.HasPrefix(r.URL.Path, "/api") {
		s.handleOptions(w, r)
		return
	}
	if !s.originAllowed(r) {
		writeError(w, apiErr(403, "origin_not_allowed", "request origin is not allowed"))
		return
	}
	if stateChangingAPIRequest(r) && r.Header.Get("X-CGU-Request") != "1" {
		writeError(w, apiErr(403, "csrf_required", "a same-site request header is required"))
		return
	}
	if r.URL.Path == "/healthz" || strings.HasPrefix(r.URL.Path, "/api") {
		w.Header().Set("Cache-Control", "no-store")
		s.handleAPI(w, r)
		return
	}
	s.serveStatic(w, r)
}

func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimRight(path.Clean(r.URL.Path), "/")
	if p == "." || p == "" {
		p = "/"
	}
	switch {
	case p == "/healthz" || p == "/api/healthz":
		mode := s.storageStatus()
		status := "healthy"
		if mode == "mysql-degraded" || mode == "memory-fallback" {
			status = "degraded"
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "CGU", "status": status, "storage": mode})
	case (p == "/api/auth/login" || p == "/api/login") && r.Method == http.MethodPost:
		s.login(w, r)
	case (p == "/api/auth/logout" || p == "/api/logout") && r.Method == http.MethodPost:
		s.logout(w, r)
	case p == "/api/auth/me" || p == "/api/me":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if user := s.currentUser(r); user != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "user": s.store.publicUser(user)})
		} else {
			writeError(w, apiErr(401, "authentication_required", "please log in first"))
		}
	case (p == "/api/auth/password" || p == "/api/password") && (r.Method == http.MethodPost || r.Method == http.MethodPatch):
		if !s.requireAuth(w, r) {
			return
		}
		s.changePassword(w, r)
	case p == "/api/auth/password" || p == "/api/password":
		methodNotAllowed(w, http.MethodPost, http.MethodPatch)
	case p == "/api/catalog.csv" && r.Method == http.MethodGet:
		s.downloadCatalog(w, r)
	case p == "/api/transcript.csv" && r.Method == http.MethodGet:
		if !s.requireAuth(w, r) {
			return
		}
		s.downloadTranscript(w, r)
	case p == "/api/courses" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "courses": s.store.coursesFor(s.currentUser(r), false)})
	case p == "/api/courses" && r.Method == http.MethodPost:
		if !s.requireAdmin(w, r) {
			return
		}
		s.createCourse(w, r)
	case strings.HasPrefix(p, "/api/courses/"):
		id := decodePathID(strings.TrimPrefix(p, "/api/courses/"))
		if id == "" {
			writeError(w, apiErr(404, "course_not_found", "course not found"))
			return
		}
		if r.Method != http.MethodPatch && r.Method != http.MethodPut && r.Method != http.MethodDelete {
			methodNotAllowed(w, http.MethodPatch, http.MethodPut, http.MethodDelete)
			return
		}
		if !s.requireAdmin(w, r) {
			return
		}
		if r.Method == http.MethodDelete {
			s.deleteCourse(w, id)
		} else {
			s.updateCourse(w, r, id)
		}
	case p == "/api/enrollments":
		if !s.requireAuth(w, r) {
			return
		}
		if r.Method == http.MethodGet {
			s.listEnrollments(w, r)
		} else if r.Method == http.MethodPost {
			s.changeEnrollment(w, r)
		} else {
			methodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
	case p == "/api/grades":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if !s.requireAuth(w, r) {
			return
		}
		s.listGrades(w, r)
	case p == "/api/schedule":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if !s.requireAuth(w, r) {
			return
		}
		s.listSchedule(w, r)
	case p == "/api/mailbox" && r.Method == http.MethodGet:
		if !s.requireAuth(w, r) {
			return
		}
		s.listMailbox(w, r)
	case strings.HasPrefix(p, "/api/mailbox/"):
		id := decodePathID(strings.TrimPrefix(p, "/api/mailbox/"))
		if id == "" {
			writeError(w, apiErr(http.StatusNotFound, "message_not_found", "message not found"))
			return
		}
		if r.Method != http.MethodPatch && r.Method != http.MethodPut {
			methodNotAllowed(w, http.MethodPatch, http.MethodPut)
			return
		}
		if !s.requireAuth(w, r) {
			return
		}
		s.markMailboxRead(w, r, id)
	case p == "/api/announcements" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "announcements": s.store.announcementsFor(s.currentUser(r), false)})
	case p == "/api/site-content" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "content": s.store.siteContentList()})
	case p == "/api/admissions" && r.Method == http.MethodPost:
		s.submitAdmission(w, r)
	case p == "/api/admissions":
		methodNotAllowed(w, http.MethodPost)
	case p == "/api/announcements" && r.Method == http.MethodPost:
		if !s.requireAdmin(w, r) {
			return
		}
		s.createAnnouncement(w, r)
	case strings.HasPrefix(p, "/api/announcements/"):
		id := decodePathID(strings.TrimPrefix(p, "/api/announcements/"))
		if id == "" {
			writeError(w, apiErr(404, "announcement_not_found", "announcement not found"))
			return
		}
		if r.Method == http.MethodGet {
			s.announcementByID(w, r, id)
			return
		}
		if r.Method != http.MethodPatch && r.Method != http.MethodPut && r.Method != http.MethodDelete {
			methodNotAllowed(w, http.MethodGet, http.MethodPatch, http.MethodPut, http.MethodDelete)
			return
		}
		if !s.requireAdmin(w, r) {
			return
		}
		if r.Method == http.MethodDelete {
			s.deleteAnnouncement(w, id)
		} else {
			s.updateAnnouncement(w, r, id)
		}
	case p == "/api/admin/stats":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if !s.requireAdmin(w, r) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "stats": s.store.stats()})
	case p == "/api/admin/notifications":
		if !s.requireAdmin(w, r) {
			return
		}
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		s.listAdminNotifications(w, r)
	case strings.HasPrefix(p, "/api/admin/notifications/"):
		if !s.requireAdmin(w, r) {
			return
		}
		id := decodePathID(strings.TrimPrefix(p, "/api/admin/notifications/"))
		if id == "" {
			writeError(w, apiErr(http.StatusNotFound, "notification_not_found", "notification not found"))
			return
		}
		if r.Method != http.MethodPatch && r.Method != http.MethodPut {
			methodNotAllowed(w, http.MethodPatch, http.MethodPut)
			return
		}
		s.markAdminNotificationRead(w, r, id)
	case p == "/api/admin/mailbox":
		if !s.requireAdmin(w, r) {
			return
		}
		if r.Method == http.MethodGet {
			s.listAdminMailbox(w, r)
		} else if r.Method == http.MethodPost {
			s.sendAdminMailbox(w, r)
		} else {
			methodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
	case strings.HasPrefix(p, "/api/admin/mailbox/"):
		if !s.requireAdmin(w, r) {
			return
		}
		parts := strings.Split(strings.Trim(strings.TrimPrefix(p, "/api/admin/mailbox/"), "/"), "/")
		if len(parts) != 2 || parts[1] != "retry" {
			writeError(w, apiErr(http.StatusNotFound, "message_not_found", "message not found"))
			return
		}
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		s.retryAdminMailbox(w, r, decodePathID(parts[0]))
	case p == "/api/admin/courses":
		if !s.requireAdmin(w, r) {
			return
		}
		if r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "courses": s.store.coursesFor(nil, true)})
		} else if r.Method == http.MethodPost {
			s.createCourse(w, r)
		} else {
			methodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
	case strings.HasPrefix(p, "/api/admin/courses/"):
		if !s.requireAdmin(w, r) {
			return
		}
		id := decodePathID(strings.TrimPrefix(p, "/api/admin/courses/"))
		if r.Method == http.MethodDelete {
			s.deleteCourse(w, id)
		} else if r.Method == http.MethodPatch || r.Method == http.MethodPut {
			s.updateCourse(w, r, id)
		} else {
			methodNotAllowed(w, http.MethodPatch, http.MethodPut, http.MethodDelete)
		}
	case p == "/api/admin/students":
		if !s.requireAdmin(w, r) {
			return
		}
		if r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "students": s.store.studentsForAdmin()})
		} else if r.Method == http.MethodPost {
			s.createStudent(w, r)
		} else {
			methodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
	case strings.HasPrefix(p, "/api/admin/students/"):
		if !s.requireAdmin(w, r) {
			return
		}
		id := decodePathID(strings.TrimPrefix(p, "/api/admin/students/"))
		if r.Method == http.MethodPatch || r.Method == http.MethodPut {
			s.updateStudent(w, r, id)
		} else {
			methodNotAllowed(w, http.MethodPatch, http.MethodPut)
		}
	case p == "/api/admin/grades":
		if !s.requireAdmin(w, r) {
			return
		}
		if r.Method == http.MethodGet {
			s.listAdminGrades(w, r)
		} else if r.Method == http.MethodPost {
			s.createGrade(w, r)
		} else {
			methodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
	case strings.HasPrefix(p, "/api/admin/grades/"):
		if !s.requireAdmin(w, r) {
			return
		}
		id := decodePathID(strings.TrimPrefix(p, "/api/admin/grades/"))
		if r.Method == http.MethodPatch || r.Method == http.MethodPut {
			s.updateGrade(w, r, id)
		} else if r.Method == http.MethodDelete {
			s.deleteGrade(w, id)
		} else {
			methodNotAllowed(w, http.MethodPatch, http.MethodPut, http.MethodDelete)
		}
	case p == "/api/admin/schedule":
		if !s.requireAdmin(w, r) {
			return
		}
		if r.Method == http.MethodGet {
			s.listAdminSchedule(w, r)
		} else if r.Method == http.MethodPost {
			s.createSchedule(w, r)
		} else {
			methodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
	case strings.HasPrefix(p, "/api/admin/schedule/"):
		if !s.requireAdmin(w, r) {
			return
		}
		id := decodePathID(strings.TrimPrefix(p, "/api/admin/schedule/"))
		if r.Method == http.MethodPatch || r.Method == http.MethodPut {
			s.updateSchedule(w, r, id)
		} else if r.Method == http.MethodDelete {
			s.deleteSchedule(w, id)
		} else {
			methodNotAllowed(w, http.MethodPatch, http.MethodPut, http.MethodDelete)
		}
	case p == "/api/admin/announcements":
		if !s.requireAdmin(w, r) {
			return
		}
		if r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "announcements": s.store.announcementsFor(nil, true)})
		} else if r.Method == http.MethodPost {
			s.createAnnouncement(w, r)
		} else {
			methodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
	case p == "/api/admin/site-content":
		if !s.requireAdmin(w, r) {
			return
		}
		if r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "content": s.store.siteContentList()})
		} else if r.Method == http.MethodPut || r.Method == http.MethodPost {
			s.updateSiteContent(w, r)
		} else {
			methodNotAllowed(w, http.MethodGet, http.MethodPut, http.MethodPost)
		}
	case p == "/api/admin/admissions":
		if !s.requireAdmin(w, r) {
			return
		}
		if r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "applications": s.store.admissionsList()})
		} else {
			methodNotAllowed(w, http.MethodGet)
		}
	case strings.HasPrefix(p, "/api/admin/admissions/"):
		if !s.requireAdmin(w, r) {
			return
		}
		suffix := strings.Trim(strings.TrimPrefix(p, "/api/admin/admissions/"), "/")
		parts := strings.Split(suffix, "/")
		if len(parts) == 2 && parts[1] == "approve" {
			if r.Method != http.MethodPost {
				methodNotAllowed(w, http.MethodPost)
				return
			}
			s.approveAdmission(w, r, decodePathID(parts[0]))
			return
		}
		id := decodePathID(suffix)
		if r.Method == http.MethodPatch || r.Method == http.MethodPut {
			s.updateAdmission(w, r, id)
		} else if r.Method == http.MethodDelete {
			// An admission record is part of the audit trail. The only workflow
			// decision is the explicit approve action; deleting a pending row
			// would bypass notification and review history.
			methodNotAllowed(w, http.MethodPatch, http.MethodPut)
		} else {
			methodNotAllowed(w, http.MethodPatch, http.MethodPut, http.MethodDelete)
		}
	case strings.HasPrefix(p, "/api/admin/announcements/"):
		if !s.requireAdmin(w, r) {
			return
		}
		id := decodePathID(strings.TrimPrefix(p, "/api/admin/announcements/"))
		if r.Method == http.MethodDelete {
			s.deleteAnnouncement(w, id)
		} else if r.Method == http.MethodPatch || r.Method == http.MethodPut {
			s.updateAnnouncement(w, r, id)
		} else {
			methodNotAllowed(w, http.MethodPatch, http.MethodPut, http.MethodDelete)
		}
	default:
		writeError(w, apiErr(404, "not_found", "endpoint not found"))
	}
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var input LoginRequest
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, apiErr(400, "invalid_input", "username and password are required"))
		return
	}
	identifier := first(input.Username, input.Account)
	if identifier == "" || input.Password == "" {
		writeError(w, apiErr(400, "invalid_input", "username and password are required"))
		return
	}
	if len([]byte(identifier)) > maxLoginIdentifierBytes || strings.ContainsAny(identifier, "\r\n") {
		writeError(w, apiErr(http.StatusBadRequest, "invalid_input", "username or email is too long"))
		return
	}
	// Check the shared client bucket first. Once an address is blocked, do not
	// allocate per-identifier buckets for the remaining spray attempts.
	ipKey := "ip\x00" + s.clientRateAddress(r)
	if retry, allowed := s.loginAllowed(ipKey); !allowed {
		writeLoginRateLimit(w, retry)
		return
	}
	rateKeys := []string{
		ipKey,
		s.loginAccountRateKey(identifier),
		s.loginRateKey(r, identifier),
	}
	for _, rateKey := range rateKeys[1:] {
		if retry, allowed := s.loginAllowed(rateKey); !allowed {
			writeLoginRateLimit(w, retry)
			return
		}
	}
	select {
	case s.passwordSlots <- struct{}{}:
		defer func() { <-s.passwordSlots }()
	default:
		w.Header().Set("Retry-After", "1")
		writeError(w, apiErr(http.StatusTooManyRequests, "login_busy", "sign-in service is busy; try again shortly"))
		return
	}
	user := s.store.authenticate(identifier, input.Password)
	if user == nil {
		for _, rateKey := range rateKeys {
			s.loginFailure(rateKey)
		}
		writeError(w, apiErr(401, "invalid_credentials", "username or password is incorrect"))
		return
	}
	for _, rateKey := range rateKeys {
		s.loginSuccess(rateKey)
	}
	token := randomID(32)
	s.sessionsMu.Lock()
	now := time.Now()
	for candidate, item := range s.sessions {
		if now.After(item.Expires) {
			delete(s.sessions, candidate)
		}
	}
	if len(s.sessions) >= maxSessions {
		// Evict the earliest-expiring session before accepting a new one.
		var oldestToken string
		var oldest time.Time
		for candidate, item := range s.sessions {
			if oldestToken == "" || item.Expires.Before(oldest) {
				oldestToken, oldest = candidate, item.Expires
			}
		}
		if oldestToken != "" {
			delete(s.sessions, oldestToken)
		}
	}
	s.sessions[token] = session{UserID: user.ID, Expires: time.Now().Add(sessionTTL)}
	s.sessionsMu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: s.cookieSecure || r.TLS != nil, SameSite: http.SameSiteLaxMode, MaxAge: int(sessionTTL.Seconds()), Expires: time.Now().Add(sessionTTL)})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "user": s.store.publicUser(user)})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		s.sessionsMu.Lock()
		delete(s.sessions, cookie.Value)
		s.sessionsMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: s.cookieSecure || r.TLS != nil, SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(1, 0)})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	user := s.currentUser(r)
	if user == nil {
		writeError(w, apiErr(http.StatusUnauthorized, "authentication_required", "please log in first"))
		return
	}
	var input PasswordChangeInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, apiErr(http.StatusBadRequest, "invalid_input", "current and new passwords are required"))
		return
	}
	currentPassword := input.CurrentPassword
	if currentPassword == "" {
		currentPassword = input.Current
	}
	newPassword := input.NewPassword
	if newPassword == "" {
		newPassword = input.Password
	}
	if strings.TrimSpace(currentPassword) == "" || strings.TrimSpace(newPassword) == "" {
		writeError(w, apiErr(http.StatusBadRequest, "invalid_input", "current and new passwords are required"))
		return
	}
	if input.ConfirmPassword != "" && input.ConfirmPassword != newPassword {
		writeError(w, apiErr(http.StatusBadRequest, "password_confirmation_mismatch", "password confirmation does not match"))
		return
	}
	updated, apiError := s.store.changeStudentPassword(user.ID, currentPassword, newPassword)
	if apiError != nil {
		writeError(w, apiError)
		return
	}
	// Keep the current request usable while revoking every other session for
	// the account after a credential rotation.
	currentToken := ""
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		currentToken = cookie.Value
	}
	s.sessionsMu.Lock()
	for token, item := range s.sessions {
		if item.UserID == updated.ID && token != currentToken {
			delete(s.sessions, token)
		}
	}
	s.sessionsMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "user": s.store.publicUser(updated)})
}

func (s *Server) listEnrollments(w http.ResponseWriter, r *http.Request) {
	user := s.currentUser(r)
	target := queryStudent(r, user)
	if user.Role != "admin" && target != user.ID {
		writeError(w, apiErr(403, "forbidden", "students may only view their own enrollments"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "enrollments": s.store.enrollmentsFor(target)})
}

func (s *Server) changeEnrollment(w http.ResponseWriter, r *http.Request) {
	user := s.currentUser(r)
	if user.Role != "student" {
		writeError(w, apiErr(403, "student_required", "only students may change enrollments"))
		return
	}
	var input EnrollmentRequest
	if err := decodeJSON(w, r, &input); err != nil || strings.TrimSpace(input.CourseID) == "" {
		writeError(w, apiErr(400, "invalid_input", "courseId is required"))
		return
	}
	enrollment, err := s.store.changeEnrollment(user.ID, strings.TrimSpace(input.CourseID), input.Action)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "enrollment": enrollment})
}

func (s *Server) listGrades(w http.ResponseWriter, r *http.Request) {
	user := s.currentUser(r)
	target := queryStudent(r, user)
	includeUnpublished := user.Role == "admin"
	if user.Role == "admin" {
		if hasStudentQuery(r) {
			target = s.store.studentRecordID(target)
			if target == "" {
				writeError(w, apiErr(http.StatusNotFound, "student_not_found", "student not found"))
				return
			}
		} else {
			// An administrator without a student filter is asking for the
			// registrar-wide view, not grades belonging to the admin account.
			target = ""
		}
	} else if target != user.ID {
		writeError(w, apiErr(403, "forbidden", "students may only view their own grades"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "grades": s.store.gradesFor(target, includeUnpublished)})
}

func (s *Server) listSchedule(w http.ResponseWriter, r *http.Request) {
	user := s.currentUser(r)
	target := queryStudent(r, user)
	if user.Role == "admin" && hasStudentQuery(r) {
		target = s.store.studentRecordID(target)
		if target == "" {
			writeError(w, apiErr(http.StatusNotFound, "student_not_found", "student not found"))
			return
		}
	}
	if user.Role != "admin" && target != user.ID {
		writeError(w, apiErr(403, "forbidden", "students may only view their own schedule"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "schedule": s.store.scheduleFor(target, user.Role == "admin" && !hasStudentQuery(r))})
}

func (s *Server) announcementByID(w http.ResponseWriter, r *http.Request, id string) {
	for _, item := range s.store.announcementsFor(s.currentUser(r), false) {
		if strings.EqualFold(item.ID, id) {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "announcement": item})
			return
		}
	}
	writeError(w, apiErr(404, "announcement_not_found", "announcement not found"))
}

func (s *Server) downloadCatalog(w http.ResponseWriter, r *http.Request) {
	locale := r.URL.Query().Get("lang")
	if locale == "" {
		locale = r.Header.Get("Accept-Language")
		if strings.HasPrefix(strings.ToLower(locale), "en") {
			locale = "en"
		}
	}
	content, err := s.store.catalogCSV(locale)
	if err != nil {
		writeError(w, apiErr(http.StatusInternalServerError, "catalog_unavailable", "course catalog could not be generated"))
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="cgu-course-catalog.csv"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func (s *Server) downloadTranscript(w http.ResponseWriter, r *http.Request) {
	user := s.currentUser(r)
	if user == nil {
		writeError(w, apiErr(http.StatusUnauthorized, "authentication_required", "please log in first"))
		return
	}
	target := user.ID
	if user.Role == "admin" {
		ref := strings.TrimSpace(first(r.URL.Query().Get("student_id"), r.URL.Query().Get("user_id")))
		if ref == "" {
			writeError(w, apiErr(http.StatusBadRequest, "student_required", "student_id is required for an administrator transcript"))
			return
		}
		target = s.store.studentRecordID(ref)
		if target == "" {
			writeError(w, apiErr(http.StatusNotFound, "student_not_found", "student not found"))
			return
		}
	}
	content, err := s.store.transcriptCSV(target, r.URL.Query().Get("lang"))
	if err != nil {
		writeError(w, apiErr(http.StatusNotFound, "transcript_not_found", "transcript is not available"))
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="cgu-transcript.csv"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func (s *Server) createCourse(w http.ResponseWriter, r *http.Request) {
	var input CourseInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, apiErr(400, "invalid_input", "course is required"))
		return
	}
	item, err := s.store.createCourse(input)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Location", "/api/courses/"+url.PathEscape(item.ID))
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "course": item})
}

func (s *Server) updateCourse(w http.ResponseWriter, r *http.Request, id string) {
	var input CourseInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, apiErr(400, "invalid_input", "course is required"))
		return
	}
	item, err := s.store.updateCourse(id, input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "course": item})
}

func (s *Server) createStudent(w http.ResponseWriter, r *http.Request) {
	var input StudentInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, apiErr(http.StatusBadRequest, "invalid_input", "student account is required"))
		return
	}
	item, apiError := s.store.createStudent(input)
	if apiError != nil {
		writeError(w, apiError)
		return
	}
	w.Header().Set("Location", "/api/admin/students/"+url.PathEscape(item.ID))
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "student": item})
}

func (s *Server) updateStudent(w http.ResponseWriter, r *http.Request, id string) {
	var input StudentInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, apiErr(http.StatusBadRequest, "invalid_input", "student update is required"))
		return
	}
	item, apiError := s.store.updateStudent(id, input)
	if apiError != nil {
		writeError(w, apiError)
		return
	}
	if item != nil && !item.Active {
		s.revokeUserSessions(item.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "student": item})
}

func (s *Server) revokeUserSessions(userID string) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	for token, item := range s.sessions {
		if item.UserID == userID {
			delete(s.sessions, token)
		}
	}
}

func (s *Server) listAdminGrades(w http.ResponseWriter, r *http.Request) {
	studentRef := strings.TrimSpace(first(r.URL.Query().Get("student_id"), r.URL.Query().Get("user_id")))
	if studentRef != "" {
		studentID := s.store.studentRecordID(studentRef)
		if studentID == "" {
			writeError(w, apiErr(http.StatusNotFound, "student_not_found", "student not found"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "grades": s.store.gradesFor(studentID, true)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "grades": s.store.gradesFor("", true)})
}

func (s *Server) createGrade(w http.ResponseWriter, r *http.Request) {
	var input GradeInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, apiErr(http.StatusBadRequest, "invalid_input", "grade is required"))
		return
	}
	item, apiError := s.store.createGrade(input)
	if apiError != nil {
		writeError(w, apiError)
		return
	}
	w.Header().Set("Location", "/api/admin/grades/"+url.PathEscape(item.ID))
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "grade": item})
}

func (s *Server) updateGrade(w http.ResponseWriter, r *http.Request, id string) {
	var input GradeInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, apiErr(http.StatusBadRequest, "invalid_input", "grade update is required"))
		return
	}
	item, apiError := s.store.updateGrade(id, input)
	if apiError != nil {
		writeError(w, apiError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "grade": item})
}

func (s *Server) deleteGrade(w http.ResponseWriter, id string) {
	item, apiError := s.store.deleteGrade(id)
	if apiError != nil {
		writeError(w, apiError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "grade": item})
}

func (s *Server) listAdminSchedule(w http.ResponseWriter, r *http.Request) {
	studentRef := strings.TrimSpace(first(r.URL.Query().Get("student_id"), r.URL.Query().Get("user_id")))
	if studentRef != "" {
		studentID := s.store.studentRecordID(studentRef)
		if studentID == "" {
			writeError(w, apiErr(http.StatusNotFound, "student_not_found", "student not found"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "schedule": s.store.scheduleFor(studentID, false)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "schedule": s.store.scheduleFor("", true)})
}

func (s *Server) createSchedule(w http.ResponseWriter, r *http.Request) {
	var input ScheduleInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, apiErr(http.StatusBadRequest, "invalid_input", "schedule entry is required"))
		return
	}
	item, apiError := s.store.createSchedule(input)
	if apiError != nil {
		writeError(w, apiError)
		return
	}
	w.Header().Set("Location", "/api/admin/schedule/"+url.PathEscape(item.ID))
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "schedule": item})
}

func (s *Server) updateSchedule(w http.ResponseWriter, r *http.Request, id string) {
	var input ScheduleInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, apiErr(http.StatusBadRequest, "invalid_input", "schedule update is required"))
		return
	}
	item, apiError := s.store.updateSchedule(id, input)
	if apiError != nil {
		writeError(w, apiError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "schedule": item})
}

func (s *Server) deleteSchedule(w http.ResponseWriter, id string) {
	item, apiError := s.store.deleteSchedule(id)
	if apiError != nil {
		writeError(w, apiError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "schedule": item})
}

func (s *Server) deleteCourse(w http.ResponseWriter, id string) {
	item, err := s.store.deleteCourse(id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "course": item})
}

func (s *Server) createAnnouncement(w http.ResponseWriter, r *http.Request) {
	var input AnnouncementInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, apiErr(400, "invalid_input", "announcement is required"))
		return
	}
	item, err := s.store.createAnnouncement(input)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Location", "/api/announcements/"+url.PathEscape(item.ID))
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "announcement": item})
}

func (s *Server) updateAnnouncement(w http.ResponseWriter, r *http.Request, id string) {
	var input AnnouncementInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, apiErr(400, "invalid_input", "announcement is required"))
		return
	}
	item, err := s.store.updateAnnouncement(id, input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "announcement": item})
}

func (s *Server) deleteAnnouncement(w http.ResponseWriter, id string) {
	item, err := s.store.deleteAnnouncement(id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "announcement": item})
}

func (s *Server) updateSiteContent(w http.ResponseWriter, r *http.Request) {
	var input SiteContentInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, apiErr(400, "invalid_input", "content key and values are required"))
		return
	}
	item, apiError := s.store.updateSiteContent(input)
	if apiError != nil {
		writeError(w, apiError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "content": item})
}

func (s *Server) submitAdmission(w http.ResponseWriter, r *http.Request) {
	var input AdmissionApplicationInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, apiErr(http.StatusBadRequest, "invalid_input", "name, email, and school are required"))
		return
	}
	if retry, allowed := s.admissionAllowed(s.admissionRateKey(r)); !allowed {
		seconds := int(retry / time.Second)
		if retry%time.Second != 0 {
			seconds++
		}
		if seconds < 1 {
			seconds = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		writeError(w, apiErr(http.StatusTooManyRequests, "admission_rate_limited", "too many applications from this client; try again later"))
		return
	}
	item, apiError := s.store.createAdmission(input)
	if apiError != nil {
		writeError(w, apiError)
		return
	}
	w.Header().Set("Location", "/api/admissions/"+url.PathEscape(item.ID))
	// Do not echo contact details from a public endpoint; the authenticated
	// administrator list is the only API that exposes applicant PII.
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "application": map[string]any{
		"id": item.ID, "status": item.Status, "createdAt": item.CreatedAt,
	}})
}

func (s *Server) updateAdmission(w http.ResponseWriter, r *http.Request, id string) {
	var input AdmissionApplicationInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, apiErr(http.StatusBadRequest, "invalid_input", "application update is required"))
		return
	}
	item, apiError := s.store.updateAdmission(id, input)
	if apiError != nil {
		writeError(w, apiError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "application": item})
}

func (s *Server) approveAdmission(w http.ResponseWriter, r *http.Request, id string) {
	admin := s.currentUser(r)
	if admin == nil || admin.Role != "admin" {
		writeError(w, apiErr(http.StatusForbidden, "admin_required", "administrator role is required"))
		return
	}
	result, apiError := s.store.approveAdmission(id, admin.ID)
	if apiError != nil {
		writeError(w, apiError)
		return
	}
	if !result.AlreadyApproved {
		s.queueAdmissionNotice(result)
	}
	status := http.StatusCreated
	if result.AlreadyApproved {
		status = http.StatusOK
	}
	w.Header().Set("Location", "/api/admin/students/"+url.PathEscape(result.Student.ID))
	payload := map[string]any{
		"ok":              true,
		"application":     result.Application,
		"student":         result.Student,
		"alreadyApproved": result.AlreadyApproved,
	}
	if result.InitialPassword != "" {
		// This key is intentionally absent on replay, rather than present with
		// an empty value, so clients cannot mistake a retry for a credential
		// delivery.
		payload["initialPassword"] = result.InitialPassword
	}
	if result.MailboxID != "" {
		payload["mailboxId"] = result.MailboxID
		payload["deliveryStatus"] = result.DeliveryStatus
		if result.DeliveryError != "" {
			payload["deliveryError"] = result.DeliveryError
		}
	}
	writeJSON(w, status, payload)
}

// queueAdmissionNotice keeps approval latency independent from a remote SMTP
// relay. The durable approval transaction has already committed, so a real
// configured SMTP sender can finish on its own bounded background context;
// administrators see the initial pending state and the mailbox record is
// refreshed by the normal admin polling/reload path. The no-SMTP path is kept
// synchronous because it only records the local, deterministic status.
func (s *Server) queueAdmissionNotice(result *AdmissionApproval) {
	if result == nil || strings.TrimSpace(result.MailboxID) == "" {
		return
	}
	if _, realSMTP := s.mailer.(*SMTPMailer); realSMTP {
		queued := *result
		go s.deliverAdmissionNotice(context.Background(), &queued)
		return
	}
	s.deliverAdmissionNotice(context.Background(), result)
}

// deliverAdmissionNotice attempts the queued applicant notification only
// after the account transaction has committed. A transport failure never
// rolls back admission; the durable mailbox record carries the state into the
// existing administrator retry workflow.
func (s *Server) deliverAdmissionNotice(ctx context.Context, result *AdmissionApproval) {
	if result == nil || strings.TrimSpace(result.MailboxID) == "" {
		return
	}
	setDelivery := func(status, deliveryError string) {
		result.DeliveryStatus = status
		result.DeliveryError = deliveryError
		if result.Application.ID != "" {
			result.Application.DeliveryStatus = status
			result.Application.DeliveryError = deliveryError
		}
	}
	item, apiError := s.store.beginMailboxDelivery(result.MailboxID)
	if apiError != nil {
		setDelivery(mailboxDeliveryPending, apiError.Message)
		return
	}
	status, deliveryError, deliveredAt := s.deliverMailboxExternally(ctx, item)
	updated, statusError := s.store.updateMailboxDelivery(item.ID, item.DeliveryStartedAt, status, deliveryError, deliveredAt)
	if statusError == nil && updated != nil {
		setDelivery(updated.DeliveryStatus, updated.DeliveryError)
		return
	}
	// The relay may already have accepted the message. Keep the outcome
	// conservative and expose a retryable record rather than claiming failure.
	unknown, _ := s.store.markMailboxDeliveryUnknown(item.ID, "SMTP outcome unknown; delivery status could not be saved", item.DeliveryStartedAt)
	if unknown != nil {
		setDelivery(unknown.DeliveryStatus, unknown.DeliveryError)
		return
	}
	setDelivery(mailboxDeliveryUnknown, "SMTP outcome unknown; delivery status could not be saved")
}

func (s *Server) deleteAdmission(w http.ResponseWriter, id string) {
	item, apiError := s.store.deleteAdmission(id)
	if apiError != nil {
		writeError(w, apiError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "application": item})
}

func (s *Server) listAdminNotifications(w http.ResponseWriter, r *http.Request) {
	user := s.currentUser(r)
	if user == nil || user.Role != "admin" {
		writeError(w, apiErr(http.StatusForbidden, "admin_required", "administrator role is required"))
		return
	}
	notifications, unread := s.store.adminNotificationsFor(user.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "unread": unread, "notifications": notifications})
}

func (s *Server) markAdminNotificationRead(w http.ResponseWriter, r *http.Request, id string) {
	user := s.currentUser(r)
	if user == nil || user.Role != "admin" {
		writeError(w, apiErr(http.StatusForbidden, "admin_required", "administrator role is required"))
		return
	}
	var input AdminNotificationReadInput
	if err := decodeJSON(w, r, &input); err != nil || input.Read == nil {
		writeError(w, apiErr(http.StatusBadRequest, "invalid_input", "read must be a boolean"))
		return
	}
	item, apiError := s.store.markAdminNotificationRead(user.ID, id, *input.Read)
	if apiError != nil {
		writeError(w, apiError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "notification": item})
}

func (s *Server) currentUser(r *http.Request) *User {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		return nil
	}
	s.sessionsMu.Lock()
	item, ok := s.sessions[cookie.Value]
	if ok && time.Now().After(item.Expires) {
		delete(s.sessions, cookie.Value)
		ok = false
	}
	s.sessionsMu.Unlock()
	if !ok {
		return nil
	}
	user := s.store.user(item.UserID)
	if user == nil || user.Disabled {
		if ok {
			s.sessionsMu.Lock()
			delete(s.sessions, cookie.Value)
			s.sessionsMu.Unlock()
		}
		return nil
	}
	return user
}

func (s *Server) requireAuth(w http.ResponseWriter, r *http.Request) bool {
	if s.currentUser(r) == nil {
		writeError(w, apiErr(401, "authentication_required", "please log in first"))
		return false
	}
	return true
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	user := s.currentUser(r)
	if user == nil {
		writeError(w, apiErr(401, "authentication_required", "please log in first"))
		return false
	}
	if user.Role != "admin" {
		writeError(w, apiErr(403, "admin_required", "administrator role is required"))
		return false
	}
	return true
}

func (s *Server) originAllowed(r *http.Request) bool {
	if r == nil || r.URL == nil || !strings.HasPrefix(r.URL.Path, "/api") {
		return true
	}
	if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodPatch && r.Method != http.MethodDelete && r.Method != http.MethodOptions {
		return true
	}
	origin, ok := normalizeOrigin(r.Header.Get("Origin"))
	if strings.TrimSpace(r.Header.Get("Origin")) == "" {
		return true
	}
	if !ok {
		return false
	}
	if s.publicOrigin != "" {
		return strings.EqualFold(origin, s.publicOrigin)
	}
	// A direct HTTP/HTTPS listener can infer its own scheme. When TLS is
	// terminated by a reverse proxy, operators must configure publicOrigin;
	// never trust arbitrary forwarded headers for this security decision.
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host, ok := normalizeRequestHost(r.Host, scheme)
	if !ok {
		return false
	}
	return strings.EqualFold(origin, scheme+"://"+host)
}

// normalizeOrigin canonicalizes the small subset of origins accepted by the
// same-origin policy. Paths, credentials, queries, fragments, and non-web
// schemes are rejected so configuration and request headers cannot broaden the
// allow-list unexpectedly.
func normalizeOrigin(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\r\n") {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Opaque != "" || u.Host == "" || u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	hostname := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if hostname == "" || strings.ContainsAny(hostname, "\r\n/@") {
		return "", false
	}
	port := u.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	host := hostname
	if strings.Contains(hostname, ":") {
		host = "[" + hostname + "]"
	}
	if port != "" {
		host += ":" + port
	}
	return scheme + "://" + host, true
}

func normalizeRequestHost(raw, scheme string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\r\n") {
		return "", false
	}
	// Parse the host with a neutral scheme, then remove only the default ports
	// for either web scheme so Host cgu.example:443 matches HTTPS origins that
	// omit the default port.
	u, err := url.Parse("http://" + raw)
	if err != nil || u.User != nil || u.Opaque != "" || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	hostname := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if hostname == "" || strings.ContainsAny(hostname, "\r\n/@") {
		return "", false
	}
	port := u.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	host := hostname
	if strings.Contains(hostname, ":") {
		host = "[" + hostname + "]"
	}
	if port != "" {
		host += ":" + port
	}
	return host, true
}

func stateChangingAPIRequest(r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/api") {
		return false
	}
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func (s *Server) loginRateKey(r *http.Request, identifier string) string {
	return s.clientRateAddress(r) + "\x00" + strings.ToLower(strings.TrimSpace(identifier))
}

func (s *Server) loginAccountRateKey(identifier string) string {
	return "account\x00" + strings.ToLower(strings.TrimSpace(identifier))
}

func writeLoginRateLimit(w http.ResponseWriter, retry time.Duration) {
	seconds := int(retry / time.Second)
	if retry%time.Second != 0 {
		seconds++
	}
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeError(w, apiErr(http.StatusTooManyRequests, "login_rate_limited", "too many failed sign-in attempts; try again later"))
}

func (s *Server) loginAllowed(key string) (time.Duration, bool) {
	now := time.Now()
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	s.pruneLoginAttemptsLocked(now)
	item := s.loginAttempts[key]
	if item == nil {
		if len(s.loginAttempts) >= loginMaxKeys {
			// Keep the limiter bounded even when an attacker sprays identifiers.
			var oldestKey string
			var oldest time.Time
			for candidate, attempt := range s.loginAttempts {
				if oldestKey == "" || attempt.windowFrom.Before(oldest) {
					oldestKey, oldest = candidate, attempt.windowFrom
				}
			}
			if oldestKey != "" {
				delete(s.loginAttempts, oldestKey)
			}
		}
		item = &loginAttempt{windowFrom: now}
		s.loginAttempts[key] = item
	}
	if now.Before(item.blockedTo) {
		return time.Until(item.blockedTo), false
	}
	return 0, true
}

func (s *Server) loginFailure(key string) {
	now := time.Now()
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	item := s.loginAttempts[key]
	if item == nil || now.Sub(item.windowFrom) >= loginWindow {
		if item == nil && len(s.loginAttempts) >= loginMaxKeys {
			// Keep failure-path allocation bounded too; loginAllowed normally
			// creates the key first, but direct callers/tests should be safe.
			var oldestKey string
			var oldest time.Time
			for candidate, attempt := range s.loginAttempts {
				if oldestKey == "" || attempt.windowFrom.Before(oldest) {
					oldestKey, oldest = candidate, attempt.windowFrom
				}
			}
			if oldestKey != "" {
				delete(s.loginAttempts, oldestKey)
			}
		}
		item = &loginAttempt{windowFrom: now}
		s.loginAttempts[key] = item
	}
	item.failures++
	if item.failures >= loginMaxFails {
		item.blockedTo = now.Add(loginBlock)
	}
}

func (s *Server) loginSuccess(key string) {
	s.loginMu.Lock()
	delete(s.loginAttempts, key)
	s.loginMu.Unlock()
}

func (s *Server) pruneLoginAttemptsLocked(now time.Time) {
	for key, item := range s.loginAttempts {
		if now.Sub(item.windowFrom) >= loginWindow && now.After(item.blockedTo) {
			delete(s.loginAttempts, key)
		}
	}
}

func (s *Server) admissionAllowed(key string) (time.Duration, bool) {
	now := time.Now()
	s.admissionMu.Lock()
	defer s.admissionMu.Unlock()
	for candidate, item := range s.admissionAttempts {
		if item == nil || now.Sub(item.windowFrom) >= admissionWindow {
			delete(s.admissionAttempts, candidate)
		}
	}
	item := s.admissionAttempts[key]
	if item == nil {
		if len(s.admissionAttempts) >= admissionMaxKeys {
			var oldestKey string
			var oldest time.Time
			for candidate, attempt := range s.admissionAttempts {
				if oldestKey == "" || attempt.windowFrom.Before(oldest) {
					oldestKey, oldest = candidate, attempt.windowFrom
				}
			}
			if oldestKey != "" {
				delete(s.admissionAttempts, oldestKey)
			}
		}
		item = &admissionAttempt{windowFrom: now}
		s.admissionAttempts[key] = item
	}
	if item.count >= admissionMax {
		return time.Until(item.windowFrom.Add(admissionWindow)), false
	}
	item.count++
	return 0, true
}

func (s *Server) admissionRateKey(r *http.Request) string {
	return s.clientRateAddress(r)
}

func (s *Server) clientRateAddress(r *http.Request) string {
	if r == nil {
		return "unknown"
	}
	remote, ok := parseRateAddress(r.RemoteAddr)
	if !ok {
		return "unknown"
	}
	if !s.isTrustedProxy(remote) {
		return remote.String()
	}
	forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if forwarded == "" {
		return remote.String()
	}
	parts := strings.Split(forwarded, ",")
	candidate := remote
	for i := len(parts) - 1; i >= 0; i-- {
		address, valid := parseRateAddress(parts[i])
		if !valid {
			return remote.String()
		}
		candidate = address
		if !s.isTrustedProxy(address) {
			return address.String()
		}
	}
	return candidate.String()
}

func (s *Server) isTrustedProxy(address netip.Addr) bool {
	address = address.Unmap()
	for _, prefix := range s.trustedProxies {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func parseRateAddress(raw string) (netip.Addr, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return netip.Addr{}, false
	}
	if addressPort, err := netip.ParseAddrPort(raw); err == nil {
		return addressPort.Addr().Unmap(), true
	}
	address, err := netip.ParseAddr(strings.Trim(raw, "[]"))
	if err != nil {
		return netip.Addr{}, false
	}
	return address.Unmap(), true
}

func (s *Server) handleOptions(w http.ResponseWriter, r *http.Request) {
	origin, originOK := normalizeOrigin(r.Header.Get("Origin"))
	if originOK && s.originAllowed(&http.Request{Method: http.MethodPost, URL: r.URL, Host: r.Host, Header: r.Header, TLS: r.TLS}) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-CGU-Request")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	clean := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
	relative := strings.TrimPrefix(clean, "/")
	switch relative {
	case "":
		relative = "index.html"
	case "about", "programs", "admissions", "campus-life", "news", "contact":
		// Public information architecture aliases keep university sections
		// addressable and share the managed homepage source of truth.
		relative = "index.html"
	case "login":
		relative = "login.html"
	case "portal":
		relative = "portal.html"
	case "admin":
		relative = "admin.html"
	case "calendar":
		relative = "calendar.html"
	case "catalog":
		relative = "catalog.html"
	}
	if sensitiveStaticPath(relative) {
		http.NotFound(w, r)
		return
	}
	root, err := filepath.Abs(s.staticDir)
	if err != nil {
		http.Error(w, "static files unavailable", 500)
		return
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	candidate := filepath.Join(root, filepath.FromSlash(relative))
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		http.NotFound(w, r)
		return
	}
	if info, statErr := os.Stat(candidate); statErr != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	resolvedCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	resolvedRel, err := filepath.Rel(resolvedRoot, resolvedCandidate)
	if err != nil || resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(os.PathSeparator)) {
		http.NotFound(w, r)
		return
	}
	switch strings.ToLower(filepath.Ext(candidate)) {
	case ".html", ".js", ".css":
		// Revalidate executable UI assets so a deployment cannot leave new HTML
		// paired with an older cached script.
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	}
	http.ServeFile(w, r, resolvedCandidate)
}

// sensitiveStaticPath prevents an accidental CGU_STATIC_DIR pointing at the
// repository or deployment directory from turning secrets and source files
// into public downloads. The normal web directory contains only allow-listed
// browser assets and never needs these names or extensions.
func sensitiveStaticPath(relative string) bool {
	clean := path.Clean("/" + strings.TrimPrefix(relative, "/"))
	for _, segment := range strings.Split(strings.TrimPrefix(clean, "/"), "/") {
		if segment == "" || strings.HasPrefix(segment, ".") {
			return true
		}
	}
	base := strings.ToLower(filepath.Base(clean))
	ext := strings.ToLower(filepath.Ext(base))
	if base == "config.json" || base == "config.yaml" || base == "config.yml" || base == "config.toml" || base == ".env" || base == "go.mod" || base == "go.sum" {
		return true
	}
	switch ext {
	case ".env", ".pem", ".key", ".crt", ".cer", ".p12", ".pfx", ".sql", ".db", ".sqlite", ".sqlite3", ".go", ".sum", ".mod", ".json", ".yaml", ".yml", ".toml", ".ps1", ".sh", ".bat", ".exe", ".dll":
		return true
	default:
		return false
	}
}

func queryStudent(r *http.Request, user *User) string {
	if value := r.URL.Query().Get("student_id"); value != "" {
		return value
	}
	if value := r.URL.Query().Get("user_id"); value != "" {
		return value
	}
	return user.ID
}

func hasStudentQuery(r *http.Request) bool {
	return r.URL.Query().Get("student_id") != "" || r.URL.Query().Get("user_id") != ""
}

func decodePathID(raw string) string {
	value, err := url.PathUnescape(strings.Trim(raw, "/"))
	if err != nil {
		return ""
	}
	return value
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	if r.Body == nil {
		return io.EOF
	}
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, bodyLimit))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("request body must contain one JSON value")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, err *apiError) {
	writeJSON(w, err.Status, map[string]any{"ok": false, "error": err.Code, "message": err.Message})
}

func methodNotAllowed(w http.ResponseWriter, methods ...string) {
	w.Header().Set("Allow", strings.Join(methods, ", "))
	writeError(w, apiErr(405, "method_not_allowed", "method not allowed"))
}

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'self'; object-src 'none'; frame-ancestors 'self'; form-action 'self'; script-src 'self'; style-src 'self'; img-src 'self' https://images.unsplash.com data:; font-src 'self' data:; connect-src 'self'; frame-src 'none'")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
}

func randomID(bytes int) string {
	if bytes < 16 {
		bytes = 16
	}
	buffer := make([]byte, bytes)
	if _, err := rand.Read(buffer); err != nil {
		panic("crypto/rand unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(buffer)
}

func hashPassword(password string) string {
	hash, err := hashPasswordChecked(password)
	if err != nil {
		return ""
	}
	return hash
}

func hashPasswordChecked(password string) (string, error) {
	if len([]byte(password)) > maxPasswordBytes {
		return "", errors.New("password exceeds bcrypt's 72-byte limit")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return "bcrypt$" + string(hash), nil
}

func verifyPassword(password, encoded string) bool {
	if strings.HasPrefix(encoded, "bcrypt$") {
		return bcrypt.CompareHashAndPassword([]byte(strings.TrimPrefix(encoded, "bcrypt$")), []byte(password)) == nil
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 1 || iterations > 2_000_000 {
		return false
	}
	salt, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	expected, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil || len(expected) == 0 {
		return false
	}
	actual := pbkdf2SHA256([]byte(password), salt, iterations, len(expected))
	return hmac.Equal(actual, expected)
}

// pbkdf2SHA256 is the RFC 8018 PBKDF2 construction kept here to avoid an
// external x/crypto dependency in the single-binary service.
func pbkdf2SHA256(password, salt []byte, iterations, keyLength int) []byte {
	if iterations < 1 || keyLength < 1 {
		return nil
	}
	hashSize := sha256.Size
	blocks := (keyLength + hashSize - 1) / hashSize
	result := make([]byte, 0, blocks*hashSize)
	for block := 1; block <= blocks; block++ {
		mac := hmac.New(sha256.New, password)
		mac.Write(salt)
		var counter [4]byte
		binary.BigEndian.PutUint32(counter[:], uint32(block))
		mac.Write(counter[:])
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < iterations; i++ {
			mac = hmac.New(sha256.New, password)
			mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		result = append(result, t...)
	}
	return result[:keyLength]
}

func (s *Store) seed() {
	s.courses = append(s.courses,
		&Course{ID: "cgu-elements-101", Code: "ELM101", NameZh: "元素力与世界观", NameEn: "Elements & Worldview", Department: "元素科学学院", DepartmentEn: "School of Elemental Sciences", Teacher: "丽莎", TeacherEn: "Lisa", Credits: 3, Description: "从七元素的基本性质出发，建立理解提瓦特世界的第一套方法。", DescriptionEn: "Study the fundamental properties of the seven elements and build a framework for understanding Teyvat.", Capacity: 60, Type: "required", Term: "2026-秋", TermEn: "Autumn 2026"},
		&Course{ID: "cgu-nature-202", Code: "NAT202", NameZh: "提瓦特自然地理", NameEn: "Teyvat Physical Geography", Department: "地脉与自然学院", DepartmentEn: "School of Ley Lines and Nature", Teacher: "钟离", TeacherEn: "Zhongli", Credits: 4, Description: "沿着地脉、山海与遗迹，练习用田野调查读懂一片土地。", DescriptionEn: "Read a landscape through fieldwork across ley lines, mountains, seas, and ruins.", Capacity: 45, Type: "required", Term: "2026-秋", TermEn: "Autumn 2026"},
		&Course{ID: "cgu-mondstadt-210", Code: "MUS210", NameZh: "风之诗与文化记忆", NameEn: "Songs of Wind & Cultural Memory", Department: "人文与艺术学院", DepartmentEn: "School of Humanities and Arts", Teacher: "温迪", TeacherEn: "Venti", Credits: 3, Description: "以民谣、诗歌和城市记忆为线索，研究文化如何被传唱。", DescriptionEn: "Trace how culture travels through folk songs, poetry, and the memories of a city.", Capacity: 50, Type: "elective", Term: "2026-秋", TermEn: "Autumn 2026"},
		&Course{ID: "cgu-adventure-301", Code: "ADV301", NameZh: "冒险实践与团队协作", NameEn: "Adventure Practice & Teamwork", Department: "冒险实践学院", DepartmentEn: "School of Adventure Practice", Teacher: "凯瑟琳", TeacherEn: "Katheryne", Credits: 2, Description: "把风险评估、路线规划与可靠的伙伴关系带进真实任务。", DescriptionEn: "Apply risk assessment, route planning, and trusted partnerships to real missions.", Capacity: 35, Type: "elective", Term: "2026-秋", TermEn: "Autumn 2026"},
		&Course{ID: "cgu-fontaine-310", Code: "FON310", NameZh: "审判与机械文明", NameEn: "Judgment & Mechanical Civilization", Department: "枫丹法政与工程学院", DepartmentEn: "School of Fontaine Law and Engineering", Teacher: "那维莱特", TeacherEn: "Neuvillette", Credits: 4, Description: "从水之国的法庭与工坊，研究规则、能源与机械创造。", DescriptionEn: "Examine rules, energy, and mechanical invention through Fontaine's courts and workshops.", Capacity: 40, Type: "required", Term: "2026-秋", TermEn: "Autumn 2026"},
		&Course{ID: "cgu-natlan-220", Code: "NAT220", NameZh: "火与竞技生态", NameEn: "Fire & Competitive Ecology", Department: "纳塔田野与竞技学院", DepartmentEn: "School of Natlan Fieldwork and Competition", Teacher: "教务联合授课", TeacherEn: "Registrar Faculty Team", Credits: 3, Description: "在部族、仪式与竞技场之间，完成一场尊重当地知识的田野研究。", DescriptionEn: "Conduct a field study that respects local knowledge across tribes, rituals, and arenas.", Capacity: 36, Type: "elective", Term: "2026-秋", TermEn: "Autumn 2026"},
		&Course{ID: "cgu-snezhnaya-401", Code: "SNE401", NameZh: "至冬研究与极地治理", NameEn: "Snezhnaya Studies & Polar Governance", Department: "至冬与极地研究学院", DepartmentEn: "School of Snezhnaya and Polar Studies", Teacher: "教务联合授课", TeacherEn: "Registrar Faculty Team", Credits: 4, Description: "以 7.0「无神怜爱的雪国」为新起点，研究冰原社会、风险与远行伦理。", DescriptionEn: "Study polar societies, risk, and travel ethics from Version 7.0's new Snezhnaya setting.", Capacity: 32, Type: "elective", Term: "2026-秋", TermEn: "Autumn 2026"},
	)
	s.announcements = append(s.announcements,
		&Announcement{ID: "announcement-welcome", TitleZh: "2026 秋季学期报到安排", TitleEn: "Autumn 2026 arrival schedule", ContentZh: "风之庭院将于 9 月 1 日开放报到，旅行者请携带录取确认函。", ContentEn: "Windrise Court opens on 1 September. Bring your admission confirmation.", Type: "ADMISSIONS", Audience: "all", PublishedAt: "2026-08-20T09:00:00Z", Published: true, Author: "admin"},
		&Announcement{ID: "announcement-enrollment", TitleZh: "选课周提醒", TitleEn: "Course selection week reminder", ContentZh: "学生门户将在 8 月 26 日 09:00 开放选课，请提前确认课表。", ContentEn: "Course selection opens at 09:00 on 26 August. Review your schedule first.", Type: "ACADEMICS", Audience: "student", PublishedAt: "2026-08-22T09:00:00Z", Published: true, Author: "admin"},
		&Announcement{ID: "announcement-snezhnaya-70", TitleZh: "至冬官方动态：凯旋与冰中余烬", TitleEn: "Snezhnaya official updates: “Triumphant Return” and “Embers Beneath the Ice”", ContentZh: "原神官方新闻于 2026 年 8 月 23 日发布「凯旋」与「冰中余烬」等至冬内容。CGU 延续 7.0 至冬研究与极地治理方向，并提供官方新闻入口。", ContentEn: "On 23 August 2026, the official Genshin news page published Snezhnaya stories including “Triumphant Return” and “Embers Beneath the Ice”. CGU continues its Version 7.0 Snezhnaya studies track and links to the official source.", Type: "WORLD_UPDATE", Audience: "all", PublishedAt: "2026-08-23T08:00:00+08:00", Published: true, Author: "admin"},
	)
}

func main() {
	cfg := LoadConfig()
	if strings.TrimSpace(cfg.AdminPassword) == "" {
		log.Fatal("administrator password is not configured; set adminPassword in config.json or CGU_ADMIN_PASSWORD")
	}
	if len([]byte(cfg.AdminPassword)) < 12 {
		log.Fatal("administrator password must contain at least 12 characters")
	}
	if len([]byte(cfg.AdminPassword)) > maxPasswordBytes {
		log.Fatal("administrator password must not exceed 72 bytes")
	}
	addr := cfg.Server.Address
	if !strings.Contains(addr, ":") {
		addr = ":" + addr
	}
	store := NewStoreWithAdminAndDomain(cfg.AdminUsername, cfg.AdminPassword, cfg.StudentEmailDomain)
	storageMode := "memory"
	var database *sql.DB
	if cfg.Database.Enabled {
		var databaseErr error
		database, databaseErr = openMySQL(cfg)
		if databaseErr != nil {
			if !cfg.Database.AllowMemoryFallback {
				log.Fatalf("MySQL is enabled but unavailable; refusing to start with writable memory fallback (%T); set CGU_DB_ALLOW_MEMORY_FALLBACK=true only for non-production recovery", databaseErr)
			}
			storageMode = "memory-fallback"
			log.Printf("MySQL unavailable; using in-memory fallback (%T)", databaseErr)
		} else if databaseErr = store.attachDatabase(database); databaseErr != nil {
			_ = database.Close()
			database = nil
			if !cfg.Database.AllowMemoryFallback {
				log.Fatalf("MySQL initialization failed; refusing to start with writable memory fallback (%T); set CGU_DB_ALLOW_MEMORY_FALLBACK=true only for non-production recovery", databaseErr)
			}
			storageMode = "memory-fallback"
			log.Printf("MySQL initialization failed; using in-memory fallback (%T)", databaseErr)
		} else {
			storageMode = "mysql"
		}
	}
	if database != nil {
		defer database.Close()
	}
	if cfg.SMTP.Enabled && storageMode != "mysql" {
		log.Fatal("SMTP external delivery requires a healthy MySQL store; refusing to start with memory fallback")
	}
	handler := NewServer(store, cfg.StaticDir)
	if cfg.SMTP.Enabled {
		mailer, mailerErr := NewSMTPMailer(cfg.SMTP)
		if mailerErr != nil {
			log.Fatalf("SMTP configuration is invalid: %v", mailerErr)
		}
		handler.setExternalMailSender(mailer)
		log.Printf("CGU SMTP external delivery enabled (host=%s port=%d tls=%s)", cfg.SMTP.Host, cfg.SMTP.Port, cfg.SMTP.TLSMode)
	}
	// LoadConfig already applies environment, .env, and config.json precedence.
	if err := handler.setTrustedProxies(cfg.TrustedProxies); err != nil {
		log.Fatalf("trusted proxy configuration is invalid: %v", err)
	}
	if strings.TrimSpace(cfg.PublicOrigin) != "" {
		publicOrigin, ok := normalizeOrigin(cfg.PublicOrigin)
		if !ok {
			log.Fatal("publicOrigin must be an http or https origin without a path, query, or fragment")
		}
		handler.publicOrigin = publicOrigin
		// A configured HTTPS origin means the browser must receive a Secure
		// session cookie even if cookieSecure was omitted from the config.
		handler.cookieSecure = cfg.CookieSecure || strings.HasPrefix(publicOrigin, "https://")
	} else {
		handler.cookieSecure = cfg.CookieSecure
	}
	handler.setStorageMode(storageMode)
	writeTimeout := 30 * time.Second
	if cfg.SMTP.Enabled && cfg.SMTP.TimeoutSecond > 0 {
		// Leave a small response window after the SMTP client deadline rather
		// than silently truncating a configured provider timeout.
		writeTimeout = time.Duration(cfg.SMTP.TimeoutSecond+5) * time.Second
	}
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: writeTimeout, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	log.Printf("CGU Go service listening on http://%s", addr)
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.ListenAndServe() }()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	case <-signals:
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("CGU shutdown warning: %v", err)
		}
	}
}

func isLoopbackListenAddress(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
