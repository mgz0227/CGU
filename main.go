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
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
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
	sessionCookie = "cgu_session"
	sessionTTL    = 8 * time.Hour
	bodyLimit     = 1 << 20
	loginWindow   = 5 * time.Minute
	loginBlock    = 15 * time.Minute
	loginMaxFails = 8
	loginMaxKeys  = 10_000
	maxSessions   = 50_000
	dummyBcrypt   = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
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
}

type Course struct {
	ID            string  `json:"id"`
	Code          string  `json:"code"`
	NameZh        string  `json:"nameZh"`
	NameEn        string  `json:"nameEn"`
	Department    string  `json:"department"`
	Teacher       string  `json:"teacher"`
	Credits       float64 `json:"credits"`
	Description   string  `json:"description"`
	Capacity      int     `json:"capacity"`
	EnrolledCount int     `json:"enrolledCount"`
	Enrolled      bool    `json:"enrolled"`
	Type          string  `json:"type"`
	Term          string  `json:"term"`
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

// Input structs accept the bilingual field names emitted by portal.js and a
// few common snake_case aliases used by older integrations.
type LoginRequest struct {
	Username string `json:"username"`
	Account  string `json:"account"`
	Password string `json:"password"`
}

type EnrollmentRequest struct {
	CourseID string `json:"courseId"`
	Action   string `json:"action"`
}

type CourseInput struct {
	ID          string   `json:"id"`
	Code        string   `json:"code"`
	Name        string   `json:"name"`
	NameZh      string   `json:"nameZh"`
	NameEn      string   `json:"nameEn"`
	Department  string   `json:"department"`
	Teacher     string   `json:"teacher"`
	Credits     *float64 `json:"credits"`
	Description string   `json:"description"`
	Capacity    *int     `json:"capacity"`
	Term        string   `json:"term"`
	Type        string   `json:"type"`
}

type AnnouncementInput struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	TitleZh      string `json:"titleZh"`
	TitleEn      string `json:"titleEn"`
	Body         string `json:"body"`
	Content      string `json:"content"`
	ContentZh    string `json:"contentZh"`
	ContentEn    string `json:"contentEn"`
	Type         string `json:"type"`
	Audience     string `json:"audience"`
	CourseID     string `json:"courseId"`
	PublishedAt  string `json:"publishedAt"`
	PublishedAt2 string `json:"published_at"`
	Published    *bool  `json:"published"`
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
	mu                      sync.RWMutex
	db                      *sql.DB
	users                   map[string]*User
	studentPasswordOverride bool
	adminPasswordOverride   bool
	courses                 []*Course
	enrollments             []*Enrollment
	grades                  []*Grade
	schedule                []*ScheduleEntry
	announcements           []*Announcement
}

func NewStore() *Store {
	return NewStoreWithPasswords(envOr("CGU_STUDENT_PASSWORD", "student-demo"), envOr("CGU_ADMIN_PASSWORD", "admin-demo"))
}

func NewStoreWithPasswords(studentPassword, adminPassword string) *Store {
	studentPasswordOverride := studentPassword != "" && studentPassword != "student-demo"
	adminPasswordOverride := adminPassword != "" && adminPassword != "admin-demo"
	if studentPassword == "" {
		studentPassword = "student-demo"
	}
	if adminPassword == "" {
		adminPassword = "admin-demo"
	}
	s := &Store{users: make(map[string]*User), studentPasswordOverride: studentPasswordOverride, adminPasswordOverride: adminPasswordOverride}
	s.users["student"] = &User{
		ID: "student", Username: "student", Name: "星尘同学", Email: "student@cgu.local", Role: "student",
		PasswordHash: hashPassword(studentPassword), StudentID: "CGU2026001", College: "风与自然科学学院", Year: "2026",
	}
	s.users["admin"] = &User{
		ID: "admin", Username: "admin", Name: "教务处", Email: "admin@cgu.local", Role: "admin",
		PasswordHash: hashPassword(adminPassword),
	}
	s.seed()
	return s
}

func (s *Store) authenticate(identifier, password string) *User {
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for _, user := range s.users {
		if strings.EqualFold(user.Username, identifier) || strings.EqualFold(user.Email, identifier) {
			found = true
			if !verifyPassword(password, user.PasswordHash) {
				return nil
			}
			// Upgrade hashes created by older builds after a successful login.
			if strings.HasPrefix(user.PasswordHash, "pbkdf2-sha256$") {
				user.PasswordHash = hashPassword(password)
				s.persistUserLocked(user)
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
		if item.Role == "student" {
			students++
		}
	}
	s.mu.RUnlock()
	return map[string]any{
		"id": user.ID, "username": user.Username, "role": user.Role, "name": user.Name,
		"email": user.Email, "studentId": user.StudentID, "college": user.College, "year": user.Year,
		"stats": map[string]any{"students": students},
	}
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

func (s *Store) gradesFor(studentID string, all bool) []Grade {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Grade, 0)
	for _, item := range s.grades {
		if all || item.StudentID == studentID {
			result = append(result, *item)
		}
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
	students, pending, sections := 0, 0, 0
	for _, user := range s.users {
		if user.Role == "student" {
			students++
		}
	}
	for _, item := range s.announcements {
		if !item.Published {
			pending++
		}
	}
	for _, item := range s.courses {
		if item.Capacity > 0 {
			sections++
		}
	}
	return map[string]int{"courses": len(s.courses), "students": students, "sections": sections, "pending": pending}
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
			s.persistEnrollmentLocked(current)
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
		s.persistEnrollmentLocked(enrollment)
		copy := *enrollment
		return &copy, nil
	}
	if current == nil {
		enrollment := &Enrollment{ID: "enrollment-" + randomID(12), StudentID: studentID, CourseID: course.ID, Term: course.Term, Status: "dropped"}
		s.persistEnrollmentLocked(enrollment)
		return enrollment, nil
	}
	current.Status = "dropped"
	s.persistEnrollmentLocked(current)
	copy := *current
	return &copy, nil
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
	s.courses = append(s.courses, course)
	s.persistCourseLocked(course)
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
	*s.courses[index] = *normalized
	s.persistCourseLocked(normalized)
	copy := *normalized
	return &copy, nil
}

func (s *Store) deleteCourse(id string) (*Course, *apiError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, item := range s.courses {
		if strings.EqualFold(item.ID, id) {
			copy := *item
			s.courses = append(s.courses[:i], s.courses[i+1:]...)
			s.deleteCoursePersistedLocked(item.ID)
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
	s.announcements = append(s.announcements, item)
	s.persistAnnouncementLocked(item)
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
			s.announcements[i] = normalized
			s.persistAnnouncementLocked(normalized)
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
			s.announcements = append(s.announcements[:i], s.announcements[i+1:]...)
			s.deleteAnnouncementPersistedLocked(item.ID)
			return &copy, nil
		}
	}
	return nil, apiErr(404, "announcement_not_found", "announcement not found")
}

func normalizeCourse(input CourseInput, existing *Course) (*Course, *apiError) {
	nameZh := first(input.NameZh, input.Name)
	nameEn := first(input.NameEn)
	code := strings.TrimSpace(input.Code)
	id := strings.TrimSpace(input.ID)
	if existing != nil {
		if nameZh == "" {
			nameZh = existing.NameZh
		}
		if nameEn == "" {
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
	if nameEn == "" {
		nameEn = nameZh
	}
	department, teacher, description, term, courseType := input.Department, input.Teacher, input.Description, input.Term, input.Type
	if existing != nil {
		if department == "" {
			department = existing.Department
		}
		if teacher == "" {
			teacher = existing.Teacher
		}
		if description == "" {
			description = existing.Description
		}
		if term == "" {
			term = existing.Term
		}
		if courseType == "" {
			courseType = existing.Type
		}
	}
	if department == "" {
		department = "综合学院"
	}
	if teacher == "" {
		teacher = "待定"
	}
	if term == "" {
		term = "2026-秋"
	}
	if courseType == "" {
		courseType = "elective"
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
	return &Course{ID: id, Code: code, NameZh: nameZh, NameEn: nameEn, Department: department, Teacher: teacher, Credits: credits, Description: description, Capacity: capacity, EnrolledCount: enrolledCount, Type: courseType, Term: term}, nil
}

func normalizeAnnouncement(input AnnouncementInput, existing *Announcement) (*Announcement, *apiError) {
	titleZh := first(input.TitleZh, input.Title)
	titleEn := first(input.TitleEn)
	contentZh := first(input.ContentZh, input.Content, input.Body)
	contentEn := first(input.ContentEn)
	id := strings.TrimSpace(input.ID)
	typ, audience, courseID, publishedAt := input.Type, input.Audience, input.CourseID, input.PublishedAt
	published := true
	if existing != nil {
		if titleZh == "" {
			titleZh = existing.TitleZh
		}
		if titleEn == "" {
			titleEn = existing.TitleEn
		}
		if contentZh == "" {
			contentZh = existing.ContentZh
		}
		if contentEn == "" {
			contentEn = existing.ContentEn
		}
		if id == "" {
			id = existing.ID
		}
		if typ == "" {
			typ = existing.Type
		}
		if audience == "" {
			audience = existing.Audience
		}
		if courseID == "" {
			courseID = existing.CourseID
		}
		if publishedAt == "" {
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
	if titleEn == "" {
		titleEn = titleZh
	}
	if contentEn == "" {
		contentEn = contentZh
	}
	if typ == "" {
		typ = "CAMPUS"
	}
	if audience == "" {
		audience = "all"
	}
	if publishedAt == "" {
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

func first(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

type Server struct {
	store         *Store
	staticDir     string
	sessions      map[string]session
	sessionsMu    sync.Mutex
	loginMu       sync.Mutex
	loginAttempts map[string]*loginAttempt
	cookieSecure  bool
	storageMode   string
	storageMu     sync.RWMutex
}

type loginAttempt struct {
	failures   int
	windowFrom time.Time
	blockedTo  time.Time
}

func NewServer(store *Store, staticDir string) *Server {
	secure := strings.EqualFold(os.Getenv("CGU_COOKIE_SECURE"), "1") || strings.EqualFold(os.Getenv("CGU_COOKIE_SECURE"), "true")
	return &Server{store: store, staticDir: staticDir, sessions: make(map[string]session), loginAttempts: make(map[string]*loginAttempt), cookieSecure: secure, storageMode: "memory"}
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
	case p == "/api/announcements" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "announcements": s.store.announcementsFor(s.currentUser(r), false)})
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
	rateKey := loginRateKey(r, identifier)
	if retry, allowed := s.loginAllowed(rateKey); !allowed {
		seconds := int(retry / time.Second)
		if retry%time.Second != 0 {
			seconds++
		}
		if seconds < 1 {
			seconds = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		writeError(w, apiErr(429, "login_rate_limited", "too many failed sign-in attempts; try again later"))
		return
	}
	user := s.store.authenticate(identifier, input.Password)
	if user == nil {
		s.loginFailure(rateKey)
		writeError(w, apiErr(401, "invalid_credentials", "username or password is incorrect"))
		return
	}
	s.loginSuccess(rateKey)
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
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: s.cookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: int(sessionTTL.Seconds()), Expires: time.Now().Add(sessionTTL)})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "user": s.store.publicUser(user)})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		s.sessionsMu.Lock()
		delete(s.sessions, cookie.Value)
		s.sessionsMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: s.cookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(1, 0)})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
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
	if user.Role != "admin" && target != user.ID {
		writeError(w, apiErr(403, "forbidden", "students may only view their own grades"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "grades": s.store.gradesFor(target, user.Role == "admin" && !hasStudentQuery(r))})
}

func (s *Server) listSchedule(w http.ResponseWriter, r *http.Request) {
	user := s.currentUser(r)
	target := queryStudent(r, user)
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
	return s.store.user(item.UserID)
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
	origin := strings.TrimRight(strings.TrimSpace(r.Header.Get("Origin")), "/")
	if origin == "" || !strings.HasPrefix(r.URL.Path, "/api") {
		return true
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodPatch && r.Method != http.MethodDelete && r.Method != http.MethodOptions {
		return true
	}
	return strings.EqualFold(origin, scheme+"://"+r.Host)
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

func loginRateKey(r *http.Request, identifier string) string {
	client := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(client); err == nil && host != "" {
		client = host
	}
	if client == "" {
		client = "unknown"
	}
	return strings.ToLower(client) + "\x00" + strings.ToLower(strings.TrimSpace(identifier))
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

func (s *Server) handleOptions(w http.ResponseWriter, r *http.Request) {
	origin := strings.TrimRight(strings.TrimSpace(r.Header.Get("Origin")), "/")
	if origin != "" && s.originAllowed(&http.Request{Method: http.MethodPost, URL: r.URL, Host: r.Host, Header: r.Header, TLS: r.TLS}) {
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
	if r.URL.Path == "/login" {
		http.Redirect(w, r, "/login.html", http.StatusFound)
		return
	}
	if r.URL.Path == "/portal" {
		http.Redirect(w, r, "/portal.html", http.StatusFound)
		return
	}
	if r.URL.Path == "/admin" {
		http.Redirect(w, r, "/admin.html", http.StatusFound)
		return
	}
	clean := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
	relative := strings.TrimPrefix(clean, "/")
	if relative == "" {
		relative = "index.html"
	}
	root, err := filepath.Abs(s.staticDir)
	if err != nil {
		http.Error(w, "static files unavailable", 500)
		return
	}
	candidate := filepath.Join(root, filepath.FromSlash(relative))
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		http.NotFound(w, r)
		return
	}
	if info, statErr := os.Stat(candidate); statErr != nil || info.IsDir() {
		candidate = filepath.Join(root, "index.html")
	}
	http.ServeFile(w, r, candidate)
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

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
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
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return "bcrypt$" + string(hash)
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
// external x/crypto dependency in the single-binary demo service.
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
		&Course{ID: "cgu-elements-101", Code: "ELM101", NameZh: "元素力与世界观", NameEn: "Elements & Worldview", Department: "元素科学学院", Teacher: "丽莎", Credits: 3, Description: "从七元素的基本性质出发，建立理解提瓦特世界的第一套方法。", Capacity: 60, Type: "required", Term: "2026-秋"},
		&Course{ID: "cgu-nature-202", Code: "NAT202", NameZh: "提瓦特自然地理", NameEn: "Teyvat Physical Geography", Department: "地脉与自然学院", Teacher: "钟离", Credits: 4, Description: "沿着地脉、山海与遗迹，练习用田野调查读懂一片土地。", Capacity: 45, Type: "required", Term: "2026-秋"},
		&Course{ID: "cgu-mondstadt-210", Code: "MUS210", NameZh: "风之诗与文化记忆", NameEn: "Songs of Wind & Cultural Memory", Department: "人文与艺术学院", Teacher: "温迪", Credits: 3, Description: "以民谣、诗歌和城市记忆为线索，研究文化如何被传唱。", Capacity: 50, Type: "elective", Term: "2026-秋"},
		&Course{ID: "cgu-adventure-301", Code: "ADV301", NameZh: "冒险实践与团队协作", NameEn: "Adventure Practice & Teamwork", Department: "冒险实践学院", Teacher: "凯瑟琳", Credits: 2, Description: "把风险评估、路线规划与可靠的伙伴关系带进真实任务。", Capacity: 35, Type: "elective", Term: "2026-秋"},
		&Course{ID: "cgu-fontaine-310", Code: "FON310", NameZh: "审判与机械文明", NameEn: "Judgment & Mechanical Civilization", Department: "枫丹法政与工程学院", Teacher: "那维莱特", Credits: 4, Description: "从水之国的法庭与工坊，研究规则、能源与机械创造。", Capacity: 40, Type: "required", Term: "2026-秋"},
		&Course{ID: "cgu-natlan-220", Code: "NAT220", NameZh: "火与竞技生态", NameEn: "Fire & Competitive Ecology", Department: "纳塔田野与竞技学院", Teacher: "教务联合授课", Credits: 3, Description: "在部族、仪式与竞技场之间，完成一场尊重当地知识的田野研究。", Capacity: 36, Type: "elective", Term: "2026-秋"},
		&Course{ID: "cgu-snezhnaya-401", Code: "SNE401", NameZh: "至冬研究与极地治理", NameEn: "Snezhnaya Studies & Polar Governance", Department: "至冬与极地研究学院", Teacher: "教务联合授课", Credits: 4, Description: "以 7.0「无神怜爱的雪国」为新起点，研究冰原社会、风险与远行伦理。", Capacity: 32, Type: "elective", Term: "2026-秋"},
	)
	s.enrollments = append(s.enrollments, &Enrollment{ID: "enrollment-elements-101", StudentID: "student", CourseID: "cgu-elements-101", Term: "2026-秋", Status: "enrolled"})
	s.grades = append(s.grades,
		&Grade{ID: "grade-elm101", StudentID: "student", CourseID: "cgu-elements-101", CourseCode: "ELM101", CourseNameZh: "元素力与世界观", CourseNameEn: "Elements & Worldview", Score: 96, Point: 4.0, Term: "2026-春", Status: "passed", Credits: 3},
		&Grade{ID: "grade-nat202", StudentID: "student", CourseID: "cgu-nature-202", CourseCode: "NAT202", CourseNameZh: "提瓦特自然地理", CourseNameEn: "Teyvat Physical Geography", Score: 91, Point: 3.7, Term: "2026-春", Status: "passed", Credits: 4},
	)
	s.schedule = append(s.schedule,
		&ScheduleEntry{ID: "slot-elm101", StudentID: "student", CourseID: "cgu-elements-101", CourseCode: "ELM101", CourseNameZh: "元素力与世界观", CourseNameEn: "Elements & Worldview", Day: 1, Start: "09:00", End: "10:40", Location: "风之庭院 101", Teacher: "丽莎"},
		&ScheduleEntry{ID: "slot-mondstadt210", StudentID: "student", CourseID: "cgu-mondstadt-210", CourseCode: "MUS210", CourseNameZh: "风之诗与文化记忆", CourseNameEn: "Songs of Wind & Cultural Memory", Day: 3, Start: "14:00", End: "15:40", Location: "西风礼堂 204", Teacher: "温迪"},
	)
	s.announcements = append(s.announcements,
		&Announcement{ID: "announcement-welcome", TitleZh: "2026 秋季学期报到安排", TitleEn: "Autumn 2026 arrival schedule", ContentZh: "风之庭院将于 9 月 1 日开放报到，旅行者请携带录取确认函。", ContentEn: "Windrise Court opens on 1 September. Bring your admission confirmation.", Type: "ADMISSIONS", Audience: "all", PublishedAt: "2026-08-20T09:00:00Z", Published: true, Author: "admin"},
		&Announcement{ID: "announcement-enrollment", TitleZh: "选课周提醒", TitleEn: "Course selection week reminder", ContentZh: "学生门户将在 8 月 26 日 09:00 开放选课，请提前确认课表。", ContentEn: "Course selection opens at 09:00 on 26 August. Review your schedule first.", Type: "ACADEMICS", Audience: "student", PublishedAt: "2026-08-22T09:00:00Z", Published: true, Author: "admin"},
		&Announcement{ID: "announcement-snezhnaya-70", TitleZh: "7.0「无神怜爱的雪国」：至冬研究方向开放", TitleEn: "Version 7.0 “Everwinter Without Mercy”: Snezhnaya studies open", ContentZh: "根据原神官方 7.0 版本资讯，至冬成为新的旅途舞台。CGU 新增至冬研究与极地治理课程，官网同步提供官方新闻入口。", ContentEn: "Following the official Version 7.0 update, Snezhnaya is now the next stage of the journey. CGU adds a Snezhnaya studies track and links to the official news source.", Type: "WORLD_UPDATE", Audience: "all", PublishedAt: "2026-08-24T08:00:00+08:00", Published: true, Author: "admin"},
	)
}

func main() {
	cfg := LoadConfig()
	addr := cfg.Server.Address
	if !strings.Contains(addr, ":") {
		addr = ":" + addr
	}
	if !isLoopbackListenAddress(addr) && cfg.AdminPassword == "admin-demo" {
		log.Fatal("refusing non-loopback listener with the default administrator password; set CGU_ADMIN_PASSWORD")
	}
	store := NewStoreWithPasswords(cfg.StudentPassword, cfg.AdminPassword)
	storageMode := "memory"
	var database *sql.DB
	if cfg.Database.Enabled {
		var databaseErr error
		database, databaseErr = openMySQL(cfg)
		if databaseErr != nil {
			storageMode = "memory-fallback"
			log.Printf("MySQL unavailable; using in-memory fallback (%T)", databaseErr)
		} else if databaseErr = store.attachDatabase(database); databaseErr != nil {
			_ = database.Close()
			database = nil
			storageMode = "memory-fallback"
			log.Printf("MySQL initialization failed; using in-memory fallback (%T)", databaseErr)
		} else {
			storageMode = "mysql"
		}
	}
	if database != nil {
		defer database.Close()
	}
	handler := NewServer(store, cfg.StaticDir)
	// LoadConfig already applies environment, .env, and config.json precedence.
	handler.cookieSecure = cfg.CookieSecure
	handler.setStorageMode(storageMode)
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
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
