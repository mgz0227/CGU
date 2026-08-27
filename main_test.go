package main

import (
	"bytes"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
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

	login := postJSON(t, client, server.URL+"/api/auth/login", map[string]string{"username": testAdminUsername, "password": testAdminPassword})
	if login.StatusCode != http.StatusOK {
		t.Fatalf("administrator login status = %d, cookies=%v", login.StatusCode, login.Cookies())
	}
	if len(login.Cookies()) == 0 {
		t.Fatalf("administrator login did not set a session cookie")
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
	reset := doJSON(t, client, http.MethodPut, server.URL+"/api/admin/site-content", map[string]string{
		"key": "home.heroTitleLead", "zh": "", "en": "",
	})
	if reset.StatusCode != http.StatusOK {
		t.Fatalf("site content reset status = %d", reset.StatusCode)
	}
	reset.Body.Close()
	if got := store.siteContent["home.heroTitleLead"]; got == nil || got.Zh != "" || got.En != "" {
		t.Fatalf("site content reset was not persisted: %#v", got)
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

func TestAdmissionsApplicationFlow(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	server := httptest.NewServer(NewServer(store, "web"))
	defer server.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}

	created := postJSON(t, client, server.URL+"/api/admissions", map[string]string{
		"name": "璃月旅行者", "englishName": "Liyue Traveler", "email": "traveler@example.com", "school": "契约与商业文明", "status": "accepted", "notes": "伪造的内部备注",
	})
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("admission create status = %d", created.StatusCode)
	}
	var createdPayload struct {
		Application AdmissionApplication `json:"application"`
	}
	if err := json.NewDecoder(created.Body).Decode(&createdPayload); err != nil {
		t.Fatal(err)
	}
	created.Body.Close()
	if createdPayload.Application.ID == "" || createdPayload.Application.Status != "pending" || createdPayload.Application.Notes != "" {
		t.Fatalf("unexpected created application: %#v", createdPayload.Application)
	}
	if len(store.admissions) != 1 {
		t.Fatalf("in-memory admission count = %d, want 1", len(store.admissions))
	}
	privateList, err := client.Get(server.URL + "/api/admin/admissions")
	if err != nil {
		t.Fatal(err)
	}
	if privateList.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous admin admission list status = %d", privateList.StatusCode)
	}
	privateList.Body.Close()

	invalid := postJSON(t, client, server.URL+"/api/admissions", map[string]string{
		"name": "Invalid", "email": "not-an-email", "school": "School",
	})
	if invalid.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid admission status = %d", invalid.StatusCode)
	}
	invalid.Body.Close()

	login := postJSON(t, client, server.URL+"/api/auth/login", map[string]string{
		"username": testAdminUsername, "password": testAdminPassword,
	})
	if login.StatusCode != http.StatusOK {
		t.Fatalf("administrator login status = %d", login.StatusCode)
	}
	login.Body.Close()

	list, err := client.Get(server.URL + "/api/admin/admissions")
	if err != nil {
		t.Fatal(err)
	}
	if list.StatusCode != http.StatusOK {
		t.Fatalf("admin admission list status = %d", list.StatusCode)
	}
	var listPayload struct {
		Applications []AdmissionApplication `json:"applications"`
	}
	if err := json.NewDecoder(list.Body).Decode(&listPayload); err != nil {
		t.Fatal(err)
	}
	list.Body.Close()
	if len(listPayload.Applications) != 1 || listPayload.Applications[0].Email != "traveler@example.com" {
		t.Fatalf("unexpected admin applications: %#v", listPayload.Applications)
	}

	updateBody, err := json.Marshal(map[string]string{"notes": "已发送申请指南"})
	if err != nil {
		t.Fatal(err)
	}
	updateRequest, err := http.NewRequest(http.MethodPatch, server.URL+"/api/admin/admissions/"+url.PathEscape(createdPayload.Application.ID), bytes.NewReader(updateBody))
	if err != nil {
		t.Fatal(err)
	}
	updateRequest.Header.Set("Content-Type", "application/json")
	updateRequest.Header.Set("X-CGU-Request", "1")
	update, err := client.Do(updateRequest)
	if err != nil {
		t.Fatal(err)
	}
	if update.StatusCode != http.StatusOK {
		t.Fatalf("admin admission update status = %d", update.StatusCode)
	}
	update.Body.Close()
	if store.admissions[0].Status != "pending" || store.admissions[0].Notes != "已发送申请指南" {
		t.Fatalf("admission update not stored: %#v", store.admissions[0])
	}

	stats, err := client.Get(server.URL + "/api/admin/stats")
	if err != nil {
		t.Fatal(err)
	}
	var statsPayload struct {
		Stats map[string]int `json:"stats"`
	}
	if err := json.NewDecoder(stats.Body).Decode(&statsPayload); err != nil {
		t.Fatal(err)
	}
	stats.Body.Close()
	if statsPayload.Stats["admissions"] != 1 || statsPayload.Stats["pendingAdmissions"] != 1 {
		t.Fatalf("unexpected admission stats: %#v", statsPayload.Stats)
	}

	// A legacy status transition must not bypass the atomic approval action.
	updateBody, err = json.Marshal(map[string]string{"status": "reviewing"})
	if err != nil {
		t.Fatal(err)
	}
	updateRequest, err = http.NewRequest(http.MethodPatch, server.URL+"/api/admin/admissions/"+url.PathEscape(createdPayload.Application.ID), bytes.NewReader(updateBody))
	if err != nil {
		t.Fatal(err)
	}
	updateRequest.Header.Set("Content-Type", "application/json")
	updateRequest.Header.Set("X-CGU-Request", "1")
	update, err = client.Do(updateRequest)
	if err != nil {
		t.Fatal(err)
	}
	if update.StatusCode != http.StatusConflict {
		t.Fatalf("reviewing admission update status = %d", update.StatusCode)
	}
	update.Body.Close()
	stats, err = client.Get(server.URL + "/api/admin/stats")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(stats.Body).Decode(&statsPayload); err != nil {
		t.Fatal(err)
	}
	stats.Body.Close()
	if statsPayload.Stats["pendingAdmissions"] != 1 || statsPayload.Stats["pending"] < 1 {
		t.Fatalf("pending application missing from pending stats: %#v", statsPayload.Stats)
	}
}

func TestAdmissionRateLimit(t *testing.T) {
	server := httptest.NewServer(NewServer(NewStoreWithAdmin("admin", ""), "web"))
	defer server.Close()
	client := &http.Client{}
	for attempt := 0; attempt < admissionMax; attempt++ {
		response := postJSON(t, client, server.URL+"/api/admissions", map[string]string{
			"name": "申请人", "englishName": "Rate Applicant", "email": "rate@example.com", "school": "综合学院",
		})
		if response.StatusCode != http.StatusCreated {
			t.Fatalf("admission attempt %d status = %d", attempt+1, response.StatusCode)
		}
		response.Body.Close()
	}
	response := postJSON(t, client, server.URL+"/api/admissions", map[string]string{
		"name": "申请人", "englishName": "Rate Applicant", "email": "rate@example.com", "school": "综合学院",
	})
	if response.StatusCode != http.StatusTooManyRequests || response.Header.Get("Retry-After") == "" {
		t.Fatalf("admission rate limit status = %d, retry-after = %q", response.StatusCode, response.Header.Get("Retry-After"))
	}
	response.Body.Close()
}

func TestAdmissionCreateRollsBackWhenDatabaseWriteFails(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	db, err := sql.Open("mysql", "cgu:cgu@tcp(127.0.0.1:1)/cgu")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store.db = db

	item, apiError := store.createAdmission(AdmissionApplicationInput{Name: "申请人", Email: "rollback@example.com", School: "综合学院"})
	if item != nil || apiError == nil || apiError.Status != http.StatusServiceUnavailable {
		t.Fatalf("create result = %#v, error = %#v", item, apiError)
	}
	if len(store.admissions) != 0 {
		t.Fatalf("failed database write left %d in-memory applications", len(store.admissions))
	}
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

	const assetVersion = "v1.5.11"
	for _, route := range []string{"/", "/login", "/login/", "/portal", "/portal/", "/admin", "/admin/", "/calendar", "/calendar/", "/catalog", "/catalog/", "/login.html", "/portal.html", "/admin.html", "/calendar.html", "/catalog.html"} {
		response, err := http.Get(server.URL + route)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("static route %s status = %d", route, response.StatusCode)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		bodyText := string(body)
		if !strings.Contains(bodyText, "assets/"+assetVersion) {
			t.Fatalf("static route %s did not use %s assets", route, assetVersion)
		}
		if strings.Contains(bodyText, "assets/v1.5.8") || strings.Contains(bodyText, `href="/styles.css"`) {
			t.Fatalf("static route %s contains a stale or unversioned asset reference", route)
		}
	}
	for _, asset := range []string{"calendar.css", "calendar.js", "catalog.css", "catalog.js", "i18n.js", "portal.css", "portal.js", "script.js", "styles.css"} {
		versionedAsset, err := http.Get(server.URL + "/assets/" + assetVersion + "/" + asset)
		if err != nil {
			t.Fatal(err)
		}
		if versionedAsset.StatusCode != http.StatusOK || !strings.Contains(versionedAsset.Header.Get("Cache-Control"), "no-cache") {
			t.Fatalf("versioned asset %s status = %d, cache %q", asset, versionedAsset.StatusCode, versionedAsset.Header.Get("Cache-Control"))
		}
		versionedAsset.Body.Close()
	}
	asset, err := http.Get(server.URL + "/portal.js")
	if err != nil {
		t.Fatal(err)
	}
	if asset.StatusCode != http.StatusOK || !strings.Contains(asset.Header.Get("Cache-Control"), "no-cache") {
		t.Fatalf("portal asset cache policy = status %d, cache %q", asset.StatusCode, asset.Header.Get("Cache-Control"))
	}
	asset.Body.Close()
	response, err := http.Get(server.URL + "/does-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown static route status = %d", response.StatusCode)
	}
	response.Body.Close()
}

func TestConfiguredPublicOriginRedirectsWWWStaticRequests(t *testing.T) {
	server := NewServer(NewStoreWithAdmin(testAdminUsername, testAdminPassword), "web")
	server.publicOrigin = "https://cgu.example.test"
	request := httptest.NewRequest(http.MethodGet, "http://www.cgu.example.test/login?next=%2Fadmin", nil)
	request.Host = "www.cgu.example.test"
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusMovedPermanently {
		t.Fatalf("www redirect status = %d, want %d", recorder.Code, http.StatusMovedPermanently)
	}
	if got, want := recorder.Header().Get("Location"), "https://cgu.example.test/login?next=%2Fadmin"; got != want {
		t.Fatalf("www redirect location = %q, want %q", got, want)
	}
}

func TestPublicCourseCatalogDownload(t *testing.T) {
	server := httptest.NewServer(NewServer(NewStoreWithAdmin(testAdminUsername, testAdminPassword), "web"))
	defer server.Close()
	request, err := http.NewRequest(http.MethodGet, server.URL+"/api/catalog.csv?lang=en", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/csv") || !strings.Contains(response.Header.Get("Content-Disposition"), "cgu-course-catalog.csv") {
		t.Fatalf("catalog response = status %d headers %#v", response.StatusCode, response.Header)
	}
	if !bytes.Contains(content, []byte("ELM101")) || !bytes.Contains(content, []byte("Name (English)")) {
		t.Fatalf("catalog did not contain expected course data: %q", content)
	}
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

func TestStudentMailboxUsesConfiguredDomain(t *testing.T) {
	store := NewStoreWithAdminAndDomain("admin", "long-test-password-2026!", "student.cgu.edu.kg")
	student := &User{ID: "student-1", Username: "student-1", Role: "student", StudentID: "CGU-001"}
	profile := store.publicUser(student)
	if profile["studentEmail"] != "cgu-001@student.cgu.edu.kg" {
		t.Fatalf("student email = %v", profile["studentEmail"])
	}
	if got := studentMailbox("bad id", "student.cgu.edu.kg"); got != "" {
		t.Fatalf("invalid student id produced mailbox %q", got)
	}
}

func TestStudentLoginIdentifiersMustRemainUnique(t *testing.T) {
	store := NewStoreWithAdminAndDomain("admin", "long-test-password-2026!", "students.cgu.edu.kg")
	first, apiError := store.createStudent(StudentInput{
		Username: "student-one", Name: "Student One", Email: "contact-one@example.com",
		StudentID: "CGU-ONE", Password: "student-one-password!",
	})
	if apiError != nil || first == nil {
		t.Fatalf("first student = %#v, error = %#v", first, apiError)
	}

	second, apiError := store.createStudent(StudentInput{
		Username: "student-two", Name: "Student Two", Email: "cgu-one@students.cgu.edu.kg",
		StudentID: "CGU-TWO", Password: "student-two-password!",
	})
	if second != nil || apiError == nil || apiError.Status != http.StatusConflict {
		t.Fatalf("conflicting student = %#v, error = %#v", second, apiError)
	}
}

func TestTrustedProxyRateAddress(t *testing.T) {
	server := NewServer(NewStoreWithAdmin("admin", "long-test-password-2026!"), "web")
	if err := server.setTrustedProxies([]string{"10.0.0.0/8"}); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "http://cgu.test/api/admissions", nil)
	request.RemoteAddr = "10.20.30.40:443"
	request.Header.Set("X-Forwarded-For", "198.51.100.22, 10.20.30.41")
	if got := server.clientRateAddress(request); got != "198.51.100.22" {
		t.Fatalf("trusted proxy client address = %q", got)
	}

	request.RemoteAddr = "192.0.2.10:443"
	if got := server.clientRateAddress(request); got != "192.0.2.10" {
		t.Fatalf("untrusted proxy client address = %q", got)
	}

	request.RemoteAddr = "10.20.30.40:443"
	request.Header.Set("X-Forwarded-For", "not-an-address")
	if got := server.clientRateAddress(request); got != "10.20.30.40" {
		t.Fatalf("malformed forwarded address = %q", got)
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

func TestAdminCourseAndAnnouncementKeepEditableFields(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	course, apiError := store.createCourse(CourseInput{
		Code: "EDIT-101", NameZh: "可编辑课程", NameEn: "Editable Course", Department: "至冬研究学院",
		Teacher: "测试教师", Term: "2026-秋", Credits: floatPtr(3), Capacity: intPtr(24), Type: "required",
	})
	if apiError != nil || course == nil || course.Department != "至冬研究学院" {
		t.Fatalf("course department was not stored: course=%#v error=%#v", course, apiError)
	}
	updatedCourse, apiError := store.updateCourse(course.ID, CourseInput{NameZh: course.NameZh, Department: "枫丹工程学院"})
	if apiError != nil || updatedCourse == nil || updatedCourse.Department != "枫丹工程学院" {
		t.Fatalf("course department was not editable: course=%#v error=%#v", updatedCourse, apiError)
	}
	announcement, apiError := store.createAnnouncement(AnnouncementInput{
		TitleZh: "双语公告", TitleEn: "Bilingual announcement", ContentZh: "中文正文", ContentEn: "English body", Published: boolPtr(false),
	})
	if apiError != nil || announcement == nil || announcement.ContentEn != "English body" || announcement.Published {
		t.Fatalf("announcement fields were not stored: announcement=%#v error=%#v", announcement, apiError)
	}
	updatedAnnouncement, apiError := store.updateAnnouncement(announcement.ID, AnnouncementInput{ContentEn: "Updated English body", Published: boolPtr(true)})
	if apiError != nil || updatedAnnouncement == nil || updatedAnnouncement.ContentEn != "Updated English body" || !updatedAnnouncement.Published {
		t.Fatalf("announcement fields were not editable: announcement=%#v error=%#v", updatedAnnouncement, apiError)
	}
}

func TestEditableCourseAndAnnouncementFieldsCanBeCleared(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	course, apiError := store.createCourse(CourseInput{
		Code: "CLEAR-101", NameZh: "可清空课程", NameEn: "Clearable Course", Department: "至冬研究学院",
		Teacher: "测试教师", Description: "课程说明", Term: "2026-秋", Type: "required",
	})
	if apiError != nil || course == nil {
		t.Fatalf("create clearable course: course=%#v error=%#v", course, apiError)
	}
	clearedCourse, apiError := store.updateCourse(course.ID, CourseInput{
		NameZh:      course.NameZh,
		ClearFields: []string{"nameEn", "department", "teacher", "description", "term", "type"},
	})
	if apiError != nil || clearedCourse == nil {
		t.Fatalf("clear course fields: course=%#v error=%#v", clearedCourse, apiError)
	}
	if clearedCourse.NameEn != "" || clearedCourse.Department != "" || clearedCourse.Teacher != "" || clearedCourse.Description != "" || clearedCourse.Term != "" || clearedCourse.Type != "" {
		t.Fatalf("course clear fields were restored/defaulted: %#v", clearedCourse)
	}

	announcement, apiError := store.createAnnouncement(AnnouncementInput{
		TitleZh: "可清空公告", TitleEn: "Clearable notice", ContentZh: "中文内容", ContentEn: "English content", Type: "RESEARCH",
	})
	if apiError != nil || announcement == nil {
		t.Fatalf("create clearable announcement: announcement=%#v error=%#v", announcement, apiError)
	}
	clearedAnnouncement, apiError := store.updateAnnouncement(announcement.ID, AnnouncementInput{
		TitleZh: announcement.TitleZh, ContentZh: announcement.ContentZh,
		ClearFields: []string{"titleEn", "contentEn", "type", "publishedAt"},
	})
	if apiError != nil || clearedAnnouncement == nil {
		t.Fatalf("clear announcement fields: announcement=%#v error=%#v", clearedAnnouncement, apiError)
	}
	if clearedAnnouncement.TitleEn != "" || clearedAnnouncement.ContentEn != "" || clearedAnnouncement.Type != "" || clearedAnnouncement.PublishedAt != "" {
		t.Fatalf("announcement clear fields were restored/defaulted: %#v", clearedAnnouncement)
	}
}

func TestAdmissionNotesAreEditableWithoutChangingDecision(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	application, apiError := store.createAdmission(AdmissionApplicationInput{Name: "备注申请人", Email: "notes@example.com", School: "综合学院"})
	if apiError != nil {
		t.Fatalf("create admission: %v", apiError)
	}
	updated, apiError := store.updateAdmission(application.ID, AdmissionApplicationInput{Notes: "已完成首次联系"})
	if apiError != nil || updated == nil || updated.Notes != "已完成首次联系" || updated.Status != "pending" {
		t.Fatalf("admission notes update = %#v, error=%#v", updated, apiError)
	}
	cleared, apiError := store.updateAdmission(application.ID, AdmissionApplicationInput{ClearNotes: boolPtr(true)})
	if apiError != nil || cleared == nil || cleared.Notes != "" || cleared.Status != "pending" {
		t.Fatalf("admission notes clear = %#v, error=%#v", cleared, apiError)
	}
}

func TestLegacyAcceptedAdmissionCanBeEditedBeforeProvisioning(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	application, apiError := store.createAdmission(AdmissionApplicationInput{Name: "旧申请人", Email: "legacy@example.com", School: "综合学院"})
	if apiError != nil {
		t.Fatalf("create admission: %v", apiError)
	}
	store.admissions[0].Status = "accepted"
	updated, apiError := store.updateAdmission(application.ID, AdmissionApplicationInput{
		Name: "修正申请人", Email: "corrected@example.com", School: "至冬学院", Notes: "待补充材料",
	})
	if apiError != nil || updated == nil || updated.Name != "修正申请人" || updated.Email != "corrected@example.com" || updated.School != "至冬研究与极地治理" || updated.Notes != "待补充材料" {
		t.Fatalf("legacy accepted admission update = %#v, error=%#v", updated, apiError)
	}
}

func TestAdmissionSchoolValuesAreCanonicalized(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	application, apiError := store.createAdmission(AdmissionApplicationInput{
		Name: "至冬申请人", Email: "polar@example.com", School: "polar",
	})
	if apiError != nil || application == nil || application.School != "至冬研究与极地治理" {
		t.Fatalf("stable school value was not canonicalized: application=%#v error=%#v", application, apiError)
	}
	english, apiError := store.createAdmission(AdmissionApplicationInput{
		Name: "English applicant", Email: "polar-en@example.com", School: "Snezhnaya studies & polar governance",
	})
	if apiError != nil || english == nil || english.School != "至冬研究与极地治理" {
		t.Fatalf("localized school value was not canonicalized: application=%#v error=%#v", english, apiError)
	}
}

func TestDisabledStudentCannotAuthenticateAndSessionsAreRevoked(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	server := httptest.NewServer(NewServer(store, "web"))
	defer server.Close()

	adminJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	adminClient := &http.Client{Jar: adminJar}
	login := postJSON(t, adminClient, server.URL+"/api/auth/login", map[string]string{"username": testAdminUsername, "password": testAdminPassword})
	if login.StatusCode != http.StatusOK {
		t.Fatalf("admin login status = %d", login.StatusCode)
	}
	login.Body.Close()
	created := postJSON(t, adminClient, server.URL+"/api/admin/students", map[string]string{
		"username": "disable-me", "name": "停用测试学生", "studentId": "CGU-DISABLE-001", "password": "disable-student-password-2026!",
	})
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("student create status = %d", created.StatusCode)
	}
	var createdPayload struct {
		Student AdminStudent `json:"student"`
	}
	if err := json.NewDecoder(created.Body).Decode(&createdPayload); err != nil {
		t.Fatal(err)
	}
	created.Body.Close()

	studentJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	studentClient := &http.Client{Jar: studentJar}
	studentLogin := postJSON(t, studentClient, server.URL+"/api/auth/login", map[string]string{"username": "disable-me", "password": "disable-student-password-2026!"})
	if studentLogin.StatusCode != http.StatusOK {
		t.Fatalf("student login status = %d", studentLogin.StatusCode)
	}
	studentLogin.Body.Close()

	disabled := doJSON(t, adminClient, http.MethodPatch, server.URL+"/api/admin/students/"+url.PathEscape(createdPayload.Student.ID), map[string]any{"active": false})
	if disabled.StatusCode != http.StatusOK {
		t.Fatalf("disable student status = %d", disabled.StatusCode)
	}
	var disabledPayload struct {
		Student AdminStudent `json:"student"`
	}
	if err := json.NewDecoder(disabled.Body).Decode(&disabledPayload); err != nil {
		t.Fatal(err)
	}
	disabled.Body.Close()
	if disabledPayload.Student.Active {
		t.Fatalf("disabled student projection remained active: %#v", disabledPayload.Student)
	}
	me, err := studentClient.Get(server.URL + "/api/auth/me")
	if err != nil {
		t.Fatal(err)
	}
	if me.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked student session status = %d", me.StatusCode)
	}
	me.Body.Close()
	newLogin := postJSON(t, &http.Client{}, server.URL+"/api/auth/login", map[string]string{"username": "disable-me", "password": "disable-student-password-2026!"})
	if newLogin.StatusCode != http.StatusUnauthorized {
		t.Fatalf("disabled student login status = %d", newLogin.StatusCode)
	}
	newLogin.Body.Close()

	enabled := doJSON(t, adminClient, http.MethodPatch, server.URL+"/api/admin/students/"+url.PathEscape(createdPayload.Student.ID), map[string]any{"active": true})
	if enabled.StatusCode != http.StatusOK {
		t.Fatalf("enable student status = %d", enabled.StatusCode)
	}
	enabled.Body.Close()
	loginAgain := postJSON(t, &http.Client{}, server.URL+"/api/auth/login", map[string]string{"username": "disable-me", "password": "disable-student-password-2026!"})
	if loginAgain.StatusCode != http.StatusOK {
		t.Fatalf("re-enabled student login status = %d", loginAgain.StatusCode)
	}
	loginAgain.Body.Close()
}

func TestAdminPasswordResetRevokesExistingStudentSessions(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	student, apiError := store.createStudent(StudentInput{
		Username: "password-reset-student", Name: "密码重置学生", Email: "password-reset@example.com",
		StudentID: "CGU-PASSWORD-RESET", Password: "old-password-reset-2026!",
	})
	if apiError != nil || student == nil {
		t.Fatalf("create password reset student = %#v, error = %#v", student, apiError)
	}
	server := httptest.NewServer(NewServer(store, "web"))
	defer server.Close()
	studentJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	studentClient := &http.Client{Jar: studentJar}
	login := postJSON(t, studentClient, server.URL+"/api/auth/login", map[string]string{
		"username": student.Username, "password": "old-password-reset-2026!",
	})
	if login.StatusCode != http.StatusOK {
		t.Fatalf("student login status = %d", login.StatusCode)
	}
	login.Body.Close()

	adminJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	adminClient := &http.Client{Jar: adminJar}
	adminLogin := postJSON(t, adminClient, server.URL+"/api/auth/login", map[string]string{
		"username": testAdminUsername, "password": testAdminPassword,
	})
	if adminLogin.StatusCode != http.StatusOK {
		t.Fatalf("admin login status = %d", adminLogin.StatusCode)
	}
	adminLogin.Body.Close()
	updated := doJSON(t, adminClient, http.MethodPatch, server.URL+"/api/admin/students/"+url.PathEscape(student.ID), map[string]string{
		"password": "new-password-reset-2026!",
	})
	if updated.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(updated.Body)
		updated.Body.Close()
		t.Fatalf("student password reset status = %d body=%s", updated.StatusCode, raw)
	}
	updated.Body.Close()
	me, err := studentClient.Get(server.URL + "/api/auth/me")
	if err != nil {
		t.Fatal(err)
	}
	if me.StatusCode != http.StatusUnauthorized {
		t.Fatalf("student session after administrator password reset status = %d", me.StatusCode)
	}
	me.Body.Close()
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

func TestAdminStudentAndAcademicMutations(t *testing.T) {
	store := NewStoreWithAdminAndDomain(testAdminUsername, testAdminPassword, "students.cgu.edu.kg")
	server := httptest.NewServer(NewServer(store, "web"))
	defer server.Close()

	adminJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	adminClient := &http.Client{Jar: adminJar}
	response, err := adminClient.Get(server.URL + "/api/admin/students")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous student directory status = %d", response.StatusCode)
	}
	response.Body.Close()

	login := postJSON(t, adminClient, server.URL+"/api/auth/login", map[string]string{"username": testAdminUsername, "password": testAdminPassword})
	if login.StatusCode != http.StatusOK {
		t.Fatalf("administrator login status = %d", login.StatusCode)
	}
	login.Body.Close()

	created := postJSON(t, adminClient, server.URL+"/api/admin/students", map[string]string{
		"username": "traveler-001", "name": "旅行者一号", "studentId": "CGU-2026-001", "college": "至冬与极地研究学院", "year": "2026", "password": "student-password-2026!",
	})
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("student create status = %d", created.StatusCode)
	}
	rawCreated, err := io.ReadAll(created.Body)
	if err != nil {
		t.Fatal(err)
	}
	created.Body.Close()
	if strings.Contains(strings.ToLower(string(rawCreated)), "passwordhash") || strings.Contains(strings.ToLower(string(rawCreated)), "password_hash") {
		t.Fatalf("student response leaked password hash: %s", rawCreated)
	}
	var createdPayload struct {
		Student AdminStudent `json:"student"`
	}
	if err := json.Unmarshal(rawCreated, &createdPayload); err != nil {
		t.Fatal(err)
	}
	student := createdPayload.Student
	if student.ID == "" || student.Role != "student" || student.StudentEmail != "cgu-2026-001@students.cgu.edu.kg" {
		t.Fatalf("unexpected student projection: %#v", student)
	}
	if store.users[student.ID] == nil || store.users[student.ID].PasswordHash == "" {
		t.Fatal("student password hash was not stored server-side")
	}

	list, err := adminClient.Get(server.URL + "/api/admin/students")
	if err != nil {
		t.Fatal(err)
	}
	if list.StatusCode != http.StatusOK {
		t.Fatalf("student directory status = %d", list.StatusCode)
	}
	var listPayload struct {
		Students []AdminStudent `json:"students"`
	}
	if err := json.NewDecoder(list.Body).Decode(&listPayload); err != nil {
		t.Fatal(err)
	}
	list.Body.Close()
	if len(listPayload.Students) != 1 || listPayload.Students[0].ID != student.ID {
		t.Fatalf("unexpected student directory: %#v", listPayload.Students)
	}

	updated := doJSON(t, adminClient, http.MethodPatch, server.URL+"/api/admin/students/"+url.PathEscape(student.ID), map[string]string{"name": "旅行者一号·更新", "password": "updated-student-password-2026!"})
	if updated.StatusCode != http.StatusOK {
		t.Fatalf("student update status = %d", updated.StatusCode)
	}
	updatedRaw, err := io.ReadAll(updated.Body)
	if err != nil {
		t.Fatal(err)
	}
	updated.Body.Close()
	if strings.Contains(strings.ToLower(string(updatedRaw)), "passwordhash") || strings.Contains(strings.ToLower(string(updatedRaw)), "password_hash") {
		t.Fatalf("student update leaked password hash: %s", updatedRaw)
	}
	if updatedUser := store.users[student.ID]; updatedUser == nil || !verifyPassword("updated-student-password-2026!", updatedUser.PasswordHash) {
		t.Fatal("student password was not rotated")
	}
	generatedEmailUpdate := doJSON(t, adminClient, http.MethodPatch, server.URL+"/api/admin/students/"+url.PathEscape(student.ID), map[string]string{"studentId": "CGU-2026-002"})
	if generatedEmailUpdate.StatusCode != http.StatusOK {
		t.Fatalf("student id update status = %d", generatedEmailUpdate.StatusCode)
	}
	var generatedEmailPayload struct {
		Student AdminStudent `json:"student"`
	}
	if err := json.NewDecoder(generatedEmailUpdate.Body).Decode(&generatedEmailPayload); err != nil {
		t.Fatal(err)
	}
	generatedEmailUpdate.Body.Close()
	if generatedEmailPayload.Student.StudentEmail != "cgu-2026-002@students.cgu.edu.kg" {
		t.Fatalf("generated student email did not follow student id: %#v", generatedEmailPayload.Student)
	}
	student = generatedEmailPayload.Student

	studentJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	studentClient := &http.Client{Jar: studentJar}
	studentLogin := postJSON(t, studentClient, server.URL+"/api/auth/login", map[string]string{"username": "traveler-001", "password": "updated-student-password-2026!"})
	if studentLogin.StatusCode != http.StatusOK {
		t.Fatalf("student login status = %d", studentLogin.StatusCode)
	}
	studentLogin.Body.Close()
	studentDirectory, err := studentClient.Get(server.URL + "/api/admin/students")
	if err != nil {
		t.Fatal(err)
	}
	if studentDirectory.StatusCode != http.StatusForbidden {
		t.Fatalf("student admin directory status = %d", studentDirectory.StatusCode)
	}
	studentDirectory.Body.Close()

	course := store.courses[0]
	grade := postJSON(t, adminClient, server.URL+"/api/admin/grades", map[string]any{
		"studentId": student.ID, "courseId": course.ID, "score": 95, "point": 4, "status": "published",
	})
	if grade.StatusCode != http.StatusCreated {
		t.Fatalf("grade create status = %d", grade.StatusCode)
	}
	var gradePayload struct {
		Grade Grade `json:"grade"`
	}
	if err := json.NewDecoder(grade.Body).Decode(&gradePayload); err != nil {
		t.Fatal(err)
	}
	grade.Body.Close()
	if gradePayload.Grade.ID == "" || gradePayload.Grade.StudentID != student.ID || gradePayload.Grade.CourseCode != course.Code {
		t.Fatalf("unexpected grade: %#v", gradePayload.Grade)
	}
	gradeUpdate := doJSON(t, adminClient, http.MethodPatch, server.URL+"/api/admin/grades/"+url.PathEscape(gradePayload.Grade.ID), map[string]any{"score": 98})
	if gradeUpdate.StatusCode != http.StatusOK {
		t.Fatalf("grade update status = %d", gradeUpdate.StatusCode)
	}
	gradeUpdate.Body.Close()

	schedule := postJSON(t, adminClient, server.URL+"/api/admin/schedule", map[string]any{
		"studentId": student.StudentID, "courseId": course.ID, "day": 2, "start": "09:00", "end": "10:30", "location": "至冬研究楼 101",
	})
	if schedule.StatusCode != http.StatusCreated {
		t.Fatalf("schedule create status = %d", schedule.StatusCode)
	}
	var schedulePayload struct {
		Schedule ScheduleEntry `json:"schedule"`
	}
	if err := json.NewDecoder(schedule.Body).Decode(&schedulePayload); err != nil {
		t.Fatal(err)
	}
	schedule.Body.Close()
	if schedulePayload.Schedule.ID == "" || schedulePayload.Schedule.StudentID != student.ID || schedulePayload.Schedule.Day != 2 {
		t.Fatalf("unexpected schedule: %#v", schedulePayload.Schedule)
	}

	studentGrades, err := studentClient.Get(server.URL + "/api/grades")
	if err != nil {
		t.Fatal(err)
	}
	if studentGrades.StatusCode != http.StatusOK {
		t.Fatalf("student grades status = %d", studentGrades.StatusCode)
	}
	var studentGradePayload struct {
		Grades []Grade `json:"grades"`
	}
	if err := json.NewDecoder(studentGrades.Body).Decode(&studentGradePayload); err != nil {
		t.Fatal(err)
	}
	studentGrades.Body.Close()
	if len(studentGradePayload.Grades) != 1 || studentGradePayload.Grades[0].StudentID != student.ID {
		t.Fatalf("student grade visibility = %#v", studentGradePayload.Grades)
	}
	studentSchedule, err := studentClient.Get(server.URL + "/api/schedule")
	if err != nil {
		t.Fatal(err)
	}
	if studentSchedule.StatusCode != http.StatusOK {
		t.Fatalf("student schedule status = %d", studentSchedule.StatusCode)
	}
	var studentSchedulePayload struct {
		Schedule []ScheduleEntry `json:"schedule"`
	}
	if err := json.NewDecoder(studentSchedule.Body).Decode(&studentSchedulePayload); err != nil {
		t.Fatal(err)
	}
	studentSchedule.Body.Close()
	if len(studentSchedulePayload.Schedule) != 1 || studentSchedulePayload.Schedule[0].StudentID != student.ID {
		t.Fatalf("student schedule visibility = %#v", studentSchedulePayload.Schedule)
	}

	removeSchedule := doJSON(t, adminClient, http.MethodDelete, server.URL+"/api/admin/schedule/"+url.PathEscape(schedulePayload.Schedule.ID), nil)
	if removeSchedule.StatusCode != http.StatusOK {
		t.Fatalf("schedule delete status = %d", removeSchedule.StatusCode)
	}
	removeSchedule.Body.Close()
	removeGrade := doJSON(t, adminClient, http.MethodDelete, server.URL+"/api/admin/grades/"+url.PathEscape(gradePayload.Grade.ID), nil)
	if removeGrade.StatusCode != http.StatusOK {
		t.Fatalf("grade delete status = %d", removeGrade.StatusCode)
	}
	removeGrade.Body.Close()
}

func TestAdminAcademicMutationRequiresCSRFHeader(t *testing.T) {
	server := httptest.NewServer(NewServer(NewStoreWithAdmin(testAdminUsername, testAdminPassword), "web"))
	defer server.Close()
	client := &http.Client{}
	login := postJSON(t, client, server.URL+"/api/auth/login", map[string]string{"username": testAdminUsername, "password": testAdminPassword})
	if login.StatusCode != http.StatusOK {
		t.Fatalf("administrator login status = %d", login.StatusCode)
	}
	login.Body.Close()
	body, err := json.Marshal(map[string]any{"studentId": "missing", "courseId": "missing", "score": 1})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/admin/grades", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("missing CSRF header status = %d", response.StatusCode)
	}
	response.Body.Close()
}

func TestStudentPasswordRotationRevokesOtherSessions(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	student, apiError := store.createStudent(StudentInput{
		Username: "password-student", Name: "密码测试学生", StudentID: "CGU-PASSWORD-001",
		Password: "old-student-password-2026!",
	})
	if apiError != nil || student == nil {
		t.Fatalf("student create = %#v, error = %#v", student, apiError)
	}
	server := httptest.NewServer(NewServer(store, "web"))
	defer server.Close()
	firstJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	secondJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	firstClient := &http.Client{Jar: firstJar}
	secondClient := &http.Client{Jar: secondJar}
	for _, client := range []*http.Client{firstClient, secondClient} {
		login := postJSON(t, client, server.URL+"/api/auth/login", map[string]string{"username": student.Username, "password": "old-student-password-2026!"})
		if login.StatusCode != http.StatusOK {
			t.Fatalf("student login status = %d", login.StatusCode)
		}
		login.Body.Close()
	}
	wrong := postJSON(t, firstClient, server.URL+"/api/auth/password", map[string]string{"currentPassword": "wrong-current-password-2026!", "newPassword": "new-student-password-2026!", "confirmPassword": "new-student-password-2026!"})
	if wrong.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong current password status = %d", wrong.StatusCode)
	}
	wrong.Body.Close()
	mismatch := postJSON(t, firstClient, server.URL+"/api/auth/password", map[string]string{"currentPassword": "old-student-password-2026!", "newPassword": "new-student-password-2026!", "confirmPassword": "different-password-2026!"})
	if mismatch.StatusCode != http.StatusBadRequest {
		t.Fatalf("password mismatch status = %d", mismatch.StatusCode)
	}
	mismatch.Body.Close()
	rotated := postJSON(t, firstClient, server.URL+"/api/auth/password", map[string]string{"currentPassword": "old-student-password-2026!", "newPassword": "new-student-password-2026!", "confirmPassword": "new-student-password-2026!"})
	if rotated.StatusCode != http.StatusOK {
		t.Fatalf("password rotation status = %d", rotated.StatusCode)
	}
	rotated.Body.Close()
	otherSession, err := secondClient.Get(server.URL + "/api/auth/me")
	if err != nil {
		t.Fatal(err)
	}
	if otherSession.StatusCode != http.StatusUnauthorized {
		t.Fatalf("other session after rotation status = %d", otherSession.StatusCode)
	}
	otherSession.Body.Close()
	oldLogin := postJSON(t, secondClient, server.URL+"/api/auth/login", map[string]string{"username": student.Username, "password": "old-student-password-2026!"})
	if oldLogin.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old password login status = %d", oldLogin.StatusCode)
	}
	oldLogin.Body.Close()
	newLogin := postJSON(t, secondClient, server.URL+"/api/auth/login", map[string]string{"username": student.StudentEmail, "password": "new-student-password-2026!"})
	if newLogin.StatusCode != http.StatusOK {
		t.Fatalf("new student mailbox login status = %d", newLogin.StatusCode)
	}
	newLogin.Body.Close()
}

func TestAdminStudentPasswordUpdateRevokesAllStudentSessions(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	student, apiError := store.createStudent(StudentInput{
		Username: "admin-reset-student", Name: "管理员重置测试", StudentID: "CGU-ADMIN-RESET-001",
		Password: "old-admin-reset-password-2026!",
	})
	if apiError != nil || student == nil {
		t.Fatalf("student create = %#v, error = %#v", student, apiError)
	}
	server := httptest.NewServer(NewServer(store, "web"))
	defer server.Close()
	studentClients := make([]*http.Client, 0, 2)
	for range 2 {
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatal(err)
		}
		client := &http.Client{Jar: jar}
		login := postJSON(t, client, server.URL+"/api/auth/login", map[string]string{
			"username": student.Username, "password": "old-admin-reset-password-2026!",
		})
		if login.StatusCode != http.StatusOK {
			t.Fatalf("student login status = %d", login.StatusCode)
		}
		login.Body.Close()
		studentClients = append(studentClients, client)
	}
	adminJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	adminClient := &http.Client{Jar: adminJar}
	adminLogin := postJSON(t, adminClient, server.URL+"/api/auth/login", map[string]string{
		"username": testAdminUsername, "password": testAdminPassword,
	})
	if adminLogin.StatusCode != http.StatusOK {
		t.Fatalf("admin login status = %d", adminLogin.StatusCode)
	}
	adminLogin.Body.Close()
	updated := doJSON(t, adminClient, http.MethodPatch, server.URL+"/api/admin/students/"+student.ID, map[string]string{
		"password": "new-admin-reset-password-2026!",
	})
	if updated.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(updated.Body)
		updated.Body.Close()
		t.Fatalf("admin password update status = %d body = %s", updated.StatusCode, raw)
	}
	updated.Body.Close()
	for index, client := range studentClients {
		me, err := client.Get(server.URL + "/api/auth/me")
		if err != nil {
			t.Fatal(err)
		}
		if me.StatusCode != http.StatusUnauthorized {
			t.Fatalf("student session %d after admin reset status = %d", index, me.StatusCode)
		}
		me.Body.Close()
	}
	oldLogin := postJSON(t, &http.Client{}, server.URL+"/api/auth/login", map[string]string{
		"username": student.Username, "password": "old-admin-reset-password-2026!",
	})
	if oldLogin.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old password login status = %d", oldLogin.StatusCode)
	}
	oldLogin.Body.Close()
	newLogin := postJSON(t, &http.Client{}, server.URL+"/api/auth/login", map[string]string{
		"username": student.Username, "password": "new-admin-reset-password-2026!",
	})
	if newLogin.StatusCode != http.StatusOK {
		t.Fatalf("new password login status = %d", newLogin.StatusCode)
	}
	newLogin.Body.Close()
}

func TestApprovedStudentIdentityCannotBeChanged(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	application, apiError := store.createAdmission(AdmissionApplicationInput{
		Name: "录取身份锁定测试", Email: "identity-lock@example.com", School: "至冬与极地研究学院",
	})
	if apiError != nil {
		t.Fatalf("create admission: %v", apiError)
	}
	approval, apiError := store.approveAdmission(application.ID, "admin")
	if apiError != nil {
		t.Fatalf("approve admission: %v", apiError)
	}
	student := store.users[approval.Student.ID]
	if student == nil {
		t.Fatal("approved student was not stored")
	}
	view := store.adminStudentView(student)
	if !view.AdmissionApproved {
		t.Fatalf("approved student projection was not marked immutable: %#v", view)
	}
	_, apiError = store.updateStudent(student.ID, StudentInput{StudentID: student.StudentID + "-CHANGED"})
	if apiError == nil || apiError.Status != http.StatusConflict || apiError.Code != "student_identity_immutable" {
		t.Fatalf("approved student id mutation error = %#v", apiError)
	}
	if student.StudentID != approval.Application.StudentID {
		t.Fatalf("student id changed despite rejection: user=%q application=%q", student.StudentID, approval.Application.StudentID)
	}
}

func TestAdmissionDeleteRequiresAdminCSRFAndRemovesPendingApplication(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	server := httptest.NewServer(NewServer(store, "web"))
	defer server.Close()
	created := postJSON(t, &http.Client{}, server.URL+"/api/admissions", map[string]string{"name": "待删除申请", "englishName": "Pending Delete", "email": "delete@example.com", "school": "综合学院"})
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("admission create status = %d", created.StatusCode)
	}
	var payload struct {
		Application AdmissionApplication `json:"application"`
	}
	if err := json.NewDecoder(created.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	created.Body.Close()
	unauthorizedRequest, err := http.NewRequest(http.MethodDelete, server.URL+"/api/admin/admissions/"+url.PathEscape(payload.Application.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	unauthorizedRequest.Header.Set("X-CGU-Request", "1")
	unauthorizedResponse, err := (&http.Client{}).Do(unauthorizedRequest)
	if err != nil {
		t.Fatal(err)
	}
	if unauthorizedResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous admission delete status = %d", unauthorizedResponse.StatusCode)
	}
	unauthorizedResponse.Body.Close()
	adminJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	adminClient := &http.Client{Jar: adminJar}
	login := postJSON(t, adminClient, server.URL+"/api/auth/login", map[string]string{"username": testAdminUsername, "password": testAdminPassword})
	if login.StatusCode != http.StatusOK {
		t.Fatalf("admin login status = %d", login.StatusCode)
	}
	login.Body.Close()

	// A browser-like authenticated request without the explicit same-site
	// header must still be rejected by the global CSRF guard.
	withoutCSRF, err := http.NewRequest(http.MethodDelete, server.URL+"/api/admin/admissions/"+url.PathEscape(payload.Application.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	withoutCSRFResponse, err := adminClient.Do(withoutCSRF)
	if err != nil {
		t.Fatal(err)
	}
	if withoutCSRFResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("admission delete without CSRF status = %d", withoutCSRFResponse.StatusCode)
	}
	withoutCSRFResponse.Body.Close()

	deleted := doJSON(t, adminClient, http.MethodDelete, server.URL+"/api/admin/admissions/"+url.PathEscape(payload.Application.ID), nil)
	if deleted.StatusCode != http.StatusOK {
		t.Fatalf("admission delete status = %d", deleted.StatusCode)
	}
	deleted.Body.Close()
	if len(store.admissions) != 0 {
		t.Fatalf("deleted admission row count = %d", len(store.admissions))
	}
	if len(store.notifications) != 0 {
		t.Fatalf("orphan notification count = %d", len(store.notifications))
	}
}

func TestAcademicMutationRollbackWhenDatabaseWriteFails(t *testing.T) {
	closedDB, err := sql.Open("mysql", "cgu:cgu@tcp(127.0.0.1:1)/cgu")
	if err != nil {
		t.Fatal(err)
	}
	if err := closedDB.Close(); err != nil {
		t.Fatal(err)
	}

	studentStore := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	studentStore.db = closedDB
	_, studentErr := studentStore.createStudent(StudentInput{Username: "rollback-student", Name: "回滚学生", StudentID: "CGU-ROLLBACK", Password: "rollback-password-2026!"})
	if studentErr == nil || studentErr.Status != http.StatusServiceUnavailable || len(studentStore.users) != 1 {
		t.Fatalf("student rollback result = %#v, users = %d", studentErr, len(studentStore.users))
	}

	gradeStore := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	gradeStore.db = closedDB
	gradeStore.users["student-roll"] = &User{ID: "student-roll", Username: "student-roll", Name: "回滚学生", Role: "student", StudentID: "CGU-ROLLBACK"}
	_, gradeErr := gradeStore.createGrade(GradeInput{StudentID: "student-roll", CourseID: gradeStore.courses[0].ID, Score: 90, Point: 3})
	if gradeErr == nil || gradeErr.Status != http.StatusServiceUnavailable || len(gradeStore.grades) != 0 {
		t.Fatalf("grade rollback result = %#v, grades = %d", gradeErr, len(gradeStore.grades))
	}

	scheduleStore := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	scheduleStore.db = closedDB
	scheduleStore.users["student-roll"] = &User{ID: "student-roll", Username: "student-roll", Name: "回滚学生", Role: "student", StudentID: "CGU-ROLLBACK"}
	_, scheduleErr := scheduleStore.createSchedule(ScheduleInput{StudentID: "student-roll", CourseID: scheduleStore.courses[0].ID, Day: intPtr(1), Start: "09:00", End: "10:00"})
	if scheduleErr == nil || scheduleErr.Status != http.StatusServiceUnavailable || len(scheduleStore.schedule) != 0 {
		t.Fatalf("schedule rollback result = %#v, schedule = %d", scheduleErr, len(scheduleStore.schedule))
	}
}

func intPtr(value int) *int { return &value }

func floatPtr(value float64) *float64 { return &value }

func doJSON(t *testing.T, client *http.Client, method, endpoint string, value any) *http.Response {
	t.Helper()
	var body io.Reader
	if value != nil {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, endpoint, body)
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
