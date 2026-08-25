package config

import "testing"

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
