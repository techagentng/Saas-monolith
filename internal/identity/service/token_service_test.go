package service

import (
	"crypto/ed25519"
	"strings"
	"testing"
	"time"
)

func TestTokenManagerSignsAndVerifiesClaims(t *testing.T) {
	manager := NewTokenManager(testTokenConfig())
	token, err := manager.Issue("550e8400-e29b-41d4-a716-446655440000", "550e8400-e29b-41d4-a716-446655440001")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	claims, err := manager.Verify(token)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if claims.UserID == "" || claims.SessionID == "" || claims.ExpiresAt <= claims.IssuedAt {
		t.Fatalf("claims = %#v", claims)
	}
	parts := strings.Split(token, ".")
	parts[2] = strings.Repeat("a", len(parts[2]))
	if _, err := manager.Verify(strings.Join(parts, ".")); err == nil {
		t.Fatal("tampered token was accepted")
	}
}

func TestTokenManagerRejectsExpiredToken(t *testing.T) {
	publicKey, privateKey, _ := ed25519.GenerateKey(nil)
	manager := NewTokenManager(TokenConfig{AccessLifetime: -time.Minute, SessionLifetime: time.Hour, PrivateKey: privateKey, PublicKey: publicKey})
	token, err := manager.Issue("550e8400-e29b-41d4-a716-446655440000", "550e8400-e29b-41d4-a716-446655440001")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if _, err := manager.Verify(token); err == nil {
		t.Fatal("expired token was accepted")
	}
}
