package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS cgu_users (
  id VARCHAR(64) PRIMARY KEY,
  username VARCHAR(128) NOT NULL UNIQUE,
  name_text VARCHAR(255) NOT NULL,
  email VARCHAR(255) NOT NULL,
  role_name VARCHAR(32) NOT NULL,
  password_hash TEXT NOT NULL,
  student_id VARCHAR(128) NOT NULL DEFAULT '',
  college VARCHAR(255) NOT NULL DEFAULT '',
  year_text VARCHAR(32) NOT NULL DEFAULT '',
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS cgu_courses (
  id VARCHAR(64) PRIMARY KEY,
  code VARCHAR(64) NOT NULL UNIQUE,
  name_zh VARCHAR(255) NOT NULL,
  name_en VARCHAR(255) NOT NULL,
  department VARCHAR(255) NOT NULL,
  teacher VARCHAR(255) NOT NULL,
  credits DECIMAL(8,2) NOT NULL DEFAULT 0,
  description TEXT NOT NULL,
  capacity INT NOT NULL DEFAULT 1,
  course_type VARCHAR(32) NOT NULL DEFAULT 'elective',
  term_name VARCHAR(64) NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS cgu_enrollments (
  id VARCHAR(64) PRIMARY KEY,
  student_id VARCHAR(64) NOT NULL,
  course_id VARCHAR(64) NOT NULL,
  term_name VARCHAR(64) NOT NULL,
  status_name VARCHAR(32) NOT NULL,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_enrollment_student (student_id),
  INDEX idx_enrollment_course (course_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS cgu_grades (
  id VARCHAR(64) PRIMARY KEY,
  student_id VARCHAR(64) NOT NULL,
  course_id VARCHAR(64) NOT NULL,
  course_code VARCHAR(64) NOT NULL,
  course_name_zh VARCHAR(255) NOT NULL,
  course_name_en VARCHAR(255) NOT NULL,
  score_text VARCHAR(32) NOT NULL,
  point_text VARCHAR(32) NOT NULL,
  term_name VARCHAR(64) NOT NULL,
  status_name VARCHAR(32) NOT NULL,
  credits INT NOT NULL DEFAULT 0,
  INDEX idx_grade_student (student_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS cgu_schedule (
  id VARCHAR(64) PRIMARY KEY,
  student_id VARCHAR(64) NOT NULL,
  course_id VARCHAR(64) NOT NULL,
  course_code VARCHAR(64) NOT NULL,
  course_name_zh VARCHAR(255) NOT NULL,
  course_name_en VARCHAR(255) NOT NULL,
  day_number INT NOT NULL,
  start_time VARCHAR(16) NOT NULL,
  end_time VARCHAR(16) NOT NULL,
  location_name VARCHAR(255) NOT NULL,
  teacher VARCHAR(255) NOT NULL,
  INDEX idx_schedule_student (student_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS cgu_announcements (
  id VARCHAR(64) PRIMARY KEY,
  title_zh VARCHAR(255) NOT NULL,
  title_en VARCHAR(255) NOT NULL,
  content_zh TEXT NOT NULL,
  content_en TEXT NOT NULL,
  type_name VARCHAR(64) NOT NULL,
  audience_name VARCHAR(64) NOT NULL,
  course_id VARCHAR(64) NOT NULL DEFAULT '',
  published_at VARCHAR(64) NOT NULL,
  published_flag TINYINT(1) NOT NULL DEFAULT 1,
  author_name VARCHAR(128) NOT NULL DEFAULT 'admin',
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS cgu_admissions (
  id VARCHAR(64) PRIMARY KEY,
  name_text VARCHAR(120) NOT NULL,
  email VARCHAR(255) NOT NULL,
  school_text VARCHAR(160) NOT NULL,
  status_name VARCHAR(32) NOT NULL DEFAULT 'pending',
  notes_text TEXT NOT NULL,
  created_at VARCHAR(64) NOT NULL,
  updated_at VARCHAR(64) NOT NULL,
  INDEX idx_admission_status (status_name),
  INDEX idx_admission_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS cgu_mailbox_messages (
  id VARCHAR(64) PRIMARY KEY,
  recipient_id VARCHAR(64) NOT NULL,
  sender_id VARCHAR(64) NOT NULL,
  sender_name VARCHAR(128) NOT NULL,
  subject_text VARCHAR(200) NOT NULL,
  body_text TEXT NOT NULL,
  created_at VARCHAR(64) NOT NULL,
  read_at VARCHAR(64) NULL,
  delivery_mode VARCHAR(32) NOT NULL DEFAULT 'internal',
  external_recipient VARCHAR(255) NOT NULL DEFAULT '',
  delivery_status VARCHAR(32) NOT NULL DEFAULT 'internal',
  delivery_error TEXT NULL,
  delivered_at VARCHAR(64) NOT NULL DEFAULT '',
  request_key VARCHAR(128) NULL,
  delivery_started_at VARCHAR(64) NOT NULL DEFAULT '',
  INDEX idx_mailbox_recipient (recipient_id, created_at),
  INDEX idx_mailbox_unread (recipient_id, read_at),
  UNIQUE KEY uq_mailbox_request_key (request_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS cgu_site_content (
  content_key VARCHAR(160) PRIMARY KEY,
  zh_text TEXT NOT NULL,
  en_text TEXT NOT NULL,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`

func openMySQL(cfg AppConfig) (*sql.DB, error) {
	db, err := sql.Open(cfg.Database.Driver, cfg.MySQLDSN())
	if err != nil {
		return nil, err
	}
	maxOpen := cfg.Database.MaxOpenConns
	if maxOpen < 1 {
		maxOpen = 10
	}
	maxIdle := cfg.Database.MaxIdleConns
	if maxIdle < 0 {
		maxIdle = 0
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(30 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrateDatabase(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrateDatabase(ctx context.Context, db *sql.DB) error {
	for _, statement := range strings.Split(schemaSQL, ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("database migration: %w", err)
		}
	}
	if err := ensureMailboxDeliveryColumns(ctx, db); err != nil {
		return fmt.Errorf("database mailbox migration: %w", err)
	}
	return nil
}

// ensureMailboxDeliveryColumns upgrades installations created before SMTP
// delivery was introduced. Column definitions are static (never built from
// user input), while existence checks keep the migration idempotent.
func ensureMailboxDeliveryColumns(ctx context.Context, db *sql.DB) error {
	columns := []struct {
		name string
		def  string
	}{
		{name: "delivery_mode", def: "VARCHAR(32) NOT NULL DEFAULT 'internal'"},
		{name: "external_recipient", def: "VARCHAR(255) NOT NULL DEFAULT ''"},
		{name: "delivery_status", def: "VARCHAR(32) NOT NULL DEFAULT 'internal'"},
		{name: "delivery_error", def: "TEXT NULL"},
		{name: "delivered_at", def: "VARCHAR(64) NOT NULL DEFAULT ''"},
		{name: "request_key", def: "VARCHAR(128) NULL"},
		{name: "delivery_started_at", def: "VARCHAR(64) NOT NULL DEFAULT ''"},
	}
	for _, column := range columns {
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?`, "cgu_mailbox_messages", column.name).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		if _, err := db.ExecContext(ctx, `ALTER TABLE cgu_mailbox_messages ADD COLUMN `+column.name+` `+column.def); err != nil && !isDuplicateSchemaObject(err) {
			return fmt.Errorf("add %s: %w", column.name, err)
		}
	}
	var indexCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = ?`, "cgu_mailbox_messages", "uq_mailbox_request_key").Scan(&indexCount); err != nil {
		return err
	}
	if indexCount == 0 {
		if _, err := db.ExecContext(ctx, `ALTER TABLE cgu_mailbox_messages ADD UNIQUE INDEX uq_mailbox_request_key (request_key)`); err != nil && !isDuplicateSchemaObject(err) {
			return fmt.Errorf("add mailbox request key index: %w", err)
		}
	}
	return nil
}

// MySQL schema discovery and ALTER are intentionally separate so this
// migration can run on more than one application instance. If another
// instance wins the race between those statements, MySQL reports a duplicate
// column or index; that outcome means the desired schema is already present.
func isDuplicateSchemaObject(err error) bool {
	var mysqlErr *mysql.MySQLError
	if !errors.As(err, &mysqlErr) {
		return false
	}
	switch mysqlErr.Number {
	case 1060, // ER_DUP_FIELDNAME
		1061, // ER_DUP_KEYNAME
		1831: // ER_DUP_INDEX
		return true
	default:
		return false
	}
}

func (s *Store) attachDatabase(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db = db
	if err := s.ensureDatabaseSeedLocked(ctx); err != nil {
		s.db = nil
		return err
	}
	if err := s.loadDatabaseLocked(ctx); err != nil {
		s.db = nil
		return err
	}
	return nil
}

func (s *Store) ensureDatabaseSeedLocked(ctx context.Context) error {
	// Older builds inserted one fixed student account and related academic rows.
	// Remove every legacy marker used by those builds, even if an operator
	// edited one field before upgrading. Current accounts use random IDs and
	// cannot match these historical markers accidentally.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin legacy account cleanup: %w", err)
	}
	for _, statement := range []string{
		`DELETE FROM cgu_enrollments WHERE id = 'enrollment-elements-101' OR student_id IN ('student', 'CGU2026001') OR student_id IN (SELECT id FROM cgu_users WHERE id = 'student' OR (role_name = 'student' AND (username = 'student' OR email = 'student@cgu.local' OR student_id = 'CGU2026001')))`,
		`DELETE FROM cgu_grades WHERE id IN ('grade-elm101', 'grade-nat202') OR student_id IN ('student', 'CGU2026001') OR student_id IN (SELECT id FROM cgu_users WHERE id = 'student' OR (role_name = 'student' AND (username = 'student' OR email = 'student@cgu.local' OR student_id = 'CGU2026001')))`,
		`DELETE FROM cgu_schedule WHERE id IN ('slot-elm101', 'slot-mondstadt210') OR student_id IN ('student', 'CGU2026001') OR student_id IN (SELECT id FROM cgu_users WHERE id = 'student' OR (role_name = 'student' AND (username = 'student' OR email = 'student@cgu.local' OR student_id = 'CGU2026001')))`,
		`DELETE FROM cgu_users WHERE id = 'student' OR (role_name = 'student' AND (username = 'student' OR email = 'student@cgu.local' OR student_id = 'CGU2026001'))`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("remove legacy account data: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit legacy account cleanup: %w", err)
	}

	admin, ok := s.users["admin"]
	if !ok || strings.TrimSpace(admin.PasswordHash) == "" {
		return fmt.Errorf("bootstrap administrator is not configured")
	}
	var conflictingID string
	err = s.db.QueryRowContext(ctx, `SELECT id FROM cgu_users WHERE username = ? AND id <> ? LIMIT 1`, admin.Username, admin.ID).Scan(&conflictingID)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("check bootstrap administrator username: %w", err)
	}
	if err == nil {
		return fmt.Errorf("bootstrap administrator username is already assigned to another account")
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO cgu_users (id, username, name_text, email, role_name, password_hash, student_id, college, year_text) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE username=VALUES(username), name_text=VALUES(name_text), email=VALUES(email), role_name=VALUES(role_name), password_hash=VALUES(password_hash), student_id=VALUES(student_id), college=VALUES(college), year_text=VALUES(year_text)`, admin.ID, admin.Username, admin.Name, admin.Email, admin.Role, admin.PasswordHash, admin.StudentID, admin.College, admin.Year); err != nil {
		return fmt.Errorf("upsert bootstrap administrator: %w", err)
	}
	if err := s.seedCoursesLocked(ctx); err != nil {
		return err
	}
	if err := s.seedAnnouncementsLocked(ctx); err != nil {
		return err
	}
	return s.seedSiteContentLocked(ctx)
}

func (s *Store) seedCoursesLocked(ctx context.Context) error {
	for _, item := range s.courses {
		if _, err := s.db.ExecContext(ctx, `INSERT IGNORE INTO cgu_courses (id, code, name_zh, name_en, department, teacher, credits, description, capacity, course_type, term_name) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, item.ID, item.Code, item.NameZh, item.NameEn, item.Department, item.Teacher, item.Credits, item.Description, item.Capacity, item.Type, item.Term); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) seedAnnouncementsLocked(ctx context.Context) error {
	for _, item := range s.announcements {
		if _, err := s.db.ExecContext(ctx, `INSERT IGNORE INTO cgu_announcements (id, title_zh, title_en, content_zh, content_en, type_name, audience_name, course_id, published_at, published_flag, author_name) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, item.ID, item.TitleZh, item.TitleEn, item.ContentZh, item.ContentEn, item.Type, item.Audience, item.CourseID, item.PublishedAt, item.Published, item.Author); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) seedSiteContentLocked(ctx context.Context) error {
	for _, item := range s.siteContent {
		if item == nil {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `INSERT IGNORE INTO cgu_site_content (content_key, zh_text, en_text) VALUES (?, ?, ?)`, item.Key, item.Zh, item.En); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) loadDatabaseLocked(ctx context.Context) error {
	users, err := loadUsers(ctx, s.db)
	if err != nil {
		return err
	}
	courses, err := loadCourses(ctx, s.db)
	if err != nil {
		return err
	}
	enrollments, err := loadEnrollments(ctx, s.db)
	if err != nil {
		return err
	}
	grades, err := loadGrades(ctx, s.db)
	if err != nil {
		return err
	}
	schedule, err := loadSchedule(ctx, s.db)
	if err != nil {
		return err
	}
	announcements, err := loadAnnouncements(ctx, s.db)
	if err != nil {
		return err
	}
	admissions, err := loadAdmissions(ctx, s.db)
	if err != nil {
		return err
	}
	mailbox, err := loadMailbox(ctx, s.db)
	if err != nil {
		return err
	}
	siteContent, err := loadSiteContent(ctx, s.db)
	if err != nil {
		return err
	}
	for id, user := range users {
		s.users[id] = user
	}
	if len(courses) > 0 {
		s.courses = courses
	}
	for _, item := range siteContent {
		copy := item
		s.siteContent[item.Key] = &copy
	}
	s.enrollments, s.grades, s.schedule, s.announcements, s.admissions, s.mailbox = enrollments, grades, schedule, announcements, admissions, mailbox
	return nil
}

func loadUsers(ctx context.Context, db *sql.DB) (map[string]*User, error) {
	rows, err := db.QueryContext(ctx, "SELECT id, username, name_text, email, role_name, password_hash, student_id, college, year_text FROM cgu_users")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]*User)
	for rows.Next() {
		item := &User{}
		if err := rows.Scan(&item.ID, &item.Username, &item.Name, &item.Email, &item.Role, &item.PasswordHash, &item.StudentID, &item.College, &item.Year); err != nil {
			return nil, err
		}
		result[item.ID] = item
	}
	return result, rows.Err()
}

func loadCourses(ctx context.Context, db *sql.DB) ([]*Course, error) {
	rows, err := db.QueryContext(ctx, "SELECT id, code, name_zh, name_en, department, teacher, credits, description, capacity, course_type, term_name FROM cgu_courses ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]*Course, 0)
	for rows.Next() {
		item := &Course{}
		if err := rows.Scan(&item.ID, &item.Code, &item.NameZh, &item.NameEn, &item.Department, &item.Teacher, &item.Credits, &item.Description, &item.Capacity, &item.Type, &item.Term); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func loadEnrollments(ctx context.Context, db *sql.DB) ([]*Enrollment, error) {
	rows, err := db.QueryContext(ctx, "SELECT id, student_id, course_id, term_name, status_name FROM cgu_enrollments ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]*Enrollment, 0)
	for rows.Next() {
		item := &Enrollment{}
		if err := rows.Scan(&item.ID, &item.StudentID, &item.CourseID, &item.Term, &item.Status); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func loadGrades(ctx context.Context, db *sql.DB) ([]*Grade, error) {
	rows, err := db.QueryContext(ctx, "SELECT id, student_id, course_id, course_code, course_name_zh, course_name_en, score_text, point_text, term_name, status_name, credits FROM cgu_grades ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]*Grade, 0)
	for rows.Next() {
		item := &Grade{}
		var score, point string
		if err := rows.Scan(&item.ID, &item.StudentID, &item.CourseID, &item.CourseCode, &item.CourseNameZh, &item.CourseNameEn, &score, &point, &item.Term, &item.Status, &item.Credits); err != nil {
			return nil, err
		}
		item.Score, item.Point = score, point
		result = append(result, item)
	}
	return result, rows.Err()
}

func loadSchedule(ctx context.Context, db *sql.DB) ([]*ScheduleEntry, error) {
	rows, err := db.QueryContext(ctx, "SELECT id, student_id, course_id, course_code, course_name_zh, course_name_en, day_number, start_time, end_time, location_name, teacher FROM cgu_schedule ORDER BY day_number, start_time")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]*ScheduleEntry, 0)
	for rows.Next() {
		item := &ScheduleEntry{}
		if err := rows.Scan(&item.ID, &item.StudentID, &item.CourseID, &item.CourseCode, &item.CourseNameZh, &item.CourseNameEn, &item.Day, &item.Start, &item.End, &item.Location, &item.Teacher); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func loadAnnouncements(ctx context.Context, db *sql.DB) ([]*Announcement, error) {
	rows, err := db.QueryContext(ctx, "SELECT id, title_zh, title_en, content_zh, content_en, type_name, audience_name, course_id, published_at, published_flag, author_name FROM cgu_announcements ORDER BY published_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]*Announcement, 0)
	for rows.Next() {
		item := &Announcement{}
		if err := rows.Scan(&item.ID, &item.TitleZh, &item.TitleEn, &item.ContentZh, &item.ContentEn, &item.Type, &item.Audience, &item.CourseID, &item.PublishedAt, &item.Published, &item.Author); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func loadAdmissions(ctx context.Context, db *sql.DB) ([]*AdmissionApplication, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name_text, email, school_text, status_name, notes_text, created_at, updated_at FROM cgu_admissions ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]*AdmissionApplication, 0)
	for rows.Next() {
		item := &AdmissionApplication{}
		if err := rows.Scan(&item.ID, &item.Name, &item.Email, &item.School, &item.Status, &item.Notes, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func loadMailbox(ctx context.Context, db *sql.DB) ([]*MailboxMessage, error) {
	leaseCutoff := time.Now().UTC().Add(-mailboxDeliveryLease).Format(time.RFC3339)
	if _, err := db.ExecContext(ctx, `UPDATE cgu_mailbox_messages SET delivery_status = 'unknown', delivery_error = 'SMTP outcome unknown after an expired delivery lease; confirm the relay did not accept it before retrying', delivery_started_at = '' WHERE delivery_status IN ('sending', 'pending') AND (delivery_started_at = '' OR delivery_started_at < ?)`, leaseCutoff); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT id, recipient_id, sender_id, sender_name, subject_text, body_text, created_at, read_at, delivery_mode, external_recipient, delivery_status, delivery_error, delivered_at, request_key, delivery_started_at FROM cgu_mailbox_messages ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]*MailboxMessage, 0)
	for rows.Next() {
		item := &MailboxMessage{}
		var readAt, deliveryMode, externalRecipient, deliveryStatus, deliveryError, deliveredAt, requestKey, deliveryStartedAt sql.NullString
		if err := rows.Scan(&item.ID, &item.RecipientID, &item.SenderID, &item.SenderName, &item.Subject, &item.Body, &item.CreatedAt, &readAt, &deliveryMode, &externalRecipient, &deliveryStatus, &deliveryError, &deliveredAt, &requestKey, &deliveryStartedAt); err != nil {
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
		if requestKey.Valid {
			item.RequestKey = requestKey.String
		}
		if deliveryStartedAt.Valid {
			item.DeliveryStartedAt = deliveryStartedAt.String
		}
		if strings.TrimSpace(item.DeliveryMode) == "" {
			item.DeliveryMode = mailboxDeliveryModeInternal
		}
		if strings.TrimSpace(item.DeliveryStatus) == "" {
			item.DeliveryStatus = mailboxDeliveryInternal
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func loadSiteContent(ctx context.Context, db *sql.DB) ([]SiteContent, error) {
	rows, err := db.QueryContext(ctx, `SELECT content_key, zh_text, en_text, updated_at FROM cgu_site_content ORDER BY content_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]SiteContent, 0)
	for rows.Next() {
		item := SiteContent{}
		var updated time.Time
		if err := rows.Scan(&item.Key, &item.Zh, &item.En, &updated); err != nil {
			return nil, err
		}
		item.UpdatedAt = updated.UTC().Format(time.RFC3339)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) persistEnrollmentLocked(item *Enrollment) error {
	if s.db == nil || item == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `INSERT INTO cgu_enrollments (id, student_id, course_id, term_name, status_name) VALUES (?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE status_name=VALUES(status_name), term_name=VALUES(term_name)`, item.ID, item.StudentID, item.CourseID, item.Term, item.Status)
	return err
}

func (s *Store) persistUserLocked(item *User) {
	if s.db == nil || item == nil {
		return
	}
	if err := s.persistUserLockedErr(item); err != nil {
		log.Printf("CGU database write warning (user hash upgrade): %v", err)
	}
}

func (s *Store) persistUserLockedErr(item *User) error {
	if s.db == nil || item == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `INSERT INTO cgu_users (id, username, name_text, email, role_name, password_hash, student_id, college, year_text) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE username=VALUES(username), password_hash=VALUES(password_hash), name_text=VALUES(name_text), email=VALUES(email), role_name=VALUES(role_name), student_id=VALUES(student_id), college=VALUES(college), year_text=VALUES(year_text)`, item.ID, item.Username, item.Name, item.Email, item.Role, item.PasswordHash, item.StudentID, item.College, item.Year)
	return err
}

func (s *Store) persistGradeLocked(item *Grade) error {
	if s.db == nil || item == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `INSERT INTO cgu_grades (id, student_id, course_id, course_code, course_name_zh, course_name_en, score_text, point_text, term_name, status_name, credits) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE student_id=VALUES(student_id), course_id=VALUES(course_id), course_code=VALUES(course_code), course_name_zh=VALUES(course_name_zh), course_name_en=VALUES(course_name_en), score_text=VALUES(score_text), point_text=VALUES(point_text), term_name=VALUES(term_name), status_name=VALUES(status_name), credits=VALUES(credits)`, item.ID, item.StudentID, item.CourseID, item.CourseCode, item.CourseNameZh, item.CourseNameEn, fmt.Sprint(item.Score), fmt.Sprint(item.Point), item.Term, item.Status, item.Credits)
	return err
}

func (s *Store) deleteGradePersistedLocked(id string) error {
	if s.db == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `DELETE FROM cgu_grades WHERE id = ?`, id)
	return err
}

func (s *Store) persistScheduleLocked(item *ScheduleEntry) error {
	if s.db == nil || item == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `INSERT INTO cgu_schedule (id, student_id, course_id, course_code, course_name_zh, course_name_en, day_number, start_time, end_time, location_name, teacher) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE student_id=VALUES(student_id), course_id=VALUES(course_id), course_code=VALUES(course_code), course_name_zh=VALUES(course_name_zh), course_name_en=VALUES(course_name_en), day_number=VALUES(day_number), start_time=VALUES(start_time), end_time=VALUES(end_time), location_name=VALUES(location_name), teacher=VALUES(teacher)`, item.ID, item.StudentID, item.CourseID, item.CourseCode, item.CourseNameZh, item.CourseNameEn, item.Day, item.Start, item.End, item.Location, item.Teacher)
	return err
}

func (s *Store) deleteSchedulePersistedLocked(id string) error {
	if s.db == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `DELETE FROM cgu_schedule WHERE id = ?`, id)
	return err
}

func (s *Store) persistCourseLocked(item *Course) error {
	if s.db == nil || item == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `INSERT INTO cgu_courses (id, code, name_zh, name_en, department, teacher, credits, description, capacity, course_type, term_name) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE code=VALUES(code), name_zh=VALUES(name_zh), name_en=VALUES(name_en), department=VALUES(department), teacher=VALUES(teacher), credits=VALUES(credits), description=VALUES(description), capacity=VALUES(capacity), course_type=VALUES(course_type), term_name=VALUES(term_name)`, item.ID, item.Code, item.NameZh, item.NameEn, item.Department, item.Teacher, item.Credits, item.Description, item.Capacity, item.Type, item.Term)
	return err
}

func (s *Store) persistAnnouncementLocked(item *Announcement) error {
	if s.db == nil || item == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `INSERT INTO cgu_announcements (id, title_zh, title_en, content_zh, content_en, type_name, audience_name, course_id, published_at, published_flag, author_name) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE title_zh=VALUES(title_zh), title_en=VALUES(title_en), content_zh=VALUES(content_zh), content_en=VALUES(content_en), type_name=VALUES(type_name), audience_name=VALUES(audience_name), course_id=VALUES(course_id), published_at=VALUES(published_at), published_flag=VALUES(published_flag), author_name=VALUES(author_name)`, item.ID, item.TitleZh, item.TitleEn, item.ContentZh, item.ContentEn, item.Type, item.Audience, item.CourseID, item.PublishedAt, item.Published, item.Author)
	return err
}

func (s *Store) persistAdmissionLocked(item *AdmissionApplication) error {
	if s.db == nil || item == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `INSERT INTO cgu_admissions (id, name_text, email, school_text, status_name, notes_text, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE name_text=VALUES(name_text), email=VALUES(email), school_text=VALUES(school_text), status_name=VALUES(status_name), notes_text=VALUES(notes_text), updated_at=VALUES(updated_at)`, item.ID, item.Name, item.Email, item.School, item.Status, item.Notes, item.CreatedAt, item.UpdatedAt)
	return err
}

// mailboxInsertSQL is intentionally a plain INSERT. request_key is a unique
// idempotency key, so an upsert here would overwrite a message when another
// process handles the same request before its local cache is populated.
const mailboxInsertSQL = `INSERT INTO cgu_mailbox_messages (id, recipient_id, sender_id, sender_name, subject_text, body_text, created_at, read_at, delivery_mode, external_recipient, delivery_status, delivery_error, delivered_at, request_key, delivery_started_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

func (s *Store) persistMailboxLocked(item *MailboxMessage) error {
	if s.db == nil || item == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	deliveryMode := strings.TrimSpace(item.DeliveryMode)
	if deliveryMode == "" {
		deliveryMode = mailboxDeliveryModeInternal
	}
	deliveryStatus := strings.TrimSpace(item.DeliveryStatus)
	if deliveryStatus == "" {
		deliveryStatus = mailboxDeliveryInternal
	}
	_, err := s.db.ExecContext(ctx, mailboxInsertSQL, item.ID, item.RecipientID, item.SenderID, item.SenderName, item.Subject, item.Body, item.CreatedAt, nullableString(item.ReadAt), deliveryMode, item.ExternalRecipient, deliveryStatus, item.DeliveryError, item.DeliveredAt, nullableString(item.RequestKey), item.DeliveryStartedAt)
	return err
}

func (s *Store) persistMailboxReadLocked(item *MailboxMessage) error {
	if s.db == nil || item == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `UPDATE cgu_mailbox_messages SET read_at = ? WHERE id = ?`, nullableString(item.ReadAt), item.ID)
	return err
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func (s *Store) persistSiteContentLocked(item *SiteContent) error {
	if s.db == nil || item == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `INSERT INTO cgu_site_content (content_key, zh_text, en_text) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE zh_text=VALUES(zh_text), en_text=VALUES(en_text)`, item.Key, item.Zh, item.En)
	return err
}

func (s *Store) deleteCoursePersistedLocked(id string) error {
	if s.db == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, statement := range []string{
		`DELETE FROM cgu_courses WHERE id = ?`,
		`DELETE FROM cgu_enrollments WHERE course_id = ?`,
		`DELETE FROM cgu_grades WHERE course_id = ?`,
		`DELETE FROM cgu_schedule WHERE course_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, statement, id); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) deleteAnnouncementPersistedLocked(id string) error {
	if s.db == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `DELETE FROM cgu_announcements WHERE id = ?`, id)
	return err
}

func (s *Store) deleteAdmissionPersistedLocked(id string) error {
	if s.db == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `DELETE FROM cgu_admissions WHERE id = ?`, id)
	return err
}
