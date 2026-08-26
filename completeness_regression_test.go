package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateStudentHonorsInitialActiveStateAndStats(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	active := false
	student, apiError := store.createStudent(StudentInput{
		Username:  "inactive-at-create",
		Name:      "待启用学生",
		StudentID: "CGU-INACTIVE-001",
		Password:  "inactive-student-password-2026!",
		Active:    &active,
	})
	if apiError != nil || student == nil {
		t.Fatalf("create disabled student = %#v, error=%#v", student, apiError)
	}
	if student.Active {
		t.Fatalf("new student unexpectedly active: %#v", student)
	}
	if got := store.stats()["students"]; got != 0 {
		t.Fatalf("disabled student counted in active statistics: %d", got)
	}
	if store.authenticate(student.Username, "inactive-student-password-2026!") != nil {
		t.Fatal("disabled student could authenticate")
	}
}

func TestCourseWithAcademicHistoryCannotBeDeleted(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	student, apiError := store.createStudent(StudentInput{
		Username:  "course-history-student",
		Name:      "课程记录学生",
		StudentID: "CGU-HISTORY-001",
		Password:  "course-history-password-2026!",
	})
	if apiError != nil || student == nil {
		t.Fatalf("create student = %#v, error=%#v", student, apiError)
	}
	course, apiError := store.createCourse(CourseInput{Code: "HISTORY-101", NameZh: "有历史课程", NameEn: "Course With History"})
	if apiError != nil || course == nil {
		t.Fatalf("create course = %#v, error=%#v", course, apiError)
	}
	if _, apiError = store.changeEnrollment(student.ID, course.ID, "enroll"); apiError != nil {
		t.Fatalf("create enrollment = %#v", apiError)
	}
	deleted, apiError := store.deleteCourse(course.ID)
	if deleted != nil || apiError == nil || apiError.Status != http.StatusConflict || apiError.Code != "course_in_use" {
		t.Fatalf("course deletion result = %#v, error=%#v", deleted, apiError)
	}
	found := false
	for _, candidate := range store.courses {
		if candidate != nil && candidate.ID == course.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("course was removed after rejected deletion")
	}
}

func TestPublicSectionAliasesServeManagedHomepage(t *testing.T) {
	server := httptest.NewServer(NewServer(NewStoreWithAdmin(testAdminUsername, testAdminPassword), "web"))
	defer server.Close()
	for _, route := range []string{"about", "programs", "admissions", "campus-life", "news", "contact"} {
		response, err := http.Get(server.URL + "/" + route)
		if err != nil {
			t.Fatalf("GET /%s: %v", route, err)
		}
		body, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil {
			t.Fatalf("read /%s: %v", route, readErr)
		}
		if response.StatusCode != http.StatusOK || !strings.Contains(string(body), "CGU") || !strings.Contains(string(body), "id=\"about\"") {
			t.Fatalf("public alias /%s response = status %d, body prefix %q", route, response.StatusCode, string(body)[:minInt(len(body), 120)])
		}
	}
}

func TestCourseBilingualFieldsFallbackAndPersistInMemory(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	course, apiError := store.createCourse(CourseInput{
		Code: "I18N-101", NameZh: "双语课程", NameEn: "Bilingual Course",
		Department: "综合学院", DepartmentEn: "School of Synthesis",
		Teacher: "教务联合授课", TeacherEn: "Registrar Faculty Team",
		Description: "中文课程简介", DescriptionEn: "English course description",
		Term: "2026-秋", TermEn: "Autumn 2026",
	})
	if apiError != nil || course == nil {
		t.Fatalf("create bilingual course = %#v, error=%#v", course, apiError)
	}
	if course.DepartmentEn != "School of Synthesis" || course.TeacherEn != "Registrar Faculty Team" || course.DescriptionEn != "English course description" || course.TermEn != "Autumn 2026" {
		t.Fatalf("bilingual metadata was not retained: %#v", course)
	}
	updated, apiError := store.updateCourse(course.ID, CourseInput{NameZh: course.NameZh, DescriptionEn: "Updated English description"})
	if apiError != nil || updated == nil || updated.DescriptionEn != "Updated English description" || updated.DepartmentEn != course.DepartmentEn {
		t.Fatalf("bilingual update = %#v, error=%#v", updated, apiError)
	}
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
