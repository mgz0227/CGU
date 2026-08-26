package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestNormalizeCourseEnglishMetadata(t *testing.T) {
	existing := &Course{
		ID: "course-existing", Code: "ENG-101", NameZh: "旧课程", NameEn: "Old Course",
		Department: "旧学院", DepartmentEn: "Old College", Teacher: "旧教师", TeacherEn: "Old Instructor",
		Description: "旧简介", DescriptionEn: "Old description", Term: "2026-秋", TermEn: "Autumn 2026",
		Type: "required", Credits: 3, Capacity: 30,
	}
	updated, apiErr := normalizeCourse(CourseInput{
		ID: existing.ID, NameZh: existing.NameZh, DepartmentEn: "New College", TeacherEn: "New Instructor",
		DescriptionEn: "New description", TermEn: "Spring 2027",
	}, existing)
	if apiErr != nil {
		t.Fatalf("normalize translated course: %#v", apiErr)
	}
	if updated.DepartmentEn != "New College" || updated.TeacherEn != "New Instructor" || updated.DescriptionEn != "New description" || updated.TermEn != "Spring 2027" {
		t.Fatalf("English metadata was not updated: %#v", updated)
	}

	cleared, apiErr := normalizeCourse(CourseInput{
		ID: existing.ID, NameZh: existing.NameZh,
		ClearFields: []string{"departmentEn", "teacherEn", "descriptionEn", "termEn"},
	}, existing)
	if apiErr != nil {
		t.Fatalf("normalize cleared course: %#v", apiErr)
	}
	if cleared.DepartmentEn != "" || cleared.TeacherEn != "" || cleared.DescriptionEn != "" || cleared.TermEn != "" {
		t.Fatalf("English clear fields were restored: %#v", cleared)
	}

	created, apiErr := normalizeCourse(CourseInput{
		Code: "FALLBACK-101", NameZh: "新课程", Department: "新学院", Teacher: "新教师",
		Description: "中文简介", Term: "2027-春",
	}, nil)
	if apiErr != nil {
		t.Fatalf("normalize fallback course: %#v", apiErr)
	}
	if created.DepartmentEn != created.Department || created.TeacherEn != created.Teacher || created.DescriptionEn != created.Description || created.TermEn != created.Term {
		t.Fatalf("missing English metadata did not receive deterministic fallback: %#v", created)
	}
}

func TestCourseEnglishMetadataJSONAndCSV(t *testing.T) {
	course := Course{
		ID: "course-json", Code: "JSON-101", NameZh: "双语课程", NameEn: "Bilingual Course",
		Department: "中文学院", DepartmentEn: "Chinese Studies", Teacher: "李老师", TeacherEn: "Professor Li",
		Description: "中文简介", DescriptionEn: "English description", Term: "2026-秋", TermEn: "Autumn 2026",
	}
	encoded, err := json.Marshal(course)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"departmentEn": "Chinese Studies", "teacherEn": "Professor Li", "descriptionEn": "English description", "termEn": "Autumn 2026",
	} {
		if got, ok := fields[key].(string); !ok || got != want {
			t.Fatalf("JSON field %s = %#v, want %q", key, fields[key], want)
		}
	}

	store := NewStoreWithAdmin("catalog-admin", "catalog-admin-password-2026!")
	csvData, err := store.catalogCSV("en")
	if err != nil {
		t.Fatal(err)
	}
	csvText := string(csvData)
	for _, header := range []string{"School (English)", "Teacher (English)", "Term (English)", "Description (English)"} {
		if !strings.Contains(csvText, header) {
			t.Fatalf("catalog CSV is missing %q: %s", header, csvText[:minCourseTest(len(csvText), 500)])
		}
	}
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(csvText, "\ufeff")))
	header, err := reader.Read()
	if err != nil {
		t.Fatalf("read catalog header: %v", err)
	}
	indices := make(map[string]int, len(header))
	for index, name := range header {
		indices[name] = index
	}
	for _, name := range []string{"School (English)", "Teacher (English)", "Term (English)", "Description (English)"} {
		if _, ok := indices[name]; !ok {
			t.Fatalf("catalog header index missing %q", name)
		}
	}
	rows := 0
	for {
		record, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			t.Fatalf("read catalog row: %v", readErr)
		}
		rows++
		for _, name := range []string{"School (English)", "Teacher (English)", "Term (English)", "Description (English)"} {
			if strings.TrimSpace(record[indices[name]]) == "" {
				t.Fatalf("seed row has empty %s: %v", name, record)
			}
		}
	}
	if rows == 0 {
		t.Fatal("catalog CSV did not include seeded courses")
	}
}

func TestLoadAndPersistCourseEnglishMetadata(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	query := regexp.QuoteMeta("SELECT id, code, name_zh, name_en, department, department_en, teacher, teacher_en, credits, description, description_en, capacity, course_type, term_name, term_name_en FROM cgu_courses ORDER BY id")
	mock.ExpectQuery(query).WillReturnRows(sqlmock.NewRows([]string{
		"id", "code", "name_zh", "name_en", "department", "department_en", "teacher", "teacher_en", "credits", "description", "description_en", "capacity", "course_type", "term_name", "term_name_en",
	}).AddRow("course-db", "DB-101", "数据库", "Database", "计算学院", "School of Computing", "王老师", "Professor Wang", 3.0, "中文简介", "English description", 40, "required", "2026-秋", "Autumn 2026"))
	courses, err := loadCourses(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if len(courses) != 1 || courses[0].DepartmentEn != "School of Computing" || courses[0].TeacherEn != "Professor Wang" || courses[0].DescriptionEn != "English description" || courses[0].TermEn != "Autumn 2026" {
		t.Fatalf("loaded course English metadata = %#v", courses)
	}

	mock.ExpectExec(`(?s)INSERT INTO cgu_courses .*department_en.*teacher_en.*description_en.*term_name_en`).
		WithArgs("course-db", "DB-101", "数据库", "Database", "计算学院", "School of Computing", "王老师", "Professor Wang", 3.0, "中文简介", "English description", 40, "required", "2026-秋", "Autumn 2026").
		WillReturnResult(sqlmock.NewResult(1, 1))
	store := &Store{db: db}
	if err := store.persistCourseLocked(courses[0]); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func minCourseTest(value, limit int) int {
	if value < limit {
		return value
	}
	return limit
}
