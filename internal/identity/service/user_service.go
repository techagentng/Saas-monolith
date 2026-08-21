package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/identity/model"
	"github.com/techagentng/saas-monolith/internal/identity/repository"
)

type CreateUserInput struct {
	Email    string
	Password string
}

type PasswordHasher interface {
	Hash(password string) (string, error)
}

type UserService interface {
	Create(ctx context.Context, input CreateUserInput) (*model.User, error)
	FindByID(ctx context.Context, id string) (*model.User, error)
	FindByEmail(ctx context.Context, email string) (*model.User, error)
}

type userService struct {
	repository repository.UserRepository
	hasher     PasswordHasher
}

func NewUserService(userRepository repository.UserRepository, hasher PasswordHasher) UserService {
	return &userService{repository: userRepository, hasher: hasher}
}

func (s *userService) Create(ctx context.Context, input CreateUserInput) (*model.User, error) {
	email, err := model.NormalizeEmail(input.Email)
	if err != nil {
		return nil, apperrors.New(apperrors.CodeValidationFailed, "invalid email", err)
	}
	if err := model.ValidatePassword(input.Password); err != nil {
		return nil, apperrors.New(apperrors.CodeValidationFailed, "invalid password", err)
	}

	hash, err := s.hasher.Hash(input.Password)
	if err != nil {
		return nil, fmt.Errorf("hashing user password: %w", err)
	}
	user := model.User{ID: newID(), Email: email, PasswordHash: hash, Status: model.StatusActive}
	created, err := s.repository.Create(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("creating user: %w", err)
	}
	return created, nil
}

func (s *userService) FindByID(ctx context.Context, id string) (*model.User, error) {
	user, err := s.repository.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("finding user by id: %w", err)
	}
	return user, nil
}

func (s *userService) FindByEmail(ctx context.Context, email string) (*model.User, error) {
	normalized, err := model.NormalizeEmail(email)
	if err != nil {
		return nil, apperrors.New(apperrors.CodeValidationFailed, "invalid email", err)
	}
	user, err := s.repository.FindByEmail(ctx, normalized)
	if err != nil {
		return nil, fmt.Errorf("finding user by email: %w", err)
	}
	return user, nil
}

func newID() string {
	return uuid.NewString()
}
