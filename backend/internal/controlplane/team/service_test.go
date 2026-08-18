package team_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"rock/internal/controlplane/identity"
	"rock/internal/controlplane/team"
	"rock/internal/controlplane/testkit"
)

func TestMembershipEffectiveAt(t *testing.T) {
	now := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		status     team.MembershipStatus
		effective  time.Time
		expiresAt  *time.Time
		expectLive bool
	}{
		{name: "active membership", status: team.MembershipStatusActive, effective: now.Add(-time.Minute), expectLive: true},
		{name: "effective boundary", status: team.MembershipStatusActive, effective: now, expectLive: true},
		{name: "future expiry", status: team.MembershipStatusActive, effective: now.Add(-time.Minute), expiresAt: timePointer(now.Add(time.Minute)), expectLive: true},
		{name: "future effective time", status: team.MembershipStatusActive, effective: now.Add(time.Minute), expectLive: false},
		{name: "expiry boundary", status: team.MembershipStatusActive, effective: now.Add(-time.Minute), expiresAt: &now, expectLive: false},
		{name: "revoked membership", status: team.MembershipStatusRevoked, effective: now.Add(-time.Minute), expectLive: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			membership := team.Membership{Status: tt.status, EffectiveAt: tt.effective, ExpiresAt: tt.expiresAt}
			if got := membership.EffectiveAtTime(now); got != tt.expectLive {
				t.Fatalf("EffectiveAtTime() = %v, want %v", got, tt.expectLive)
			}
		})
	}
}

func TestFixedRoles(t *testing.T) {
	for _, role := range []team.Role{team.RoleViewer, team.RoleDeveloper, team.RoleAdmin} {
		if !role.Valid() {
			t.Errorf("team role %q should be valid", role)
		}
	}
	for _, role := range []team.PlatformRole{team.PlatformRoleAdmin, team.PlatformRoleAssetOperator, team.PlatformRoleAuditor} {
		if !role.Valid() {
			t.Errorf("platform role %q should be valid", role)
		}
	}
	for _, role := range []team.Role{"owner", "", "PLATFORM_ADMIN"} {
		if role.Valid() {
			t.Errorf("team role %q should be invalid", role)
		}
	}
	for _, role := range []team.PlatformRole{"owner", "", "admin"} {
		if role.Valid() {
			t.Errorf("platform role %q should be invalid", role)
		}
	}
}

func TestUserIsActive(t *testing.T) {
	tests := []struct {
		status identity.UserStatus
		want   bool
	}{
		{status: identity.UserStatusActive, want: true},
		{status: identity.UserStatusSuspended, want: false},
		{status: identity.UserStatusDisabled, want: false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := (identity.User{ID: uuid.New(), Status: tt.status}).IsActive(); got != tt.want {
				t.Fatalf("IsActive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlatformScopeIsFixed(t *testing.T) {
	if !team.PlatformScopePlatform.Valid() {
		t.Fatal("platform scope should be valid")
	}
	if team.PlatformScope("global").Valid() {
		t.Fatal("global scope should be invalid")
	}
}

func timePointer(value time.Time) *time.Time { return &value }

func TestServiceUsesCurrentTimeForMembershipQueries(t *testing.T) {
	now := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	userID := uuid.New()
	teamID := uuid.New()
	reader := &fakeMembershipReader{
		list: []team.MembershipContext{{Team: team.Team{ID: teamID}}},
		one:  team.MembershipContext{Team: team.Team{ID: teamID}},
	}
	service := team.NewService(reader, testkit.FixedClock{Time: now})

	listed, err := service.ListEffectiveMemberships(context.Background(), userID)
	if err != nil || len(listed) != 1 {
		t.Fatalf("ListEffectiveMemberships() = %#v, %v", listed, err)
	}
	resolved, err := service.ResolveEffectiveMembership(context.Background(), userID, teamID)
	if err != nil || resolved.Team.ID != teamID {
		t.Fatalf("ResolveEffectiveMembership() = %#v, %v", resolved, err)
	}
	if len(reader.times) != 2 || !reader.times[0].Equal(now) || !reader.times[1].Equal(now) {
		t.Fatalf("reader times = %#v, want two %v values", reader.times, now)
	}
}

func TestServicePreservesMembershipReaderErrors(t *testing.T) {
	storeErr := errors.New("store unavailable")
	service := team.NewService(&fakeMembershipReader{err: storeErr}, testkit.FixedClock{Time: time.Now().UTC()})
	_, err := service.ListEffectiveMemberships(context.Background(), uuid.New())
	if !errors.Is(err, storeErr) {
		t.Fatalf("ListEffectiveMemberships() error = %v, want %v", err, storeErr)
	}
}

type fakeMembershipReader struct {
	list  []team.MembershipContext
	one   team.MembershipContext
	err   error
	times []time.Time
}

func (r *fakeMembershipReader) ListEffectiveMemberships(_ context.Context, _ uuid.UUID, at time.Time) ([]team.MembershipContext, error) {
	r.times = append(r.times, at)
	return r.list, r.err
}

func (r *fakeMembershipReader) FindEffectiveMembership(_ context.Context, _, _ uuid.UUID, at time.Time) (team.MembershipContext, error) {
	r.times = append(r.times, at)
	return r.one, r.err
}
