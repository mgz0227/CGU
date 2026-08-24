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
	if err := os.WriteFile(configPath, []byte(`{"server":{"host":"0.0.0.0","port":8111},"database":{"enabled":false,"host":"db-from-config"},"adminUsername":"registrar","adminPassword":"configured-from-json"}`), 0600); err != nil {
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
	t.Setenv("CGU_DB_ENABLED", "true")
	if !LoadConfig().Database.Enabled {
		t.Fatal("process environment should override config and dotenv")
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
