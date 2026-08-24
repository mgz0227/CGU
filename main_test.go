package main

import (
	"bytes"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	testAdminUsername = "initial-admin"
	testAdminPassword = "test-admin-password-2026!"
)

func TestAcademicHTTPFlow(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	server := httptest.NewServer(NewServer(store, "web"))
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

	unauthorizedBody, err := json.Marshal(map[string]string{"key": "home.heroTitleLead", "zh": "不应保存"})
	if err != nil {
		t.Fatal(err)
	}
	unauthorizedRequest, err := http.NewRequest(http.MethodPut, server.URL+"/api/admin/site-content", bytes.NewReader(unauthorizedBody))
	if err != nil {
		t.Fatal(err)
	}
	unauthorizedRequest.Header.Set("Content-Type", "application/json")
	unauthorizedRequest.Header.Set("X-CGU-Request", "1")
	unauthorizedResponse, err := client.Do(unauthorizedRequest)
	if err != nil {
		t.Fatal(err)
	}
	if unauthorizedResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous site content update status = %d", unauthorizedResponse.StatusCode)
	}
	unauthorizedResponse.Body.Close()

	login := postJSON(t, client, server.URL+"/api/auth/login", map[string]string{"username": "student", "password": "removed-account-password"})
	if login.StatusCode != http.StatusUnauthorized {
		t.Fatalf("removed student account login status = %d", login.StatusCode)
	}
	login.Body.Close()

	login = postJSON(t, client, server.URL+"/api/auth/login", map[string]string{"username": testAdminUsername, "password": testAdminPassword})
	if login.StatusCode != http.StatusOK {
		t.Fatalf("administrator login status = %d", login.StatusCode)
	}
	login.Body.Close()

	publicContent, err := client.Get(server.URL + "/api/site-content")
	if err != nil {
		t.Fatal(err)
	}
	if publicContent.StatusCode != http.StatusOK {
		t.Fatalf("public site content status = %d", publicContent.StatusCode)
	}
	var publicPayload struct {
		Content []SiteContent `json:"content"`
	}
	if err := json.NewDecoder(publicContent.Body).Decode(&publicPayload); err != nil {
		t.Fatal(err)
	}
	publicContent.Body.Close()
	if len(publicPayload.Content) < 30 {
		t.Fatalf("public content catalog has %d entries, want managed copy and asset entries", len(publicPayload.Content))
	}

	contentBody, err := json.Marshal(map[string]string{"key": "home.heroTitleLead", "zh": "新的中文标题", "en": "A new English title"})
	if err != nil {
		t.Fatal(err)
	}
	contentRequest, err := http.NewRequest(http.MethodPut, server.URL+"/api/admin/site-content", bytes.NewReader(contentBody))
	if err != nil {
		t.Fatal(err)
	}
	contentRequest.Header.Set("Content-Type", "application/json")
	contentRequest.Header.Set("X-CGU-Request", "1")
	contentResponse, err := client.Do(contentRequest)
	if err != nil {
		t.Fatal(err)
	}
	if contentResponse.StatusCode != http.StatusOK {
		t.Fatalf("site content update status = %d", contentResponse.StatusCode)
	}
	contentResponse.Body.Close()
	if got := store.siteContent["home.heroTitleLead"]; got == nil || got.Zh != "新的中文标题" || got.En != "A new English title" {
		t.Fatalf("site content update was not stored: %#v", got)
	}

	for _, endpoint := range []string{"/api/auth/me", "/api/courses", "/api/enrollments", "/api/grades", "/api/schedule", "/api/announcements", "/api/admin/stats"} {
		response, err = client.Get(server.URL + endpoint)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("administrator %s status = %d", endpoint, response.StatusCode)
		}
		response.Body.Close()
	}

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
	csrfRequest.Header.Set("Origin", "https://foreign.example")
	csrfRequest.Header.Set("X-Forwarded-Proto", "https")
	csrfResponse, err := client.Do(csrfRequest)
	if err != nil {
		t.Fatal(err)
	}
	if csrfResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign origin status = %d", csrfResponse.StatusCode)
	}
	csrfResponse.Body.Close()

	login = postJSON(t, client, server.URL+"/api/auth/login", map[string]string{"username": testAdminUsername, "password": testAdminPassword})
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

func TestOriginAllowedBehindTLSProxy(t *testing.T) {
	server := NewServer(NewStoreWithAdmin(testAdminUsername, testAdminPassword), "web")
	request := httptest.NewRequest(http.MethodPost, "http://internal-cgu/api/auth/login", strings.NewReader(`{}`))
	request.Host = "cgu.edu.kg"
	request.Header.Set("Origin", "https://cgu.edu.kg")
	request.Header.Set("X-CGU-Request", "1")
	if server.originAllowed(request) {
		t.Fatal("HTTPS origin must require publicOrigin when TLS terminates before Go")
	}
	server.publicOrigin = "https://cgu.edu.kg"
	if !server.originAllowed(request) {
		t.Fatal("configured public origin should be accepted when TLS terminates before Go")
	}
	server.publicOrigin = ""
	request.Host = "cgu.edu.kg:443"
	request.TLS = &tls.ConnectionState{}
	if !server.originAllowed(request) {
		t.Fatal("direct HTTPS Host port should match the canonical origin")
	}
	request.TLS = nil
	request.Header.Set("Origin", "http://cgu.edu.kg")
	if server.originAllowed(request) {
		t.Fatal("HTTP listener must not treat an explicit HTTPS port as its default port")
	}
	request.Host = "cgu.edu.kg:80"
	if !server.originAllowed(request) {
		t.Fatal("direct HTTP Host port should match the canonical origin")
	}
	request.Host = "cgu.edu.kg"

	request.Header.Set("Origin", "https://foreign.example")
	if server.originAllowed(request) {
		t.Fatal("foreign origin must remain rejected")
	}

	server.publicOrigin = "https://cgu.edu.kg"
	request.Host = "internal-cgu:8000"
	request.Header.Set("Origin", "https://cgu.edu.kg/")
	if !server.originAllowed(request) {
		t.Fatal("configured public origin should be accepted independently of upstream Host")
	}
	request.Header.Set("Origin", "https://internal-cgu:8000")
	if server.originAllowed(request) {
		t.Fatal("configured public origin must reject the upstream Host origin")
	}
}

func TestTLSProxyLoginAndPreflightWithPublicOrigin(t *testing.T) {
	server := NewServer(NewStoreWithAdmin(testAdminUsername, testAdminPassword), "web")
	server.publicOrigin = "https://cgu.edu.kg"

	loginRequest := httptest.NewRequest(http.MethodPost, "http://upstream/api/auth/login", strings.NewReader(`{"username":"initial-admin","password":"test-admin-password-2026!"}`))
	loginRequest.Host = "upstream:8000"
	loginRequest.Header.Set("Origin", "https://cgu.edu.kg")
	loginRequest.Header.Set("X-CGU-Request", "1")
	loginResponse := httptest.NewRecorder()
	server.ServeHTTP(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("TLS proxy login status = %d, body = %s", loginResponse.Code, loginResponse.Body.String())
	}
	if cookie := loginResponse.Header().Get("Set-Cookie"); !strings.Contains(cookie, "HttpOnly") || !strings.Contains(cookie, "SameSite=Lax") {
		t.Fatalf("login cookie missing session protections: %q", cookie)
	}

	optionsRequest := httptest.NewRequest(http.MethodOptions, "http://upstream/api/auth/login", nil)
	optionsRequest.Host = "upstream:8000"
	optionsRequest.Header.Set("Origin", "https://cgu.edu.kg")
	optionsResponse := httptest.NewRecorder()
	server.ServeHTTP(optionsResponse, optionsRequest)
	if optionsResponse.Code != http.StatusNoContent || optionsResponse.Header().Get("Access-Control-Allow-Origin") != "https://cgu.edu.kg" {
		t.Fatalf("preflight = status %d, allow-origin %q", optionsResponse.Code, optionsResponse.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestNormalizeOrigin(t *testing.T) {
	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		{input: "https://CGU.edu.kg/", want: "https://cgu.edu.kg", ok: true},
		{input: "http://127.0.0.1:8000", want: "http://127.0.0.1:8000", ok: true},
		{input: "https://cgu.edu.kg/login", ok: false},
		{input: "https://cgu.edu.kg?next=/", ok: false},
		{input: "https://cgu.edu.kg?", ok: false},
		{input: "javascript://cgu.edu.kg", ok: false},
		{input: "null", ok: false},
	}
	for _, test := range tests {
		got, ok := normalizeOrigin(test.input)
		if ok != test.ok || got != test.want {
			t.Errorf("normalizeOrigin(%q) = (%q, %t), want (%q, %t)", test.input, got, ok, test.want, test.ok)
		}
	}
}

func TestSecurityGuards(t *testing.T) {
	server := httptest.NewServer(NewServer(NewStoreWithAdmin(testAdminUsername, testAdminPassword), "web"))
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
	server := httptest.NewServer(NewServer(NewStoreWithAdmin(testAdminUsername, testAdminPassword), "web"))
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

func TestStoreContainsOnlyConfiguredBootstrapAdmin(t *testing.T) {
	store := NewStoreWithAdmin("registrar", "long-test-password-2026!")
	if len(store.users) != 1 {
		t.Fatalf("store contains %d users, want exactly one bootstrap administrator", len(store.users))
	}
	admin, ok := store.users["admin"]
	if !ok || admin.Username != "registrar" || admin.Role != "admin" {
		t.Fatalf("unexpected bootstrap administrator: %#v", admin)
	}
	if _, ok := store.users["student"]; ok {
		t.Fatal("legacy student account must not be seeded")
	}
}

func TestStoreWithoutPasswordContainsNoLoginAccount(t *testing.T) {
	store := NewStoreWithAdmin("admin", "")
	if len(store.users) != 0 {
		t.Fatalf("empty bootstrap password created %d users", len(store.users))
	}
}

func TestSiteContentUpdateRollsBackWhenDatabaseWriteFails(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	original := *store.siteContent["home.heroTitleLead"]
	db, err := sql.Open("mysql", "cgu:cgu@tcp(127.0.0.1:1)/cgu")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store.db = db

	updated, apiError := store.updateSiteContent(SiteContentInput{Key: original.Key, Zh: "不应保存", En: "Must not persist"})
	if updated != nil || apiError == nil || apiError.Status != http.StatusServiceUnavailable {
		t.Fatalf("update result = %#v, error = %#v", updated, apiError)
	}
	got := store.siteContent[original.Key]
	if got == nil || got.Zh != original.Zh || got.En != original.En {
		t.Fatalf("failed database write changed in-memory content: %#v", got)
	}
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
