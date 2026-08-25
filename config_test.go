package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDotEnv(t *testing.T) {
	values := parseDotEnv("# comment\nexport CGU_ADDR=127.0.0.1:9000\nCGU_DB_PASSWORD=\"quoted=value\"\nBROKEN\n")
	if values["CGU_ADDR"] != "127.0.0.1:9000" {
		t.Fatalf("unexpected address: %q", values["CGU_ADDR"])
	}
	if values["CGU_DB_PASSWORD"] != "quoted=value" {
		t.Fatalf("unexpected password value: %q", values["CGU_DB_PASSWORD"])
	}
	if _, ok := values["BROKEN"]; ok {
		t.Fatal("invalid dotenv line should be ignored")
	}
}

func TestLoadConfigPrecedenceAndExplicitDatabaseDisable(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(configPath, []byte(`{"server":{"host":"0.0.0.0","port":8111},"database":{"enabled":false,"host":"db-from-config"},"publicOrigin":"https://cgu.edu.kg/","adminUsername":"registrar","adminPassword":"configured-from-json"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envPath, []byte("CGU_DB_USER=cgu\nCGU_DB_NAME=cgu\nCGU_DB_PASSWORD=secret\nCGU_PORT=9222\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CGU_CONFIG_FILE", configPath)
	t.Setenv("CONFIG_FILE", "")
	t.Setenv("CGU_ENV_FILE", envPath)
	t.Setenv("ENV_FILE", "")
	t.Setenv("CGU_DB_ENABLED", "")
	t.Setenv("CGU_ADDR", "")
	t.Setenv("PORT", "")
	t.Setenv("CGU_HOST", "")
	cfg := LoadConfig()
	if cfg.Database.Enabled {
		t.Fatal("explicit database.enabled=false must remain disabled")
	}
	if cfg.Server.Address != "0.0.0.0:9222" {
		t.Fatalf("port override should rebuild address, got %q", cfg.Server.Address)
	}
	if cfg.Database.Host != "db-from-config" {
		t.Fatalf("config database host was not preserved: %q", cfg.Database.Host)
	}
	if cfg.AdminUsername != "registrar" || cfg.AdminPassword != "configured-from-json" {
		t.Fatalf("administrator config was not loaded: username=%q password-set=%t", cfg.AdminUsername, cfg.AdminPassword != "")
	}
	if cfg.StudentEmailDomain != "cgu.edu.kg" {
		t.Fatalf("default student email domain = %q", cfg.StudentEmailDomain)
	}
	if cfg.PublicOrigin != "https://cgu.edu.kg/" {
		t.Fatalf("public origin was not loaded: %q", cfg.PublicOrigin)
	}
	t.Setenv("CGU_PUBLIC_ORIGIN", "https://portal.cgu.edu.kg")
	if got := LoadConfig().PublicOrigin; got != "https://portal.cgu.edu.kg" {
		t.Fatalf("process environment should override public origin, got %q", got)
	}
	t.Setenv("CGU_DB_ENABLED", "true")
	if !LoadConfig().Database.Enabled {
		t.Fatal("process environment should override config and dotenv")
	}
}

func TestLoadConfigSMTPPrecedence(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(configPath, []byte(`{"smtp":{"enabled":true,"host":"smtp-json.example","port":465,"username":"json-user","password":"json-secret","from":"json@example.com","auth":"auto","tlsMode":"ssl","timeoutSecond":20}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envPath, []byte("CGU_SMTP_HOST=smtp-env.example\nCGU_SMTP_PASSWORD=env-secret\nCGU_SMTP_TIMEOUT_SECONDS=25\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CGU_CONFIG_FILE", configPath)
	t.Setenv("CONFIG_FILE", "")
	t.Setenv("CGU_ENV_FILE", envPath)
	t.Setenv("ENV_FILE", "")
	for _, key := range []string{"CGU_SMTP_ENABLED", "CGU_SMTP_HOST", "CGU_SMTP_PORT", "CGU_SMTP_USERNAME", "CGU_SMTP_PASSWORD", "CGU_SMTP_FROM", "CGU_SMTP_FROM_NAME", "CGU_SMTP_AUTH", "CGU_SMTP_TLS_MODE", "CGU_SMTP_HELO", "CGU_SMTP_TIMEOUT_SECONDS", "CGU_SMTP_ALLOW_INSECURE"} {
		t.Setenv(key, "")
	}
	cfg := LoadConfig()
	if !cfg.SMTP.Enabled || cfg.SMTP.Host != "smtp-env.example" || cfg.SMTP.Password != "env-secret" || cfg.SMTP.Port != 465 || cfg.SMTP.TimeoutSecond != 25 {
		t.Fatalf("unexpected SMTP config precedence: enabled=%t host=%q port=%d timeout=%d password-set=%t", cfg.SMTP.Enabled, cfg.SMTP.Host, cfg.SMTP.Port, cfg.SMTP.TimeoutSecond, cfg.SMTP.Password != "")
	}
	if cfg.SMTP.TLSMode != "ssl" || cfg.SMTP.From != "json@example.com" {
		t.Fatalf("SMTP config values were not loaded from config.json: tls=%q from=%q", cfg.SMTP.TLSMode, cfg.SMTP.From)
	}
}

func TestDefaultStaticDirectory(t *testing.T) {
	cfg := defaultConfig()
	if cfg.StaticDir != "web" {
		t.Fatalf("default static directory = %q, want web", cfg.StaticDir)
	}
	server := NewServer(NewStoreWithAdmin("admin", "test-admin-password-2026!"), "")
	if server.staticDir != "web" {
		t.Fatalf("empty static directory = %q, want web", server.staticDir)
	}
}

func TestResolveDeploymentFilePrefersExistingWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.json")
	if err := os.WriteFile(file, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	if got := resolveDeploymentFile("", "config.json"); got != file {
		t.Fatalf("working-directory config path = %q, want %q", got, file)
	}
}

func TestResolveDeploymentFileUsesExecutableDirectoryWhenNeeded(t *testing.T) {
	workingDir := t.TempDir()
	executableDir := t.TempDir()
	executableConfig := filepath.Join(executableDir, "config.json")
	if err := os.WriteFile(executableConfig, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	path := resolveDeploymentFileFrom("", "config.json", workingDir, executableDir)
	if path != executableConfig {
		t.Fatalf("executable directory should be searched when cwd is missing, got %q", path)
	}
}
