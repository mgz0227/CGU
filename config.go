package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// AppConfig is intentionally small and serializable so the same deployment
// can be configured by config.json, .env, or process environment variables.
type AppConfig struct {
	Server             ServerConfig   `json:"server"`
	Database           DatabaseConfig `json:"database"`
	SMTP               SMTPConfig     `json:"smtp"`
	StaticDir          string         `json:"staticDir"`
	CookieSecure       bool           `json:"cookieSecure"`
	PublicOrigin       string         `json:"publicOrigin"`
	TrustedProxies     []string       `json:"trustedProxies"`
	StudentEmailDomain string         `json:"studentEmailDomain"`
	AdminUsername      string         `json:"adminUsername"`
	AdminPassword      string         `json:"adminPassword"`
}

type ServerConfig struct {
	Address string `json:"address"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
}

type DatabaseConfig struct {
	Enabled      bool   `json:"enabled"`
	Driver       string `json:"driver"`
	DSN          string `json:"dsn"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	User         string `json:"user"`
	Password     string `json:"password"`
	Name         string `json:"name"`
	MaxOpenConns int    `json:"maxOpenConns"`
	MaxIdleConns int    `json:"maxIdleConns"`
}

// SMTPConfig controls optional real-mail delivery. Internal mailbox storage is
// always available; SMTP is enabled explicitly and defaults to mandatory TLS.
type SMTPConfig struct {
	Enabled       bool   `json:"enabled"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	From          string `json:"from"`
	FromName      string `json:"fromName"`
	Auth          string `json:"auth"`
	TLSMode       string `json:"tlsMode"`
	HELO          string `json:"helo"`
	TimeoutSecond int    `json:"timeoutSecond"`
	AllowInsecure bool   `json:"allowInsecure"`
}

func defaultConfig() AppConfig {
	return AppConfig{
		Server:             ServerConfig{Address: "127.0.0.1:8000", Host: "127.0.0.1", Port: 8000},
		Database:           DatabaseConfig{Driver: "mysql", Host: "127.0.0.1", Port: 3306, Name: "cgu", MaxOpenConns: 10, MaxIdleConns: 5},
		SMTP:               SMTPConfig{Port: 587, Auth: "auto", TLSMode: "starttls", TimeoutSecond: 15},
		StaticDir:          "web",
		AdminUsername:      "admin",
		StudentEmailDomain: "cgu.edu.kg",
	}
}

func LoadConfig() AppConfig {
	cfg := defaultConfig()
	configDatabaseEnabledSet := false
	configAddressSet := false
	configFile := firstEnv("CGU_CONFIG_FILE", "CONFIG_FILE")
	configFile = resolveDeploymentFile(configFile, "config.json")
	if data, err := os.ReadFile(configFile); err == nil {
		var raw struct {
			Server   map[string]json.RawMessage `json:"server"`
			Database map[string]json.RawMessage `json:"database"`
		}
		if json.Unmarshal(data, &raw) == nil {
			_, configAddressSet = raw.Server["address"]
			_, configDatabaseEnabledSet = raw.Database["enabled"]
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			logConfigWarning(configFile, err)
		}
	}
	fileEnv := map[string]string{}
	envFile := firstEnv("CGU_ENV_FILE", "ENV_FILE")
	envFile = resolveDeploymentFile(envFile, ".env")
	if data, err := os.ReadFile(envFile); err == nil {
		fileEnv = parseDotEnv(string(data))
	}

	cfg.StaticDir = envString(fileEnv, "CGU_STATIC_DIR", cfg.StaticDir)
	cfg.CookieSecure = envBool(fileEnv, "CGU_COOKIE_SECURE", cfg.CookieSecure)
	cfg.PublicOrigin = envString(fileEnv, "CGU_PUBLIC_ORIGIN", cfg.PublicOrigin)
	cfg.TrustedProxies = envList(fileEnv, "CGU_TRUSTED_PROXIES", cfg.TrustedProxies)
	cfg.StudentEmailDomain = envString(fileEnv, "CGU_STUDENT_EMAIL_DOMAIN", cfg.StudentEmailDomain)
	if strings.TrimSpace(cfg.StudentEmailDomain) == "" {
		cfg.StudentEmailDomain = "cgu.edu.kg"
	}
	cfg.AdminUsername = envString(fileEnv, "CGU_ADMIN_USERNAME", cfg.AdminUsername)
	cfg.AdminPassword = envString(fileEnv, "CGU_ADMIN_PASSWORD", cfg.AdminPassword)
	if strings.TrimSpace(cfg.AdminUsername) == "" {
		cfg.AdminUsername = "admin"
	}
	addressOverride := firstEnvWithFile(fileEnv, "CGU_ADDR")
	address := cfg.Server.Address
	if port := envInt(fileEnv, "CGU_PORT", cfg.Server.Port); port > 0 {
		cfg.Server.Port = port
	}
	if host := envString(fileEnv, "CGU_HOST", cfg.Server.Host); host != "" {
		cfg.Server.Host = host
	}
	if configuredPort := firstEnvWithFile(fileEnv, "PORT"); configuredPort != "" {
		if port, err := strconv.Atoi(configuredPort); err == nil && port > 0 {
			cfg.Server.Port = port
		}
	}
	if addressOverride != "" {
		address = addressOverride
	} else if firstEnvWithFile(fileEnv, "CGU_PORT") != "" || firstEnvWithFile(fileEnv, "PORT") != "" || firstEnvWithFile(fileEnv, "CGU_HOST") != "" || !configAddressSet {
		address = fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	} else if strings.TrimSpace(address) == "" {
		address = fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	}
	if strings.TrimSpace(address) != "" {
		cfg.Server.Address = address
	}

	cfg.Database.Driver = envString(fileEnv, "CGU_DB_DRIVER", cfg.Database.Driver)
	cfg.Database.DSN = envString(fileEnv, "CGU_DB_DSN", cfg.Database.DSN)
	cfg.Database.Host = envString(fileEnv, "CGU_DB_HOST", cfg.Database.Host)
	cfg.Database.User = envString(fileEnv, "CGU_DB_USER", cfg.Database.User)
	cfg.Database.Password = envString(fileEnv, "CGU_DB_PASSWORD", cfg.Database.Password)
	cfg.Database.Name = envString(fileEnv, "CGU_DB_NAME", cfg.Database.Name)
	cfg.Database.Port = envInt(fileEnv, "CGU_DB_PORT", cfg.Database.Port)
	cfg.Database.MaxOpenConns = envInt(fileEnv, "CGU_DB_MAX_OPEN", cfg.Database.MaxOpenConns)
	cfg.Database.MaxIdleConns = envInt(fileEnv, "CGU_DB_MAX_IDLE", cfg.Database.MaxIdleConns)
	databaseEnabledOverride := firstEnvWithFile(fileEnv, "CGU_DB_ENABLED")
	cfg.Database.Enabled = envBool(fileEnv, "CGU_DB_ENABLED", cfg.Database.Enabled)
	if databaseEnabledOverride == "" && !configDatabaseEnabledSet && !cfg.Database.Enabled && (cfg.Database.DSN != "" || (cfg.Database.User != "" && cfg.Database.Name != "")) {
		cfg.Database.Enabled = true
	}
	if cfg.Database.Driver == "" {
		cfg.Database.Driver = "mysql"
	}
	if cfg.Database.Port <= 0 {
		cfg.Database.Port = 3306
	}
	cfg.SMTP.Enabled = envBool(fileEnv, "CGU_SMTP_ENABLED", cfg.SMTP.Enabled)
	cfg.SMTP.Host = envString(fileEnv, "CGU_SMTP_HOST", cfg.SMTP.Host)
	cfg.SMTP.Port = envInt(fileEnv, "CGU_SMTP_PORT", cfg.SMTP.Port)
	cfg.SMTP.Username = envString(fileEnv, "CGU_SMTP_USERNAME", cfg.SMTP.Username)
	cfg.SMTP.Password = envString(fileEnv, "CGU_SMTP_PASSWORD", cfg.SMTP.Password)
	cfg.SMTP.From = envString(fileEnv, "CGU_SMTP_FROM", cfg.SMTP.From)
	cfg.SMTP.FromName = envString(fileEnv, "CGU_SMTP_FROM_NAME", cfg.SMTP.FromName)
	cfg.SMTP.Auth = envString(fileEnv, "CGU_SMTP_AUTH", cfg.SMTP.Auth)
	cfg.SMTP.TLSMode = envString(fileEnv, "CGU_SMTP_TLS_MODE", cfg.SMTP.TLSMode)
	cfg.SMTP.HELO = envString(fileEnv, "CGU_SMTP_HELO", cfg.SMTP.HELO)
	cfg.SMTP.TimeoutSecond = envInt(fileEnv, "CGU_SMTP_TIMEOUT_SECONDS", cfg.SMTP.TimeoutSecond)
	cfg.SMTP.AllowInsecure = envBool(fileEnv, "CGU_SMTP_ALLOW_INSECURE", cfg.SMTP.AllowInsecure)
	if cfg.SMTP.Port <= 0 {
		cfg.SMTP.Port = 587
	}
	if cfg.SMTP.TimeoutSecond <= 0 {
		cfg.SMTP.TimeoutSecond = 15
	}
	if strings.TrimSpace(cfg.SMTP.Auth) == "" {
		cfg.SMTP.Auth = "auto"
	}
	if strings.TrimSpace(cfg.SMTP.TLSMode) == "" {
		cfg.SMTP.TLSMode = "starttls"
	}
	return cfg
}

// resolveDeploymentFile lets a binary find its private config beside the
// executable while retaining the conventional current-working-directory
// behavior. Explicit paths remain untouched so operators can use an absolute
// path or a path relative to the process directory.
func resolveDeploymentFile(explicit, name string) string {
	if strings.TrimSpace(explicit) != "" {
		return explicit
	}
	workingDir, _ := os.Getwd()
	executableDir := ""
	if executable, err := os.Executable(); err == nil {
		executableDir = filepath.Dir(executable)
	}
	return resolveDeploymentFileFrom("", name, workingDir, executableDir)
}

func resolveDeploymentFileFrom(explicit, name, workingDir, executableDir string) string {
	if strings.TrimSpace(explicit) != "" {
		return explicit
	}
	candidates := make([]string, 0, 2)
	if workingDir != "" {
		candidates = append(candidates, filepath.Join(workingDir, name))
	}
	if executableDir != "" && !samePath(workingDir, executableDir) {
		candidates = append(candidates, filepath.Join(executableDir, name))
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	// Keep the conventional name for warning messages and future creation.
	return name
}

func samePath(left, right string) bool {
	leftPath, leftErr := filepath.Abs(left)
	rightPath, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && strings.EqualFold(filepath.Clean(leftPath), filepath.Clean(rightPath))
}

func (c AppConfig) MySQLDSN() string {
	if strings.TrimSpace(c.Database.DSN) != "" {
		return c.Database.DSN
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&charset=utf8mb4&collation=utf8mb4_unicode_ci&loc=Local",
		c.Database.User, c.Database.Password, c.Database.Host, c.Database.Port, c.Database.Name)
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func firstEnvWithFile(fileEnv map[string]string, name string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return strings.TrimSpace(fileEnv[name])
}

func envString(fileEnv map[string]string, name, fallback string) string {
	if value := firstEnvWithFile(fileEnv, name); value != "" {
		return value
	}
	return fallback
}

func envInt(fileEnv map[string]string, name string, fallback int) int {
	value := firstEnvWithFile(fileEnv, name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envBool(fileEnv map[string]string, name string, fallback bool) bool {
	value := firstEnvWithFile(fileEnv, name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envList(fileEnv map[string]string, name string, fallback []string) []string {
	value := firstEnvWithFile(fileEnv, name)
	if value == "" {
		return fallback
	}
	result := make([]string, 0)
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func parseDotEnv(content string) map[string]string {
	values := make(map[string]string)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		if key != "" {
			values[key] = value
		}
	}
	return values
}

func logConfigWarning(file string, err error) {
	// Keep configuration errors visible without printing any values, especially
	// passwords or DSNs.
	fmt.Fprintf(os.Stderr, "CGU config warning: cannot parse %s: %v\n", file, err)
}
