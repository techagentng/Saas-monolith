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

func TestLoadRejectsInvalidPort(t *testing.T) {
	values := map[string]string{"PORT": "invalid", "POSTGRES_HOST": "db", "POSTGRES_USER": "user", "POSTGRES_PASSWORD": "password", "POSTGRES_DB": "booking"}
	_, err := load(func(key string) (string, bool) { value, ok := values[key]; return value, ok })
	if err == nil {
		t.Fatal("Load() accepted invalid port")
	}
}
