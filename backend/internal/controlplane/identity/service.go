package identity

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

var (
	ErrNotRegistered = errors.New("identity not registered")
	ErrInactive      = errors.New("identity inactive")
)

type Reader interface {
	FindUser(context.Context, uuid.UUID) (User, error)
}

type Service struct {
	reader Reader
}

func NewService(reader Reader) *Service { return &Service{reader: reader} }

func (s *Service) ResolveActiveUser(ctx context.Context, id uuid.UUID) (User, error) {
	user, err := s.reader.FindUser(ctx, id)
	if errors.Is(err, ErrUserNotFound) {
		return User{}, ErrNotRegistered
	}
	if err != nil {
		return User{}, err
	}
	if !user.IsActive() {
		return User{}, ErrInactive
	}
	return user, nil
}
