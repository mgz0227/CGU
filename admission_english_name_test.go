package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicAdmissionRequiresEnglishName(t *testing.T) {
	server := httptest.NewServer(NewServer(NewStoreWithAdmin(testAdminUsername, testAdminPassword), "web"))
	defer server.Close()
	response := postJSON(t, &http.Client{}, server.URL+"/api/admissions", map[string]string{
		"name": "申请人", "email": "required@example.com", "school": "综合学院",
	})
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing EnglishName status = %d", response.StatusCode)
	}
	response.Body.Close()
}

func TestNormalizeAdmissionEnglishNameAndRegistrarIdentity(t *testing.T) {
	input := AdmissionApplicationInput{
		Name: "旅行者", EnglishName: "Jean-Luc O'Connor", Email: "jean@example.com", School: "至冬学院",
	}
	item, apiError := normalizeAdmission(input, nil)
	if apiError != nil || item == nil {
		t.Fatalf("normalize English name = %#v, error = %#v", item, apiError)
	}
	if item.EnglishName != "Jean-Luc O'Connor" {
		t.Fatalf("EnglishName = %q", item.EnglishName)
	}
	if token := admissionEnglishToken(item); token != "JEANLUCOCONNOR" {
		t.Fatalf("registrar English token = %q", token)
	}
	studentID, _, _ := admissionStudentIdentityForApplication(item)
	if !strings.HasPrefix(studentID, "CGU-JEANLUCOCONNOR-") {
		t.Fatalf("student ID did not use EnglishName: %q", studentID)
	}
}

func TestNormalizeAdmissionEnglishNameAcceptsLegacyAliasAndPreservesOldRows(t *testing.T) {
	item, apiError := normalizeAdmission(AdmissionApplicationInput{
		Name: "申请人", EnglishNameSnake: "Lumine", Email: "lumine@example.com", School: "综合学院",
	}, nil)
	if apiError != nil || item == nil || item.EnglishName != "Lumine" {
		t.Fatalf("snake-case English name = %#v, error = %#v", item, apiError)
	}
	legacy := &AdmissionApplication{ID: "legacy", Name: "旧申请人", Email: "legacy@example.com", School: "综合学院", Status: "pending", EnglishName: "Legacy Name"}
	updated, apiError := normalizeAdmission(AdmissionApplicationInput{Notes: "已联系"}, legacy)
	if apiError != nil || updated == nil || updated.EnglishName != legacy.EnglishName {
		t.Fatalf("legacy EnglishName was not preserved: %#v, error = %#v", updated, apiError)
	}
}

func TestNormalizeAdmissionEnglishNameRejectsNonASCIIAndUnsafeValues(t *testing.T) {
	cases := []string{"张三", "Traveler2", "Traveler/Two", "---", strings.Repeat("A", 121)}
	for _, value := range cases {
		item, apiError := normalizeAdmission(AdmissionApplicationInput{
			Name: "申请人", EnglishName: value, Email: "applicant@example.com", School: "综合学院",
		}, nil)
		if item != nil || apiError == nil || apiError.Status != http.StatusBadRequest || apiError.Code != "invalid_input" {
			t.Errorf("EnglishName %q accepted unexpectedly: item=%#v error=%#v", value, item, apiError)
		}
	}
}
