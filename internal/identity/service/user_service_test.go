package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/identity/model"
)

func TestCreateUserHashesNormalizedPasswordAndEmail(t *testing.T) {
	repository := &fakeRepository{}
	hasher := &fakeHasher{hash: "bcrypt-hash"}
	service := NewUserService(repository, hasher)

	user, err := service.Create(context.Background(), CreateUserInput{Email: " Person@Example.COM ", Password: "password"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if user.Email != "person@example.com" || repository.created.Email != user.Email {
		t.Fatalf("email was not normalized: user=%q persisted=%q", user.Email, repository.created.Email)
	}
	if repository.created.PasswordHash != "bcrypt-hash" {
		t.Fatalf("password hash = %q, want bcrypt hash", repository.created.PasswordHash)
	}
	if hasher.input != "password" {
		t.Fatalf("hasher input = %q, want original password", hasher.input)
	}
	if user.PasswordHash != "bcrypt-hash" {
		t.Fatalf("returned internal user hash = %q, want hash for service use", user.PasswordHash)
	}
}

func TestCreateUserRejectsInvalidInputBeforeDependencies(t *testing.T) {
	repository := &fakeRepository{}
	hasher := &fakeHasher{}
	service := NewUserService(repository, hasher)

	_, err := service.Create(context.Background(), CreateUserInput{Email: "bad", Password: "short"})
	assertCode(t, err, apperrors.CodeValidationFailed)
	if repository.createCalls != 0 || hasher.calls != 0 {
		t.Fatal("dependencies were called for invalid input")
	}
}

func TestCreateUserPreservesDuplicateErrorIdentity(t *testing.T) {
	repository := &fakeRepository{createErr: apperrors.New(apperrors.CodeUserAlreadyExists, "duplicate", nil)}
	service := NewUserService(repository, &fakeHasher{hash: "hash"})

	_, err := service.Create(context.Background(), CreateUserInput{Email: "user@example.com", Password: "password"})
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeUserAlreadyExists {
		t.Fatalf("error = %v, want wrapped USER_ALREADY_EXISTS", err)
	}
}

func TestFindByIDReturnsNotFound(t *testing.T) {
	service := NewUserService(&fakeRepository{findErr: apperrors.New(apperrors.CodeUserNotFound, "missing", nil)}, &fakeHasher{})

	_, err := service.FindByID(context.Background(), "id")
	assertCode(t, err, apperrors.CodeUserNotFound)
}

func TestFindByEmailNormalizesBeforeRepositoryLookup(t *testing.T) {
	repository := &fakeRepository{found: &model.User{ID: "id", Email: "user@example.com"}}
	service := NewUserService(repository, &fakeHasher{})

	_, err := service.FindByEmail(context.Background(), " User@Example.COM ")
	if err != nil {
		t.Fatalf("FindByEmail() error = %v", err)
	}
	if repository.email != "user@example.com" {
		t.Fatalf("repository email = %q, want normalized email", repository.email)
	}
}

func assertCode(t *testing.T, err error, expected apperrors.ErrorCode) {
	t.Helper()
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != expected {
		t.Fatalf("error = %v, want application code %q", err, expected)
	}
}

type fakeRepository struct {
	created     *model.User
	found       *model.User
	createErr   error
	findErr     error
	createCalls int
	email       string
}

func (r *fakeRepository) Create(_ context.Context, user model.User) (*model.User, error) {
	r.createCalls++
	r.created = &user
	if r.createErr != nil {
		return nil, fmt.Errorf("create user: %w", r.createErr)
	}
	return &user, nil
}

func (r *fakeRepository) FindByID(_ context.Context, _ string) (*model.User, error) {
	if r.findErr != nil {
		return nil, fmt.Errorf("find user: %w", r.findErr)
	}
	return r.found, nil
}

func (r *fakeRepository) FindByEmail(_ context.Context, email string) (*model.User, error) {
	r.email = email
	if r.findErr != nil {
		return nil, fmt.Errorf("find user: %w", r.findErr)
	}
	return r.found, nil
}

type fakeHasher struct {
	hash  string
	input string
	calls int
	err   error
}

func (h *fakeHasher) Hash(password string) (string, error) {
	h.calls++
	h.input = password
	if h.err != nil {
		return "", h.err
	}
	return h.hash, nil
}

var _ = time.Time{}
