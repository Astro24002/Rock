package bootstrap

import (
	"context"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"rock/internal/controlplane/identity"
	"rock/internal/controlplane/team"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

type Clock interface {
	Now() time.Time
}

type Service struct {
	store Store
	clock Clock
}

func NewService(store Store, clock Clock) *Service {
	return &Service{store: store, clock: clock}
}

func (s *Service) Apply(ctx context.Context, manifest Manifest) (Result, error) {
	seed, err := makeSeed(manifest, s.clock.Now().UTC())
	if err != nil {
		return Result{}, err
	}
	return s.store.ApplyBootstrap(ctx, seed)
}

func makeSeed(manifest Manifest, now time.Time) (Seed, error) {
	email := strings.ToLower(strings.TrimSpace(manifest.User.Email))
	slug := strings.ToLower(strings.TrimSpace(manifest.Team.Slug))
	displayName := strings.TrimSpace(manifest.User.DisplayName)
	teamName := strings.TrimSpace(manifest.Team.Name)
	grantReference := strings.TrimSpace(manifest.GrantReference)

	if manifest.User.ID == uuid.Nil || manifest.Team.ID == uuid.Nil {
		return Seed{}, fmt.Errorf("%w: user and team ids are required", ErrInvalidManifest)
	}
	parsedAddress, err := mail.ParseAddress(email)
	if err != nil || parsedAddress.Address != email {
		return Seed{}, fmt.Errorf("%w: email is invalid", ErrInvalidManifest)
	}
	if displayName == "" || teamName == "" {
		return Seed{}, fmt.Errorf("%w: display names are required", ErrInvalidManifest)
	}
	if !slugPattern.MatchString(slug) {
		return Seed{}, fmt.Errorf("%w: team slug is invalid", ErrInvalidManifest)
	}
	if grantReference == "" {
		return Seed{}, fmt.Errorf("%w: grant reference is required", ErrInvalidManifest)
	}

	seed := Seed{
		User: identity.User{
			ID: manifest.User.ID, Email: email, DisplayName: displayName, Status: identity.UserStatusActive,
			ProfileVersion: 1, CreatedAt: now, UpdatedAt: now,
		},
		Team: team.Team{
			ID: manifest.Team.ID, Slug: slug, Name: teamName, Status: team.TeamStatusActive,
			ConfigVersion: 1, CreatedAt: now, UpdatedAt: now,
		},
		Membership: team.Membership{
			ID: uuid.New(), TeamID: manifest.Team.ID, UserID: manifest.User.ID, Role: team.RoleAdmin,
			Status: team.MembershipStatusActive, EffectiveAt: now, Source: "bootstrap", Version: 1,
			CreatedAt: now, UpdatedAt: now,
		},
	}
	if manifest.PlatformAdmin {
		seed.PlatformRoleBinding = &team.PlatformRoleBinding{
			ID: uuid.New(), UserID: manifest.User.ID, Role: team.PlatformRoleAdmin,
			Scope: team.PlatformScopePlatform, EffectiveAt: now, GrantReference: grantReference,
			Status: team.BindingStatusActive, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
	}
	return seed, nil
}
