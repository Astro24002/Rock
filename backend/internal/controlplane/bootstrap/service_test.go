package bootstrap_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"rock/internal/controlplane/bootstrap"
	"rock/internal/controlplane/persistence"
	"rock/internal/controlplane/testkit"
)

var fixedNow = time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)

func TestServiceBootstrapsEmptySchemaOnce(t *testing.T) {
	db := newBootstrapDB(t)
	repo := persistence.NewRepository(db)
	svc := bootstrap.NewService(repo, testkit.FixedClock{Time: fixedNow})
	manifest := validManifest()

	result, err := svc.Apply(context.Background(), manifest)
	require.NoError(t, err)
	require.Equal(t, manifest.User.ID, result.UserID)
	require.Equal(t, manifest.Team.ID, result.TeamID)
	require.NotEqual(t, uuid.Nil, result.MembershipID)
	require.NotEqual(t, uuid.Nil, *result.PlatformRoleBindingID)
	require.Equal(t, int64(1), tableCount(t, db, "users"))
	require.Equal(t, int64(1), tableCount(t, db, "teams"))
	require.Equal(t, int64(1), tableCount(t, db, "team_memberships"))
	require.Equal(t, int64(1), tableCount(t, db, "platform_role_bindings"))

	_, err = svc.Apply(context.Background(), manifest)
	require.ErrorIs(t, err, bootstrap.ErrSchemaNotEmpty)
}

func TestServiceSerializesConcurrentBootstrapAttempts(t *testing.T) {
	db := newBootstrapDB(t)
	repo := persistence.NewRepository(db)
	first := bootstrap.NewService(repo, testkit.FixedClock{Time: fixedNow})
	second := bootstrap.NewService(repo, testkit.FixedClock{Time: fixedNow})
	secondManifest := validManifest()
	secondManifest.User.ID = uuid.New()
	secondManifest.User.Email = "second@example.com"
	secondManifest.Team.ID = uuid.New()
	secondManifest.Team.Slug = "second-team"

	start := make(chan struct{})
	errorsByAttempt := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	apply := func(service *bootstrap.Service, manifest bootstrap.Manifest) {
		ready.Done()
		<-start
		_, err := service.Apply(context.Background(), manifest)
		errorsByAttempt <- err
	}
	go apply(first, validManifest())
	go apply(second, secondManifest)
	ready.Wait()
	close(start)

	var succeeded, refused int
	for range 2 {
		err := <-errorsByAttempt
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, bootstrap.ErrSchemaNotEmpty):
			refused++
		default:
			require.NoError(t, err)
		}
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, refused)
	require.Equal(t, int64(1), tableCount(t, db, "users"))
	require.Equal(t, int64(1), tableCount(t, db, "teams"))
	require.Equal(t, int64(1), tableCount(t, db, "team_memberships"))
	require.Equal(t, int64(1), tableCount(t, db, "platform_role_bindings"))
}

func TestServiceBootstrapsWithoutOptionalPlatformRole(t *testing.T) {
	db := newBootstrapDB(t)
	svc := bootstrap.NewService(persistence.NewRepository(db), testkit.FixedClock{Time: fixedNow})
	manifest := validManifest()
	manifest.PlatformAdmin = false

	result, err := svc.Apply(context.Background(), manifest)
	require.NoError(t, err)
	require.Nil(t, result.PlatformRoleBindingID)
	require.Equal(t, int64(0), tableCount(t, db, "platform_role_bindings"))
}

func TestServiceRejectsInvalidManifestBeforeStore(t *testing.T) {
	store := &recordingStore{}
	svc := bootstrap.NewService(store, testkit.FixedClock{Time: fixedNow})
	tests := []struct {
		name   string
		mutate func(*bootstrap.Manifest)
	}{
		{name: "nil user id", mutate: func(m *bootstrap.Manifest) { m.User.ID = uuid.Nil }},
		{name: "invalid email", mutate: func(m *bootstrap.Manifest) { m.User.Email = "invalid" }},
		{name: "invalid slug", mutate: func(m *bootstrap.Manifest) { m.Team.Slug = "Bad Slug" }},
		{name: "missing team name", mutate: func(m *bootstrap.Manifest) { m.Team.Name = "" }},
		{name: "missing grant reference", mutate: func(m *bootstrap.Manifest) { m.GrantReference = "" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest := validManifest()
			tt.mutate(&manifest)
			_, err := svc.Apply(context.Background(), manifest)
			require.ErrorIs(t, err, bootstrap.ErrInvalidManifest)
		})
	}
	require.Zero(t, store.calls)
}

func TestServiceNormalizesManifest(t *testing.T) {
	store := &recordingStore{}
	svc := bootstrap.NewService(store, testkit.FixedClock{Time: fixedNow})
	manifest := validManifest()
	manifest.User.Email = "  USER@Example.COM "
	manifest.Team.Slug = "  PLATFORM-TEAM "

	_, err := svc.Apply(context.Background(), manifest)
	require.NoError(t, err)
	require.Equal(t, "user@example.com", store.seed.User.Email)
	require.Equal(t, "platform-team", store.seed.Team.Slug)
	require.Equal(t, fixedNow, store.seed.User.CreatedAt)
	require.Equal(t, fixedNow, store.seed.Membership.EffectiveAt)
}

func TestRepositoryBootstrapRollsBackOnWriteFailure(t *testing.T) {
	db := newBootstrapDB(t)
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:fail-platform-role", func(tx *gorm.DB) {
		if tx.Statement.Table == "platform_role_bindings" {
			tx.AddError(errors.New("forced platform role failure"))
		}
	}))
	svc := bootstrap.NewService(persistence.NewRepository(db), testkit.FixedClock{Time: fixedNow})

	_, err := svc.Apply(context.Background(), validManifest())
	require.Error(t, err)
	require.Equal(t, int64(0), tableCount(t, db, "users"))
	require.Equal(t, int64(0), tableCount(t, db, "teams"))
	require.Equal(t, int64(0), tableCount(t, db, "team_memberships"))
	require.Equal(t, int64(0), tableCount(t, db, "platform_role_bindings"))
}

type recordingStore struct {
	calls int
	seed  bootstrap.Seed
	err   error
}

func (s *recordingStore) ApplyBootstrap(_ context.Context, seed bootstrap.Seed) (bootstrap.Result, error) {
	s.calls++
	s.seed = seed
	return bootstrap.Result{UserID: seed.User.ID, TeamID: seed.Team.ID, MembershipID: seed.Membership.ID}, s.err
}

func validManifest() bootstrap.Manifest {
	var manifest bootstrap.Manifest
	manifest.User.ID = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	manifest.User.Email = "user@example.com"
	manifest.User.DisplayName = "Platform Admin"
	manifest.Team.ID = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	manifest.Team.Slug = "platform-team"
	manifest.Team.Name = "Platform Team"
	manifest.PlatformAdmin = true
	manifest.GrantReference = "initial-governance-bootstrap"
	return manifest
}

func newBootstrapDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_foreign_keys=on", uuid.NewString())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, persistence.AutoMigrateForTest(db))
	return db
}

func tableCount(t *testing.T, db *gorm.DB, table string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Table(table).Count(&count).Error)
	return count
}
