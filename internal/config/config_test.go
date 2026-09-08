package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"strings"
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

func r2Environment() map[string]string {
	values := baseEnvironment()
	values["MEDIA_STORAGE_DRIVER"] = "r2"
	values["MEDIA_R2_BUCKET"] = "bookflow"
	values["MEDIA_R2_ENDPOINT"] = "https://7ffa12f1c0499f3341c35fc21f9347f5.r2.cloudflarestorage.com"
	values["MEDIA_R2_PREFIX"] = "serviceimg"
	values["MEDIA_R2_ACCESS_KEY_ID"] = "access-key-id"
	values["MEDIA_R2_SECRET_ACCESS_KEY"] = "secret-access-key"
	values["MEDIA_PUBLIC_BASE_URL"] = "https://media.iweapps.com"
	return values
}

func TestLoadAcceptsTheR2MediaStorageDriver(t *testing.T) {
	cfg, err := loadFrom(r2Environment())
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if cfg.MediaStorageDriver != "r2" {
		t.Fatalf("MediaStorageDriver = %q, want \"r2\"", cfg.MediaStorageDriver)
	}
	if cfg.MediaR2Bucket != "bookflow" || cfg.MediaR2Prefix != "serviceimg" {
		t.Fatalf("R2 bucket/prefix = %q/%q", cfg.MediaR2Bucket, cfg.MediaR2Prefix)
	}
	if cfg.MediaR2Endpoint != "https://7ffa12f1c0499f3341c35fc21f9347f5.r2.cloudflarestorage.com" {
		t.Fatalf("MediaR2Endpoint = %q", cfg.MediaR2Endpoint)
	}
	if cfg.MediaPublicBaseURL != "https://media.iweapps.com" {
		t.Fatalf("MediaPublicBaseURL = %q", cfg.MediaPublicBaseURL)
	}
}

func TestLoadR2DriverFailsClosedWhenRequiredSettingsAreMissing(t *testing.T) {
	for _, missing := range []string{
		"MEDIA_R2_BUCKET",
		"MEDIA_R2_ENDPOINT",
		"MEDIA_R2_ACCESS_KEY_ID",
		"MEDIA_R2_SECRET_ACCESS_KEY",
		"MEDIA_PUBLIC_BASE_URL",
	} {
		t.Run("missing "+missing, func(t *testing.T) {
			values := r2Environment()
			delete(values, missing)
			_, err := loadFrom(values)
			if err == nil {
				t.Fatalf("Load() accepted an r2 configuration missing %s", missing)
			}
			if !strings.Contains(err.Error(), missing) {
				t.Fatalf("error = %v, want it to name %s", err, missing)
			}
		})
	}
}

// An empty prefix is a valid R2 configuration — objects land at the bucket
// root.
func TestLoadR2DriverAllowsAnEmptyPrefix(t *testing.T) {
	values := r2Environment()
	delete(values, "MEDIA_R2_PREFIX")
	cfg, err := loadFrom(values)
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if cfg.MediaR2Prefix != "" {
		t.Fatalf("MediaR2Prefix = %q, want empty", cfg.MediaR2Prefix)
	}
}

// The secret must never be echoed back in a configuration error.
func TestLoadR2DriverErrorsNeverContainTheSecret(t *testing.T) {
	values := r2Environment()
	values["MEDIA_R2_SECRET_ACCESS_KEY"] = "top-secret-value"
	delete(values, "MEDIA_R2_BUCKET")
	_, err := loadFrom(values)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "top-secret-value") {
		t.Fatalf("error %q leaked the secret", err)
	}
}

func TestLoadR2DriverRejectsANonAbsoluteEndpoint(t *testing.T) {
	values := r2Environment()
	values["MEDIA_R2_ENDPOINT"] = "7ffa12f1c0499f3341c35fc21f9347f5.r2.cloudflarestorage.com"
	if _, err := loadFrom(values); err == nil {
		t.Fatal("Load() accepted a schemeless MEDIA_R2_ENDPOINT")
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
