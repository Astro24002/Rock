package team

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrAccessDenied = errors.New("team access denied")

type TeamStatus string

const (
	TeamStatusActive    TeamStatus = "active"
	TeamStatusSuspended TeamStatus = "suspended"
	TeamStatusDisabled  TeamStatus = "disabled"
)

type MembershipStatus string

const (
	MembershipStatusActive  MembershipStatus = "active"
	MembershipStatusRevoked MembershipStatus = "revoked"
	MembershipStatusExpired MembershipStatus = "expired"
)

type BindingStatus string

const (
	BindingStatusActive  BindingStatus = "active"
	BindingStatusRevoked BindingStatus = "revoked"
	BindingStatusExpired BindingStatus = "expired"
)

type Role string

const (
	RoleViewer    Role = "viewer"
	RoleDeveloper Role = "developer"
	RoleAdmin     Role = "admin"
)

func (r Role) Valid() bool {
	switch r {
	case RoleViewer, RoleDeveloper, RoleAdmin:
		return true
	default:
		return false
	}
}

type PlatformRole string

const (
	PlatformRoleAdmin         PlatformRole = "platform_admin"
	PlatformRoleAssetOperator PlatformRole = "asset_operator"
	PlatformRoleAuditor       PlatformRole = "auditor"
)

func (r PlatformRole) Valid() bool {
	switch r {
	case PlatformRoleAdmin, PlatformRoleAssetOperator, PlatformRoleAuditor:
		return true
	default:
		return false
	}
}

type PlatformScope string

const PlatformScopePlatform PlatformScope = "platform"

func (s PlatformScope) Valid() bool { return s == PlatformScopePlatform }

type Team struct {
	ID            uuid.UUID
	Slug          string
	Name          string
	Status        TeamStatus
	ConfigVersion int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Membership struct {
	ID          uuid.UUID
	TeamID      uuid.UUID
	UserID      uuid.UUID
	Role        Role
	Status      MembershipStatus
	EffectiveAt time.Time
	ExpiresAt   *time.Time
	Source      string
	Version     int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (m Membership) EffectiveAtTime(now time.Time) bool {
	return m.Status == MembershipStatusActive &&
		!now.Before(m.EffectiveAt) &&
		(m.ExpiresAt == nil || now.Before(*m.ExpiresAt))
}

type MembershipContext struct {
	Team       Team
	Membership Membership
}

type PlatformRoleBinding struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	Role           PlatformRole
	Scope          PlatformScope
	EffectiveAt    time.Time
	ExpiresAt      *time.Time
	GrantReference string
	Status         BindingStatus
	Version        int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
