package scope_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"rock/internal/controlplane/scope"
	"rock/internal/controlplane/team"
	"rock/internal/controlplane/testkit"
)

var scopeNow = time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)

func TestResolverBuildsOneTeamScope(t *testing.T) {
	userID := uuid.New()
	teamID := uuid.New()
	membershipID := uuid.New()
	reader := &countingTeamReader{context: team.MembershipContext{
		Team:       team.Team{ID: teamID, Status: team.TeamStatusActive},
		Membership: team.Membership{ID: membershipID, TeamID: teamID, UserID: userID, Role: team.RoleDeveloper, Status: team.MembershipStatusActive},
	}}
	resolver := scope.NewResolver(reader, testkit.FixedClock{Time: scopeNow})

	got, err := resolver.ResolveTeam(context.Background(), scope.TeamRequest{
		RequestID: "req-1", TraceID: "trace-1", ActorUserID: userID,
		HeaderTeamID: teamID.String(), PathTeamID: teamID.String(), AuthenticatedAt: scopeNow.Add(-time.Minute),
	})
	require.NoError(t, err)
	require.Equal(t, scope.ScopeTeam, got.ScopeType)
	require.Equal(t, teamID, *got.ActiveTeamID)
	require.Equal(t, membershipID, *got.MembershipID)
	require.Equal(t, team.RoleDeveloper, *got.MembershipRole)
	require.Equal(t, 1, reader.calls)
	require.Equal(t, scopeNow, reader.at)
}

func TestResolverRejectsInvalidContextBeforeLookup(t *testing.T) {
	teamA := uuid.New()
	teamB := uuid.New()
	tests := []struct {
		name   string
		header string
		path   string
		want   error
	}{
		{name: "missing", want: scope.ErrTeamContextRequired},
		{name: "not uuid", header: "team-a", want: scope.ErrInvalidTeamContext},
		{name: "uppercase uuid", header: stringsUpper(teamA.String()), want: scope.ErrInvalidTeamContext},
		{name: "surrounding whitespace", header: " " + teamA.String(), want: scope.ErrInvalidTeamContext},
		{name: "comma list", header: teamA.String() + "," + teamB.String(), want: scope.ErrInvalidTeamContext},
		{name: "path mismatch", header: teamA.String(), path: teamB.String(), want: scope.ErrInvalidTeamContext},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &countingTeamReader{}
			resolver := scope.NewResolver(reader, testkit.FixedClock{Time: scopeNow})
			_, err := resolver.ResolveTeam(context.Background(), scope.TeamRequest{ActorUserID: uuid.New(), HeaderTeamID: tt.header, PathTeamID: tt.path})
			require.ErrorIs(t, err, tt.want)
			require.Zero(t, reader.calls)
		})
	}
}

func TestResolverHidesMembershipLookupOutcome(t *testing.T) {
	reader := &countingTeamReader{err: team.ErrAccessDenied}
	resolver := scope.NewResolver(reader, testkit.FixedClock{Time: scopeNow})
	teamID := uuid.New()
	_, err := resolver.ResolveTeam(context.Background(), scope.TeamRequest{ActorUserID: uuid.New(), HeaderTeamID: teamID.String()})
	require.ErrorIs(t, err, scope.ErrTeamAccessDenied)
}

func TestResolverPreservesInfrastructureFailure(t *testing.T) {
	storeErr := errors.New("database unavailable")
	reader := &countingTeamReader{err: storeErr}
	resolver := scope.NewResolver(reader, testkit.FixedClock{Time: scopeNow})
	teamID := uuid.New()
	_, err := resolver.ResolveTeam(context.Background(), scope.TeamRequest{ActorUserID: uuid.New(), HeaderTeamID: teamID.String()})
	require.ErrorIs(t, err, storeErr)
}

type countingTeamReader struct {
	context team.MembershipContext
	err     error
	calls   int
	at      time.Time
}

func (r *countingTeamReader) FindEffectiveMembership(_ context.Context, _, _ uuid.UUID, at time.Time) (team.MembershipContext, error) {
	r.calls++
	r.at = at
	return r.context, r.err
}

func stringsUpper(value string) string {
	bytes := []byte(value)
	for i := range bytes {
		if bytes[i] >= 'a' && bytes[i] <= 'f' {
			bytes[i] -= 'a' - 'A'
		}
	}
	return string(bytes)
}
