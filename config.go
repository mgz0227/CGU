package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// AppConfig is intentionally small and serializable so the same deployment
// can be configured by config.json, .env, or process environment variables.
type AppConfig struct {
	Server          ServerConfig   `json:"server"`
	Database        DatabaseConfig `json:"database"`
	StaticDir       string         `json:"staticDir"`
	CookieSecure    bool           `json:"cookieSecure"`
	StudentPassword string         `json:"studentPassword"`
	AdminPassword   string         `json:"adminPassword"`
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

func defaultConfig() AppConfig {
	return AppConfig{
		Server:          ServerConfig{Address: "127.0.0.1:8000", Host: "127.0.0.1", Port: 8000},
		Database:        DatabaseConfig{Driver: "mysql", Host: "127.0.0.1", Port: 3306, Name: "cgu", MaxOpenConns: 10, MaxIdleConns: 5},
		StaticDir:       "web",
		StudentPassword: "student-demo",
		AdminPassword:   "admin-demo",
	}
}

func LoadConfig() AppConfig {
	cfg := defaultConfig()
	configDatabaseEnabledSet := false
	configAddressSet := false
	configFile := firstEnv("CGU_CONFIG_FILE", "CONFIG_FILE")
	if configFile == "" {
		configFile = "config.json"
	}
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
	if envFile == "" {
		envFile = ".env"
	}
	if data, err := os.ReadFile(envFile); err == nil {
		fileEnv = parseDotEnv(string(data))
	}

	cfg.StaticDir = envString(fileEnv, "CGU_STATIC_DIR", cfg.StaticDir)
	cfg.CookieSecure = envBool(fileEnv, "CGU_COOKIE_SECURE", cfg.CookieSecure)
	cfg.StudentPassword = envString(fileEnv, "CGU_STUDENT_PASSWORD", cfg.StudentPassword)
	cfg.AdminPassword = envString(fileEnv, "CGU_ADMIN_PASSWORD", cfg.AdminPassword)
	if cfg.StudentPassword == "" {
		cfg.StudentPassword = "student-demo"
	}
	if cfg.AdminPassword == "" {
		cfg.AdminPassword = "admin-demo"
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
	return cfg
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
