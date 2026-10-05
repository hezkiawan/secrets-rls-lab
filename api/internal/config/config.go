// Package config loads the API's startup settings from environment variables.
//
// Nothing in here is a secret. The only sensitive thing the API holds at startup
// is its AppRole secret ID ("secret zero"), and that lives in a file, not here.
// Every real secret (JWT key, API tokens, Firebase key) is fetched from OpenBao.
package config

import "os"

// Config holds the settings the API needs to start.
// Capitalized names = exported, so main.go (another package) can read them.
type Config struct {
	Port   string // port the API listens on
	AppEnv string // "local", "staging", "production"…

	OpenBaoAddr  string // where OpenBao is reachable
	KVMount      string // where the KV v2 engine is mounted ("secret" in dev mode)
	Product      string // which product's secrets this API may read ("kouventa")
	RoleIDFile   string // AppRole role ID  (like a username — not very secret)
	SecretIDFile string // AppRole secret ID (like a password — "secret zero")

	DBAddr string // where to reach Postgres: PgBouncer in the reference setup
	DBName string // database name
}

// Load reads settings from environment variables, with defaults that match
// docker-compose.yml and the bootstrap script, so `go run .` works with no setup.
func Load() Config {
	return Config{
		Port:   getEnv("PORT", "3000"),
		AppEnv: getEnv("APP_ENV", "local"),

		OpenBaoAddr:  getEnv("OPENBAO_ADDR", "http://127.0.0.1:8200"),
		KVMount:      getEnv("OPENBAO_KV_MOUNT", "secret"),
		Product:      getEnv("PRODUCT", "kouventa"),
		RoleIDFile:   getEnv("OPENBAO_ROLE_ID_FILE", ".openbao/role_id"),
		SecretIDFile: getEnv("OPENBAO_SECRET_ID_FILE", ".openbao/secret_id"),

		DBAddr: getEnv("DB_ADDR", "127.0.0.1:6432"), // reference/ PgBouncer
		DBName: getEnv("DB_NAME", "supportdesk"),
	}
}

// getEnv returns the environment variable `key`, or `fallback` if it's empty.
// Lowercase name = private to this package.
func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
