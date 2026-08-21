package service

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestBcryptHasherCreatesVerifiableHash(t *testing.T) {
	hasher := NewBcryptHasher()
	hash, err := hasher.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	if hash == "correct horse battery staple" {
		t.Fatal("hasher returned the plaintext password")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("correct horse battery staple")); err != nil {
		t.Fatalf("generated hash is not verifiable: %v", err)
	}
}
