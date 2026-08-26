package main

import (
	"net/http"
	"strings"
	"testing"
)

func addAcademicTestStudent(store *Store) *User {
	student := &User{
		ID: "student-academic-integrity", Username: "academic-integrity",
		Name: "学务测试学生", Email: "academic-integrity@example.com",
		Role: "student", StudentID: "CGU-ACADEMIC-1",
		PasswordHash: hashPassword("academic-integrity-password"),
	}
	store.users[student.ID] = student
	return student
}

func TestStudentGradesOnlyExposePublishedRecords(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	student := addAcademicTestStudent(store)
	store.grades = []*Grade{
		{ID: "grade-published", StudentID: student.ID, Status: "published"},
		{ID: "grade-graded", StudentID: student.ID, Status: "graded"},
		{ID: "grade-inprogress", StudentID: student.ID, Status: "inprogress"},
		{ID: "grade-withdrawn", StudentID: student.ID, Status: "withdrawn"},
		{ID: "grade-other", StudentID: "another-student", Status: "published"},
	}

	visible := store.gradesFor(student.ID, false)
	if len(visible) != 1 || visible[0].ID != "grade-published" {
		t.Fatalf("student grade projection = %#v, want only published own grade", visible)
	}
	adminView := store.gradesFor(student.ID, true)
	if len(adminView) != 4 {
		t.Fatalf("administrator filtered grade projection = %#v, want all four own records", adminView)
	}
	allAdminView := store.gradesFor("", true)
	if len(allAdminView) != 5 {
		t.Fatalf("administrator grade projection = %#v, want all records", allAdminView)
	}
}

func TestGradeScoreAndPointBounds(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	student := addAcademicTestStudent(store)
	courseID := store.courses[0].ID

	tests := []struct {
		name  string
		score any
		point any
	}{
		{name: "score below zero", score: -0.01, point: 2},
		{name: "score above one hundred", score: 100.01, point: 2},
		{name: "score not numeric", score: "not-a-number", point: 2},
		{name: "score nan", score: "NaN", point: 2},
		{name: "point below zero", score: 80, point: -0.01},
		{name: "point above four", score: 80, point: 4.01},
		{name: "point not numeric", score: 80, point: "unknown"},
		{name: "point infinity", score: 80, point: "Inf"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, apiError := store.createGrade(GradeInput{StudentID: student.ID, CourseID: courseID, Score: test.score, Point: test.point})
			if apiError == nil || apiError.Status != http.StatusBadRequest || apiError.Code != "invalid_input" {
				t.Fatalf("create grade error = %#v, want 400 invalid_input", apiError)
			}
		})
	}

	valid, apiError := store.createGrade(GradeInput{StudentID: student.ID, CourseID: courseID, Score: "", Point: ""})
	if apiError != nil || valid == nil || valid.Score != "" || valid.Point != "" {
		t.Fatalf("empty academic values should be accepted: grade=%#v error=%#v", valid, apiError)
	}
	valid, apiError = store.createGrade(GradeInput{StudentID: student.ID, CourseID: courseID, Score: 0, Point: 4})
	if apiError != nil || valid == nil || valid.Score != "0" || valid.Point != "4" {
		t.Fatalf("boundary academic values should be accepted: grade=%#v error=%#v", valid, apiError)
	}
}

func TestScheduleRejectsOverlappingClassesForSameStudent(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	student := addAcademicTestStudent(store)
	first, apiError := store.createSchedule(ScheduleInput{
		StudentID: student.ID, CourseID: store.courses[0].ID, Day: intPtr(1), Start: "09:00", End: "10:00",
	})
	if apiError != nil || first == nil {
		t.Fatalf("first schedule create = %#v error=%#v", first, apiError)
	}

	_, apiError = store.createSchedule(ScheduleInput{
		StudentID: student.ID, CourseID: store.courses[1].ID, Day: intPtr(1), Start: "09:30", End: "11:00",
	})
	if apiError == nil || apiError.Status != http.StatusConflict || apiError.Code != "schedule_overlap" {
		t.Fatalf("overlapping schedule error = %#v, want 409 schedule_overlap", apiError)
	}

	adjacent, apiError := store.createSchedule(ScheduleInput{
		StudentID: student.ID, CourseID: store.courses[1].ID, Day: intPtr(1), Start: "10:00", End: "11:00",
	})
	if apiError != nil || adjacent == nil {
		t.Fatalf("adjacent schedule should be allowed: item=%#v error=%#v", adjacent, apiError)
	}

	otherDay, apiError := store.createSchedule(ScheduleInput{
		StudentID: student.ID, CourseID: store.courses[2].ID, Day: intPtr(2), Start: "09:30", End: "11:00",
	})
	if apiError != nil || otherDay == nil {
		t.Fatalf("same time on another day should be allowed: item=%#v error=%#v", otherDay, apiError)
	}

	updated, apiError := store.updateSchedule(adjacent.ID, ScheduleInput{Day: intPtr(1), Start: "09:45", End: "10:30"})
	if apiError == nil || apiError.Status != http.StatusConflict || apiError.Code != "schedule_overlap" || updated != nil {
		t.Fatalf("overlapping schedule update = item=%#v error=%#v", updated, apiError)
	}
	for _, item := range store.schedule {
		if item != nil && strings.EqualFold(item.ID, adjacent.ID) && (item.Start != "10:00" || item.End != "11:00") {
			t.Fatalf("failed update mutated schedule: %#v", item)
		}
	}
}
