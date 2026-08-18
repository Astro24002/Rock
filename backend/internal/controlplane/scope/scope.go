package scope

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"rock/internal/controlplane/team"
)

var (
	ErrTeamContextRequired = errors.New("active team context is required")
	ErrInvalidTeamContext  = errors.New("active team context is invalid")
	ErrTeamAccessDenied    = errors.New("team access denied")
)

type ScopeType string

const (
	ScopeTeam     ScopeType = "team"
	ScopePlatform ScopeType = "platform"
)

type RequestScope struct {
	RequestID       string
	TraceID         string
	ActorUserID     uuid.UUID
	ScopeType       ScopeType
	ActiveTeamID    *uuid.UUID
	MembershipID    *uuid.UUID
	MembershipRole  *team.Role
	AuthenticatedAt time.Time
}

type TeamRequest struct {
	RequestID       string
	TraceID         string
	ActorUserID     uuid.UUID
	HeaderTeamID    string
	PathTeamID      string
	AuthenticatedAt time.Time
}
