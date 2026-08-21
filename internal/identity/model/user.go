package model

import (
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

type Status string

const (
	StatusActive   Status = "ACTIVE"
	StatusDisabled Status = "DISABLED"
)

type User struct {
	ID           string
	Email        string
	PasswordHash string
	Status       Status
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type PublicUser struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Status    Status    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (u User) Public() PublicUser {
	return PublicUser{ID: u.ID, Email: u.Email, Status: u.Status, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt}
}

func NormalizeEmail(email string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(email))
	parsed, err := mail.ParseAddress(normalized)
	if err != nil || parsed.Address != normalized || !strings.Contains(normalized, "@") {
		return "", fmt.Errorf("invalid email")
	}
	return normalized, nil
}

func ValidatePassword(password string) error {
	length := utf8.RuneCountInString(password)
	if !utf8.ValidString(password) || length < 8 || length > 128 {
		return fmt.Errorf("invalid password")
	}
	return nil
}
