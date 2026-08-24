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
	if err := os.WriteFile(configPath, []byte(`{"server":{"address":"0.0.0.0:8111"},"database":{"enabled":false,"host":"db-from-config"}}`), 0600); err != nil {
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
	if cfg.Server.Address != "127.0.0.1:9222" {
		t.Fatalf("port override should rebuild address, got %q", cfg.Server.Address)
	}
	if cfg.Database.Host != "db-from-config" {
		t.Fatalf("config database host was not preserved: %q", cfg.Database.Host)
	}
	t.Setenv("CGU_DB_ENABLED", "true")
	if !LoadConfig().Database.Enabled {
		t.Fatal("process environment should override config and dotenv")
	}
}
