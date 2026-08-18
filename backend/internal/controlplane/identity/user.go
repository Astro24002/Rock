package identity

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrUserNotFound = errors.New("user not found")

type UserStatus string

const (
	UserStatusActive    UserStatus = "active"
	UserStatusSuspended UserStatus = "suspended"
	UserStatusDisabled  UserStatus = "disabled"
)

// User stores the identity profile used by the control plane. Email is expected
// to be normalized before a User is constructed.
type User struct {
	ID             uuid.UUID
	Email          string
	DisplayName    string
	Status         UserStatus
	ProfileVersion int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (u User) IsActive() bool { return u.Status == UserStatusActive }
