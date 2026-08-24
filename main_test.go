package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAcademicHTTPFlow(t *testing.T) {
	server := httptest.NewServer(NewServer(NewStore(), "web"))
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

	logoutRequest, err := http.NewRequest(http.MethodPost, server.URL+"/api/auth/logout", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	logoutRequest.Header.Set("Content-Type", "application/json")
	logoutRequest.Header.Set("X-CGU-Request", "1")
	response, err = client.Do(logoutRequest)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()

	missingHeader, err := http.NewRequest(http.MethodPost, server.URL+"/api/auth/logout", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	missingHeader.Header.Set("Content-Type", "application/json")
	missingHeaderResponse, err := client.Do(missingHeader)
	if err != nil {
		t.Fatal(err)
	}
	if missingHeaderResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("missing request header status = %d", missingHeaderResponse.StatusCode)
	}
	missingHeaderResponse.Body.Close()

	csrfRequest, err := http.NewRequest(http.MethodPost, server.URL+"/api/auth/logout", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	csrfRequest.Header.Set("Content-Type", "application/json")
	csrfRequest.Header.Set("X-CGU-Request", "1")
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
	request.Header.Set("X-CGU-Request", "1")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("course delete status = %d", response.StatusCode)
	}
	response.Body.Close()
}

func TestSecurityGuards(t *testing.T) {
	server := httptest.NewServer(NewServer(NewStore(), "web"))
	defer server.Close()

	response, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("homepage status = %d", response.StatusCode)
	}
	if response.Header.Get("Content-Security-Policy") == "" || response.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("security headers missing: %#v", response.Header)
	}
	response.Body.Close()

	for attempt := 0; attempt < loginMaxFails; attempt++ {
		request, requestErr := http.NewRequest(http.MethodPost, server.URL+"/api/auth/login", strings.NewReader(`{"username":"rate-test","password":"wrong"}`))
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CGU-Request", "1")
		response, err = http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("failed login attempt %d status = %d", attempt+1, response.StatusCode)
		}
		response.Body.Close()
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/auth/login", strings.NewReader(`{"username":"rate-test","password":"wrong"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CGU-Request", "1")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusTooManyRequests || response.Header.Get("Retry-After") == "" {
		t.Fatalf("rate limit response = %d, retry-after = %q", response.StatusCode, response.Header.Get("Retry-After"))
	}
	response.Body.Close()

	if !strings.HasPrefix(hashPassword("test-password"), "bcrypt$") || !verifyPassword("test-password", hashPassword("test-password")) {
		t.Fatal("bcrypt password hashing/verification failed")
	}
}

func TestStaticRoutes(t *testing.T) {
	server := httptest.NewServer(NewServer(NewStore(), "web"))
	defer server.Close()

	for _, route := range []string{"/", "/login", "/portal", "/admin", "/login.html", "/portal.html", "/admin.html"} {
		response, err := http.Get(server.URL + route)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("static route %s status = %d", route, response.StatusCode)
		}
		response.Body.Close()
	}
	response, err := http.Get(server.URL + "/does-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown static route status = %d", response.StatusCode)
	}
	response.Body.Close()
}

func TestLoopbackListenerDetection(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:8000", "[::1]:8000", "localhost:8000"} {
		if !isLoopbackListenAddress(addr) {
			t.Fatalf("expected loopback address: %s", addr)
		}
	}
	for _, addr := range []string{"0.0.0.0:8000", ":8000", "192.0.2.1:8000", "invalid"} {
		if isLoopbackListenAddress(addr) {
			t.Fatalf("expected non-loopback address: %s", addr)
		}
	}
}

func postJSON(t *testing.T, client *http.Client, endpoint string, value any) *http.Response {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CGU-Request", "1")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
