package team

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Clock interface {
	Now() time.Time
}

type Reader interface {
	ListEffectiveMemberships(context.Context, uuid.UUID, time.Time) ([]MembershipContext, error)
	FindEffectiveMembership(context.Context, uuid.UUID, uuid.UUID, time.Time) (MembershipContext, error)
}

type Service struct {
	reader Reader
	clock  Clock
}

func NewService(reader Reader, clock Clock) *Service {
	return &Service{reader: reader, clock: clock}
}

func (s *Service) ListEffectiveMemberships(ctx context.Context, userID uuid.UUID) ([]MembershipContext, error) {
	return s.reader.ListEffectiveMemberships(ctx, userID, s.clock.Now().UTC())
}

func (s *Service) ResolveEffectiveMembership(ctx context.Context, userID, teamID uuid.UUID) (MembershipContext, error) {
	return s.reader.FindEffectiveMembership(ctx, userID, teamID, s.clock.Now().UTC())
}
