package persistence

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"rock/internal/controlplane/identity"
	"rock/internal/controlplane/team"
)

func TestRepositoryListsOnlyEffectiveTeamsInSlugOrder(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db)
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	user := seedUser(t, db, identity.UserStatusActive, "user@example.com")
	zeta := seedTeam(t, db, "zeta", team.TeamStatusActive)
	alpha := seedTeam(t, db, "alpha", team.TeamStatusActive)
	suspended := seedTeam(t, db, "hidden", team.TeamStatusSuspended)
	seedMembership(t, db, user.ID, zeta.ID, team.RoleDeveloper, team.MembershipStatusActive, now.Add(-time.Hour), nil)
	seedMembership(t, db, user.ID, alpha.ID, team.RoleViewer, team.MembershipStatusActive, now.Add(-time.Hour), nil)
	seedMembership(t, db, user.ID, suspended.ID, team.RoleAdmin, team.MembershipStatusActive, now.Add(-time.Hour), nil)

	got, err := repo.ListEffectiveMemberships(context.Background(), uuid.MustParse(user.ID), now)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, []string{"alpha", "zeta"}, []string{got[0].Team.Slug, got[1].Team.Slug})
	require.Equal(t, []team.Role{team.RoleViewer, team.RoleDeveloper}, []team.Role{got[0].Membership.Role, got[1].Membership.Role})
}

func TestRepositoryFiltersMembershipTimeWindow(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db)
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	user := seedUser(t, db, identity.UserStatusActive, "time@example.com")
	future := seedTeam(t, db, "future", team.TeamStatusActive)
	expired := seedTeam(t, db, "expired", team.TeamStatusActive)
	seedMembership(t, db, user.ID, future.ID, team.RoleViewer, team.MembershipStatusActive, now.Add(time.Minute), nil)
	seedMembership(t, db, user.ID, expired.ID, team.RoleViewer, team.MembershipStatusActive, now.Add(-time.Hour), timePointer(now))

	got, err := repo.ListEffectiveMemberships(context.Background(), uuid.MustParse(user.ID), now)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestRepositoryFindEffectiveMembership(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db)
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	user := seedUser(t, db, identity.UserStatusActive, "member@example.com")
	target := seedTeam(t, db, "target", team.TeamStatusActive)
	seedMembership(t, db, user.ID, target.ID, team.RoleAdmin, team.MembershipStatusActive, now, nil)

	got, err := repo.FindEffectiveMembership(context.Background(), uuid.MustParse(user.ID), uuid.MustParse(target.ID), now)
	require.NoError(t, err)
	require.Equal(t, team.RoleAdmin, got.Membership.Role)
	require.Equal(t, "target", got.Team.Slug)
}

func TestRepositoryDoesNotSynthesizeTeamAccessFromPlatformRole(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db)
	user := seedUser(t, db, identity.UserStatusActive, "platform@example.com")
	target := seedTeam(t, db, "target", team.TeamStatusActive)
	seedPlatformRole(t, db, user.ID, team.PlatformRoleAdmin)

	_, err := repo.FindEffectiveMembership(context.Background(), uuid.MustParse(user.ID), uuid.MustParse(target.ID), time.Now().UTC())
	require.ErrorIs(t, err, team.ErrAccessDenied)
}

func TestRepositoryFindUser(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db)
	row := seedUser(t, db, identity.UserStatusActive, "USER@example.com")

	got, err := repo.FindUser(context.Background(), uuid.MustParse(row.ID))
	require.NoError(t, err)
	require.Equal(t, "user@example.com", got.Email)
	require.True(t, got.IsActive())

	_, err = repo.FindUser(context.Background(), uuid.New())
	require.ErrorIs(t, err, identity.ErrUserNotFound)
}

func TestSchemaEnforcesUniqueEmailAndSlug(t *testing.T) {
	db := newTestDB(t)
	seedUser(t, db, identity.UserStatusActive, "unique@example.com")
	require.Error(t, db.Create(&userRow{ID: uuid.NewString(), Email: "unique@example.com", Status: string(identity.UserStatusActive), ProfileVersion: 1}).Error)
	seedTeam(t, db, "unique", team.TeamStatusActive)
	require.Error(t, db.Create(&teamRow{ID: uuid.NewString(), Slug: "unique", Name: "Duplicate", Status: string(team.TeamStatusActive), ConfigVersion: 1}).Error)
}

func TestSchemaEnforcesMembershipForeignKeys(t *testing.T) {
	db := newTestDB(t)
	err := db.Create(&membershipRow{
		ID: uuid.NewString(), UserID: uuid.NewString(), TeamID: uuid.NewString(), Role: string(team.RoleViewer),
		Status: string(team.MembershipStatusActive), EffectiveAt: time.Now().UTC(), Source: "test", Version: 1,
	}).Error
	require.Error(t, err)
}

func TestSchemaAllowsOnlyOneActiveMembershipPerUserTeam(t *testing.T) {
	db := newTestDB(t)
	user := seedUser(t, db, identity.UserStatusActive, "active@example.com")
	target := seedTeam(t, db, "active", team.TeamStatusActive)
	seedMembership(t, db, user.ID, target.ID, team.RoleViewer, team.MembershipStatusActive, time.Now().Add(-time.Hour), nil)

	err := db.Create(&membershipRow{
		ID: uuid.NewString(), UserID: user.ID, TeamID: target.ID, Role: string(team.RoleAdmin),
		Status: string(team.MembershipStatusActive), EffectiveAt: time.Now().UTC(), Source: "test", Version: 1,
	}).Error
	require.Error(t, err)

	require.NoError(t, db.Create(&membershipRow{
		ID: uuid.NewString(), UserID: user.ID, TeamID: target.ID, Role: string(team.RoleViewer),
		Status: string(team.MembershipStatusRevoked), EffectiveAt: time.Now().UTC(), Source: "history", Version: 1,
	}).Error)
}

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_foreign_keys=on", uuid.NewString())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, AutoMigrateForTest(db))
	return db
}

func seedUser(t *testing.T, db *gorm.DB, status identity.UserStatus, email string) userRow {
	t.Helper()
	row := userRow{ID: uuid.NewString(), Email: normalizeEmail(email), DisplayName: "User", Status: string(status), ProfileVersion: 1}
	require.NoError(t, db.Create(&row).Error)
	return row
}

func seedTeam(t *testing.T, db *gorm.DB, slug string, status team.TeamStatus) teamRow {
	t.Helper()
	row := teamRow{ID: uuid.NewString(), Slug: slug, Name: slug, Status: string(status), ConfigVersion: 1}
	require.NoError(t, db.Create(&row).Error)
	return row
}

func seedMembership(t *testing.T, db *gorm.DB, userID, teamID string, role team.Role, status team.MembershipStatus, effectiveAt time.Time, expiresAt *time.Time) membershipRow {
	t.Helper()
	row := membershipRow{ID: uuid.NewString(), UserID: userID, TeamID: teamID, Role: string(role), Status: string(status), EffectiveAt: effectiveAt, ExpiresAt: expiresAt, Source: "test", Version: 1}
	require.NoError(t, db.Create(&row).Error)
	return row
}

func seedPlatformRole(t *testing.T, db *gorm.DB, userID string, role team.PlatformRole) platformRoleBindingRow {
	t.Helper()
	row := platformRoleBindingRow{ID: uuid.NewString(), UserID: userID, Role: string(role), Scope: string(team.PlatformScopePlatform), EffectiveAt: time.Now().Add(-time.Hour), GrantReference: "bootstrap", Status: string(team.BindingStatusActive), Version: 1}
	require.NoError(t, db.Create(&row).Error)
	return row
}

func timePointer(value time.Time) *time.Time { return &value }
