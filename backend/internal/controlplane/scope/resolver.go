package scope

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"rock/internal/controlplane/team"
)

type Clock interface {
	Now() time.Time
}

type MembershipReader interface {
	FindEffectiveMembership(context.Context, uuid.UUID, uuid.UUID, time.Time) (team.MembershipContext, error)
}

type Resolver struct {
	reader MembershipReader
	clock  Clock
}

func NewResolver(reader MembershipReader, clock Clock) *Resolver {
	return &Resolver{reader: reader, clock: clock}
}

func (r *Resolver) ResolveTeam(ctx context.Context, request TeamRequest) (RequestScope, error) {
	teamID, err := parseTeamID(request.HeaderTeamID, true)
	if err != nil {
		return RequestScope{}, err
	}
	if request.ActorUserID == uuid.Nil {
		return RequestScope{}, ErrInvalidTeamContext
	}
	if request.PathTeamID != "" {
		pathTeamID, err := parseTeamID(request.PathTeamID, false)
		if err != nil || pathTeamID != teamID {
			return RequestScope{}, ErrInvalidTeamContext
		}
	}

	membershipContext, err := r.reader.FindEffectiveMembership(ctx, request.ActorUserID, teamID, r.clock.Now().UTC())
	if errors.Is(err, team.ErrAccessDenied) {
		return RequestScope{}, ErrTeamAccessDenied
	}
	if err != nil {
		return RequestScope{}, err
	}
	if membershipContext.Team.ID != teamID || membershipContext.Membership.TeamID != teamID || membershipContext.Membership.UserID != request.ActorUserID {
		return RequestScope{}, ErrTeamAccessDenied
	}
	if !membershipContext.Membership.Role.Valid() {
		return RequestScope{}, ErrTeamAccessDenied
	}

	membershipID := membershipContext.Membership.ID
	role := membershipContext.Membership.Role
	return RequestScope{
		RequestID: request.RequestID, TraceID: request.TraceID, ActorUserID: request.ActorUserID,
		ScopeType: ScopeTeam, ActiveTeamID: &teamID, MembershipID: &membershipID,
		MembershipRole: &role, AuthenticatedAt: request.AuthenticatedAt.UTC(),
	}, nil
}

func parseTeamID(raw string, required bool) (uuid.UUID, error) {
	if raw == "" {
		if required {
			return uuid.Nil, ErrTeamContextRequired
		}
		return uuid.Nil, ErrInvalidTeamContext
	}
	if strings.TrimSpace(raw) != raw || strings.Contains(raw, ",") {
		return uuid.Nil, ErrInvalidTeamContext
	}
	parsed, err := uuid.Parse(raw)
	if err != nil || parsed == uuid.Nil || parsed.String() != raw {
		return uuid.Nil, ErrInvalidTeamContext
	}
	return parsed, nil
}
