package identity_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"rock/internal/controlplane/identity"
)

func TestServiceResolvesActiveUser(t *testing.T) {
	user := identity.User{ID: uuid.New(), Status: identity.UserStatusActive}
	service := identity.NewService(fakeUserReader{user: user})

	got, err := service.ResolveActiveUser(context.Background(), user.ID)
	require.NoError(t, err)
	require.Equal(t, user.ID, got.ID)
}

func TestServiceRejectsUnknownAndInactiveUsers(t *testing.T) {
	_, err := identity.NewService(fakeUserReader{err: identity.ErrUserNotFound}).ResolveActiveUser(context.Background(), uuid.New())
	require.ErrorIs(t, err, identity.ErrNotRegistered)

	for _, status := range []identity.UserStatus{identity.UserStatusSuspended, identity.UserStatusDisabled} {
		_, err = identity.NewService(fakeUserReader{user: identity.User{ID: uuid.New(), Status: status}}).ResolveActiveUser(context.Background(), uuid.New())
		require.ErrorIs(t, err, identity.ErrInactive)
	}
}

func TestServicePreservesReaderFailures(t *testing.T) {
	storeErr := errors.New("store unavailable")
	_, err := identity.NewService(fakeUserReader{err: storeErr}).ResolveActiveUser(context.Background(), uuid.New())
	require.ErrorIs(t, err, storeErr)
}

type fakeUserReader struct {
	user identity.User
	err  error
}

func (f fakeUserReader) FindUser(context.Context, uuid.UUID) (identity.User, error) {
	return f.user, f.err
}
