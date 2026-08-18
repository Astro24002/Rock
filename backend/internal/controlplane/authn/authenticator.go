package authn

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrInvalidToken = errors.New("invalid identity token")

type Principal struct {
	Subject         uuid.UUID
	Email           string
	AuthenticatedAt time.Time
}

type Authenticator interface {
	Authenticate(context.Context, string) (Principal, error)
}
