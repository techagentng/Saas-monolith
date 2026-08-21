package model

import (
	"testing"
	"time"
)

func TestNormalizeEmail(t *testing.T) {
	normalized, err := NormalizeEmail("  Person@Example.COM  ")
	if err != nil {
		t.Fatalf("NormalizeEmail() error = %v", err)
	}
	if normalized != "person@example.com" {
		t.Fatalf("normalized email = %q, want %q", normalized, "person@example.com")
	}
}

func TestNormalizeEmailRejectsInvalidAddresses(t *testing.T) {
	for _, email := range []string{"", "person", "@example.com", "person@", "person@@example.com"} {
		t.Run(email, func(t *testing.T) {
			if _, err := NormalizeEmail(email); err == nil {
				t.Fatalf("NormalizeEmail(%q) expected an error", email)
			}
		})
	}
}

func TestValidatePasswordLength(t *testing.T) {
	if err := ValidatePassword("short"); err == nil {
		t.Fatal("short password was accepted")
	}
	if err := ValidatePassword("12345678"); err != nil {
		t.Fatalf("minimum-length password rejected: %v", err)
	}
	if err := ValidatePassword(string(make([]byte, 129))); err == nil {
		t.Fatal("password longer than 128 bytes was accepted")
	}
}

func TestPublicUserDoesNotContainPasswordHash(t *testing.T) {
	user := User{ID: "id", Email: "user@example.com", PasswordHash: "hash", Status: StatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	public := user.Public()
	if public.Email != user.Email || public.ID != user.ID || public.Status != user.Status {
		t.Fatalf("public user = %#v, missing safe user fields", public)
	}
}
