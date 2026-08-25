package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env              string
	Host             string
	Port             int
	PostgresHost     string
	PostgresPort     int
	PostgresUser     string
	PostgresPassword string
	PostgresDB       string
	PostgresSSLMode  string
	MigrationsDir    string
	AccessLifetime   time.Duration
	SessionLifetime  time.Duration
	PrivateKey       ed25519.PrivateKey
	PublicKey        ed25519.PublicKey
	AllowedOrigins   []string
}

func Load() (Config, error) { return load(os.LookupEnv) }

func load(lookup func(string) (string, bool)) (Config, error) {
	c := Config{
		Env:             get(lookup, "APP_ENV", "development"),
		Host:            get(lookup, "HOST", "127.0.0.1"),
		MigrationsDir:   get(lookup, "MIGRATIONS_DIR", "migrations"),
		PostgresSSLMode: get(lookup, "POSTGRES_SSLMODE", "disable"),
	}
	var err error
	if c.Port, err = integer(lookup, "PORT", 8080); err != nil {
		return Config{}, err
	}
	if c.PostgresPort, err = requiredInteger(lookup, "POSTGRES_PORT"); err != nil {
		return Config{}, err
	}
	for key, target := range map[string]*string{"POSTGRES_HOST": &c.PostgresHost, "POSTGRES_USER": &c.PostgresUser, "POSTGRES_PASSWORD": &c.PostgresPassword, "POSTGRES_DB": &c.PostgresDB} {
		*target = get(lookup, key, "")
		if *target == "" {
			return Config{}, fmt.Errorf("%s is required", key)
		}
	}
	if c.AccessLifetime, err = duration(lookup, "ACCESS_TOKEN_TTL", 15*time.Minute); err != nil {
		return Config{}, err
	}
	if c.SessionLifetime, err = duration(lookup, "SESSION_TTL", 24*time.Hour); err != nil {
		return Config{}, err
	}
	c.AllowedOrigins = originList(lookup, "ALLOWED_ORIGINS")
	privateValue := get(lookup, "ED25519_PRIVATE_KEY", "")
	publicValue := get(lookup, "ED25519_PUBLIC_KEY", "")
	privateSet, publicSet := privateValue != "", publicValue != ""
	if !privateSet && !publicSet && c.Env != "production" && c.Env != "prod" {
		c.PublicKey, c.PrivateKey, _ = ed25519.GenerateKey(nil)
		return c, nil
	}
	if !privateSet || !publicSet {
		return Config{}, fmt.Errorf("ED25519_PRIVATE_KEY and ED25519_PUBLIC_KEY are required")
	}
	if c.PrivateKey, err = decodeKey(privateValue, ed25519.PrivateKeySize); err != nil {
		return Config{}, fmt.Errorf("ED25519_PRIVATE_KEY: %w", err)
	}
	if c.PublicKey, err = decodeKey(publicValue, ed25519.PublicKeySize); err != nil {
		return Config{}, fmt.Errorf("ED25519_PUBLIC_KEY: %w", err)
	}
	return c, nil
}

func get(lookup func(string) (string, bool), key, fallback string) string {
	if value, ok := lookup(key); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func integer(lookup func(string) (string, bool), key string, fallback int) (int, error) {
	value := get(lookup, key, strconv.Itoa(fallback))
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return parsed, nil
}

func requiredInteger(lookup func(string) (string, bool), key string) (int, error) {
	if get(lookup, key, "") == "" {
		return 0, fmt.Errorf("%s is required", key)
	}
	return integer(lookup, key, 0)
}

func duration(lookup func(string) (string, bool), key string, fallback time.Duration) (time.Duration, error) {
	value := get(lookup, key, fallback.String())
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return parsed, nil
}

func originList(lookup func(string) (string, bool), key string) []string {
	raw := get(lookup, key, "")
	if raw == "" {
		return nil
	}
	var origins []string
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			origins = append(origins, trimmed)
		}
	}
	return origins
}

func decodeKey(value string, size int) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(value)
	}
	if err != nil || len(decoded) != size {
		return nil, fmt.Errorf("must be base64 encoded and %d bytes", size)
	}
	return decoded, nil
}
