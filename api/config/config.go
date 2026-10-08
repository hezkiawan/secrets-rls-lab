// Package config reads the API's settings from environment variables.
//
// Nothing here is a secret: only addresses and file names.
// Every default points at the reference stack, so `go run .` works with no setup.
package config

import "os"

type Config struct {
	Port string // the port this API listens on

	OpenBaoAddr  string // OpenBao address (HAProxy in front of the cluster)
	RoleIDFile   string // AppRole role_id   (like a username)
	SecretIDFile string // AppRole secret_id (like a password, "secret zero")
	Product      string // which product's secrets to read: secret/<product>/...
	DBRole       string // OpenBao database role that creates our temporary DB users

	DBAddr string // PgBouncer address
	DBName string // database name
}

func Load() Config {
	var cfg Config

	cfg.Port = getEnv("PORT", "3000")

	cfg.OpenBaoAddr = getEnv("OPENBAO_ADDR", "http://127.0.0.1:8300")
	cfg.RoleIDFile = getEnv("OPENBAO_ROLE_ID_FILE", ".reference/role_id")
	cfg.SecretIDFile = getEnv("OPENBAO_SECRET_ID_FILE", ".reference/secret_id")
	cfg.Product = getEnv("PRODUCT", "kouventa")
	cfg.DBRole = getEnv("DB_ROLE", "kouventa-app")

	cfg.DBAddr = getEnv("DB_ADDR", "127.0.0.1:6432")
	cfg.DBName = getEnv("DB_NAME", "supportdesk")

	return cfg
}

// getEnv returns the environment variable, or the default if it is not set.
func getEnv(name string, defaultValue string) string {
	value := os.Getenv(name)
	if value == "" {
		return defaultValue
	}
	return value
}
