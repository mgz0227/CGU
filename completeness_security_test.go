package main

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTranscriptDownloadIsStudentScopedAndPublishedOnly(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	student, apiError := store.createStudent(StudentInput{
		Username: "transcript-student", Name: "成绩单学生", Email: "transcript@example.com",
		StudentID: "CGU-TRANSCRIPT-001", Password: "transcript-password-2026!",
	})
	if apiError != nil || student == nil {
		t.Fatalf("create student = %#v, error = %#v", student, apiError)
	}
	course := store.courses[0]
	if _, apiError = store.createGrade(GradeInput{StudentID: student.ID, CourseID: course.ID, Score: 92, Point: 3.8, Term: "2026-秋", Status: "published", Credits: intPtr(3)}); apiError != nil {
		t.Fatalf("create published grade = %#v", apiError)
	}
	if _, apiError = store.createGrade(GradeInput{StudentID: student.ID, CourseID: store.courses[1].ID, Score: 40, Point: 1, Term: "2026-秋", Status: "inprogress", Credits: intPtr(4)}); apiError != nil {
		t.Fatalf("create draft grade = %#v", apiError)
	}
	server := httptest.NewServer(NewServer(store, "web"))
	defer server.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	login := postJSON(t, client, server.URL+"/api/auth/login", map[string]string{"username": student.Username, "password": "transcript-password-2026!"})
	if login.StatusCode != http.StatusOK {
		t.Fatalf("student login status = %d", login.StatusCode)
	}
	login.Body.Close()
	request, err := http.NewRequest(http.MethodGet, server.URL+"/api/transcript.csv?lang=en", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(response.Body)
	response.Body.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(response.Header.Get("Content-Disposition"), "cgu-transcript.csv") {
		t.Fatalf("transcript response = status %d headers %#v", response.StatusCode, response.Header)
	}
	if !strings.Contains(string(body), "ELM101") || !strings.Contains(string(body), "Published") || strings.Contains(string(body), "NAT202") {
		t.Fatalf("transcript exposed unexpected rows: %q", body)
	}
}

func TestTLSLoginAlwaysMarksSessionCookieSecure(t *testing.T) {
	server := httptest.NewTLSServer(NewServer(NewStoreWithAdmin(testAdminUsername, testAdminPassword), "web"))
	defer server.Close()
	client := server.Client()
	response := postJSON(t, client, server.URL+"/api/auth/login", map[string]string{"username": testAdminUsername, "password": testAdminPassword})
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(response.Header.Get("Set-Cookie"), "Secure") {
		t.Fatalf("TLS login cookie = status %d, %q", response.StatusCode, response.Header.Get("Set-Cookie"))
	}
}

func TestSensitiveStaticFilesAreNeverServed(t *testing.T) {
	server := httptest.NewServer(NewServer(NewStoreWithAdmin(testAdminUsername, testAdminPassword), "."))
	defer server.Close()
	for _, name := range []string{"/config.json", "/config.example.json", "/.env", "/main.go", "/go.mod", "/database.go"} {
		response, err := http.Get(server.URL + name)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("sensitive static path %s status = %d", name, response.StatusCode)
		}
	}
}

func TestCSVFormulaCellsAreNeutralized(t *testing.T) {
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	store.mu.Lock()
	store.courses[0].NameZh = "=HYPERLINK(\"https://attacker.invalid\")"
	store.mu.Unlock()
	catalog, err := store.catalogCSV("zh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(catalog), "'=HYPERLINK") {
		t.Fatalf("catalog formula was not neutralized: %q", catalog)
	}

	student, apiError := store.createStudent(StudentInput{
		Username: "formula-student", Name: "@SUM(1+1)", Email: "formula@example.com",
		StudentID: "CGU-FORMULA-001", Password: "formula-student-password-2026!",
	})
	if apiError != nil || student == nil {
		t.Fatalf("create formula student = %#v, error = %#v", student, apiError)
	}
	if _, apiError = store.createGrade(GradeInput{StudentID: student.ID, CourseID: store.courses[0].ID, Score: 90, Point: 4, Term: "2026-秋", Status: "published", Credits: intPtr(3)}); apiError != nil {
		t.Fatalf("create formula transcript grade = %#v", apiError)
	}
	transcript, err := store.transcriptCSV(student.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(transcript), "'@SUM(1+1)") {
		t.Fatalf("transcript formula was not neutralized: %q", transcript)
	}
}

func TestOversizedPasswordsAreRejectedBeforeBcrypt(t *testing.T) {
	oversized := strings.Repeat("x", maxPasswordBytes+1)
	if _, err := hashPasswordChecked(oversized); err == nil {
		t.Fatal("oversized password was accepted by hashPasswordChecked")
	}
	if hashPassword(oversized) != "" {
		t.Fatal("compatibility hashPassword wrapper returned a hash for an oversized password")
	}
	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	if user := store.authenticate(testAdminUsername, oversized); user != nil {
		t.Fatal("oversized login password unexpectedly authenticated")
	}
	if _, apiError := store.createStudent(StudentInput{
		Username: "oversized-student", Name: "超长密码学生", StudentID: "CGU-OVERSIZED-001", Password: oversized,
	}); apiError == nil || apiError.Code != "invalid_input" {
		t.Fatalf("oversized student password error = %#v", apiError)
	}
}

func TestLoginRateLimitAlsoBlocksIdentifierSprayByIP(t *testing.T) {
	server := httptest.NewServer(NewServer(NewStoreWithAdmin(testAdminUsername, testAdminPassword), "web"))
	defer server.Close()
	client := &http.Client{}
	for attempt := 0; attempt < loginMaxFails; attempt++ {
		response := postJSON(t, client, server.URL+"/api/auth/login", map[string]string{
			"username": "spray-account-" + string(rune('a'+attempt)),
			"password": "wrong-password-2026!",
		})
		if response.StatusCode != http.StatusUnauthorized {
			response.Body.Close()
			t.Fatalf("identifier spray attempt %d status = %d", attempt+1, response.StatusCode)
		}
		response.Body.Close()
	}
	response := postJSON(t, client, server.URL+"/api/auth/login", map[string]string{
		"username": "another-unique-account", "password": "wrong-password-2026!",
	})
	defer response.Body.Close()
	if response.StatusCode != http.StatusTooManyRequests || response.Header.Get("Retry-After") == "" {
		t.Fatalf("IP spray rate limit = status %d, retry-after %q", response.StatusCode, response.Header.Get("Retry-After"))
	}
}
