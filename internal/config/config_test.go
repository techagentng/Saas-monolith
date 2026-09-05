package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"
)

func TestLoadRequiresDatabaseSettings(t *testing.T) {
	_, err := load(func(key string) (string, bool) {
		if key == "POSTGRES_HOST" {
			return "db", true
		}
		return "", false
	})
	if err == nil {
		t.Fatal("Load() accepted missing database settings")
	}
}

func TestLoadRequiresPostgresPort(t *testing.T) {
	values := map[string]string{"POSTGRES_HOST": "db", "POSTGRES_USER": "user", "POSTGRES_PASSWORD": "password", "POSTGRES_DB": "booking"}
	_, err := load(func(key string) (string, bool) { value, ok := values[key]; return value, ok })
	if err == nil {
		t.Fatal("Load() accepted missing POSTGRES_PORT")
	}
	values["POSTGRES_PORT"] = "5433"
	cfg, err := load(func(key string) (string, bool) { value, ok := values[key]; return value, ok })
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if cfg.PostgresPort != 5433 {
		t.Fatalf("PostgresPort = %d, want 5433", cfg.PostgresPort)
	}
}

func TestLoadDefaultsMediaStorageToLocalWithADevelopmentPublicURL(t *testing.T) {
	cfg, err := loadFrom(baseEnvironment())
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if cfg.MediaStorageDriver != "local" {
		t.Fatalf("MediaStorageDriver = %q, want \"local\"", cfg.MediaStorageDriver)
	}
	if cfg.MediaLocalDir != "uploads" {
		t.Fatalf("MediaLocalDir = %q, want \"uploads\"", cfg.MediaLocalDir)
	}
	// Host defaults to 127.0.0.1 and Port to 8080; the dev default base URL
	// must be built from exactly those, not hardcoded.
	if cfg.MediaPublicBaseURL != "http://127.0.0.1:8080/media" {
		t.Fatalf("MediaPublicBaseURL = %q", cfg.MediaPublicBaseURL)
	}
}

func TestLoadRejectsAnUnsupportedMediaStorageDriver(t *testing.T) {
	values := baseEnvironment()
	values["MEDIA_STORAGE_DRIVER"] = "s3"
	if _, err := loadFrom(values); err == nil {
		t.Fatal("Load() accepted an unsupported MEDIA_STORAGE_DRIVER")
	}
}

// Production must never inherit "the origin this process happens to bind
// to" (127.0.0.1) as the origin it tells anonymous browsers to fetch images
// from — that address means "this machine" to every browser, not the API.
func TestLoadRequiresExplicitMediaPublicBaseURLInProduction(t *testing.T) {
	values := baseEnvironment()
	values["APP_ENV"] = "production"
	privateKey, publicKey := generateTestKeyPair(t)
	values["ED25519_PRIVATE_KEY"] = privateKey
	values["ED25519_PUBLIC_KEY"] = publicKey

	if _, err := loadFrom(values); err == nil {
		t.Fatal("Load() accepted production with no MEDIA_PUBLIC_BASE_URL")
	}

	values["MEDIA_PUBLIC_BASE_URL"] = "https://cdn.example.com/media"
	cfg, err := loadFrom(values)
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if cfg.MediaPublicBaseURL != "https://cdn.example.com/media" {
		t.Fatalf("MediaPublicBaseURL = %q", cfg.MediaPublicBaseURL)
	}
}

func generateTestKeyPair(t *testing.T) (privateKey string, publicKey string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(priv), base64.StdEncoding.EncodeToString(pub)
}

func TestLoadParsesAllowedOrigins(t *testing.T) {
	values := map[string]string{"POSTGRES_HOST": "db", "POSTGRES_USER": "user", "POSTGRES_PASSWORD": "password", "POSTGRES_DB": "booking", "POSTGRES_PORT": "5433", "ALLOWED_ORIGINS": " http://localhost:3000 ,https://app.example.com,"}
	cfg, err := load(func(key string) (string, bool) { value, ok := values[key]; return value, ok })
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	want := []string{"http://localhost:3000", "https://app.example.com"}
	if len(cfg.AllowedOrigins) != len(want) || cfg.AllowedOrigins[0] != want[0] || cfg.AllowedOrigins[1] != want[1] {
		t.Fatalf("AllowedOrigins = %#v, want %#v", cfg.AllowedOrigins, want)
	}
}

func TestLoadDefaultsAllowedOriginsToEmpty(t *testing.T) {
	values := map[string]string{"POSTGRES_HOST": "db", "POSTGRES_USER": "user", "POSTGRES_PASSWORD": "password", "POSTGRES_DB": "booking", "POSTGRES_PORT": "5433"}
	cfg, err := load(func(key string) (string, bool) { value, ok := values[key]; return value, ok })
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if len(cfg.AllowedOrigins) != 0 {
		t.Fatalf("AllowedOrigins = %#v, want empty", cfg.AllowedOrigins)
	}
}

func TestLoadRejectsInvalidPort(t *testing.T) {
	values := map[string]string{"PORT": "invalid", "POSTGRES_HOST": "db", "POSTGRES_USER": "user", "POSTGRES_PASSWORD": "password", "POSTGRES_DB": "booking"}
	_, err := load(func(key string) (string, bool) { value, ok := values[key]; return value, ok })
	if err == nil {
		t.Fatal("Load() accepted invalid port")
	}
}
