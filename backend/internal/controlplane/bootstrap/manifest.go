package bootstrap

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"rock/internal/controlplane/identity"
	"rock/internal/controlplane/team"
)

var (
	ErrInvalidManifest = errors.New("invalid bootstrap manifest")
	ErrSchemaNotEmpty  = errors.New("control plane identity schema is not empty")
)

type Manifest struct {
	User struct {
		ID          uuid.UUID `json:"id"`
		Email       string    `json:"email"`
		DisplayName string    `json:"display_name"`
	} `json:"user"`
	Team struct {
		ID   uuid.UUID `json:"id"`
		Slug string    `json:"slug"`
		Name string    `json:"name"`
	} `json:"team"`
	PlatformAdmin  bool   `json:"platform_admin"`
	GrantReference string `json:"grant_reference"`
}

type Seed struct {
	User                identity.User
	Team                team.Team
	Membership          team.Membership
	PlatformRoleBinding *team.PlatformRoleBinding
}

type Result struct {
	UserID                uuid.UUID  `json:"user_id"`
	TeamID                uuid.UUID  `json:"team_id"`
	MembershipID          uuid.UUID  `json:"membership_id"`
	PlatformRoleBindingID *uuid.UUID `json:"platform_role_binding_id,omitempty"`
}

type Store interface {
	ApplyBootstrap(context.Context, Seed) (Result, error)
}
