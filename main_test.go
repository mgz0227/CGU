package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
)

func TestAcademicHTTPFlow(t *testing.T) {
	server := httptest.NewServer(NewServer(NewStore(), "wwwroot"))
	defer server.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}

	response, err := client.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", response.StatusCode)
	}
	response.Body.Close()
	response, err = client.Get(server.URL + "/api/healthz")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("api health status = %d", response.StatusCode)
	}
	response.Body.Close()

	response, err = client.Get(server.URL + "/api/enrollments")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous enrollment status = %d", response.StatusCode)
	}
	response.Body.Close()

	login := postJSON(t, client, server.URL+"/api/auth/login", map[string]string{"username": "student", "password": "student-demo"})
	if login.StatusCode != http.StatusOK {
		t.Fatalf("student login status = %d", login.StatusCode)
	}
	login.Body.Close()

	for _, endpoint := range []string{"/api/auth/me", "/api/courses", "/api/enrollments", "/api/grades", "/api/schedule", "/api/announcements"} {
		response, err = client.Get(server.URL + endpoint)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("student %s status = %d", endpoint, response.StatusCode)
		}
		response.Body.Close()
	}

	response, err = client.Get(server.URL + "/api/admin/stats")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("student admin status = %d", response.StatusCode)
	}
	response.Body.Close()

	response, err = client.Post(server.URL+"/api/auth/logout", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()

	csrfRequest, err := http.NewRequest(http.MethodPost, server.URL+"/api/auth/logout", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	csrfRequest.Header.Set("Content-Type", "application/json")
	csrfRequest.Header.Set("Origin", "https://"+csrfRequest.Host)
	csrfRequest.Header.Set("X-Forwarded-Proto", "https")
	csrfResponse, err := client.Do(csrfRequest)
	if err != nil {
		t.Fatal(err)
	}
	if csrfResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("spoofed forwarded-proto origin status = %d", csrfResponse.StatusCode)
	}
	csrfResponse.Body.Close()

	login = postJSON(t, client, server.URL+"/api/auth/login", map[string]string{"username": "admin", "password": "admin-demo"})
	if login.StatusCode != http.StatusOK {
		t.Fatalf("admin login status = %d", login.StatusCode)
	}
	login.Body.Close()

	created := postJSON(t, client, server.URL+"/api/admin/courses", map[string]any{
		"code": "TEST101", "nameZh": "测试课程", "nameEn": "Test Course", "teacher": "CGU", "credits": 2, "capacity": 20, "term": "2026-秋",
	})
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("course create status = %d", created.StatusCode)
	}
	var payload struct {
		Course Course `json:"course"`
	}
	if err := json.NewDecoder(created.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	created.Body.Close()
	if payload.Course.ID == "" {
		t.Fatal("created course has no id")
	}

	request, err := http.NewRequest(http.MethodDelete, server.URL+"/api/admin/courses/"+payload.Course.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("course delete status = %d", response.StatusCode)
	}
	response.Body.Close()
}

func postJSON(t *testing.T, client *http.Client, endpoint string, value any) *http.Response {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return response
}
