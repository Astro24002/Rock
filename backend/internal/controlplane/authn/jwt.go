package authn

import (
	"context"
	"crypto/rsa"
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Clock interface {
	Now() time.Time
}

type JWTConfig struct {
	Issuer    string
	Audience  string
	PublicKey *rsa.PublicKey
	Clock     Clock
}

type RS256Authenticator struct {
	issuer    string
	audience  string
	publicKey *rsa.PublicKey
	clock     Clock
}

func NewRS256Authenticator(config JWTConfig) (*RS256Authenticator, error) {
	issuer := strings.TrimSpace(config.Issuer)
	audience := strings.TrimSpace(config.Audience)
	if issuer == "" || audience == "" || config.PublicKey == nil || config.Clock == nil {
		return nil, errors.New("issuer, audience, public key, and clock are required")
	}
	if config.PublicKey.N == nil || config.PublicKey.N.BitLen() < 2048 {
		return nil, errors.New("RSA public key must be at least 2048 bits")
	}
	return &RS256Authenticator{issuer: issuer, audience: audience, publicKey: config.PublicKey, clock: config.Clock}, nil
}

type identityClaims struct {
	Email string `json:"email,omitempty"`
	jwt.RegisteredClaims
}

func (a *RS256Authenticator) Authenticate(ctx context.Context, encoded string) (Principal, error) {
	if err := ctx.Err(); err != nil {
		return Principal{}, err
	}
	if strings.TrimSpace(encoded) == "" {
		return Principal{}, ErrInvalidToken
	}

	claims := &identityClaims{}
	token, err := jwt.ParseWithClaims(
		encoded,
		claims,
		func(token *jwt.Token) (any, error) { return a.publicKey, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithIssuer(a.issuer),
		jwt.WithAudience(a.audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithTimeFunc(func() time.Time { return a.clock.Now().UTC() }),
	)
	if err != nil || token == nil || !token.Valid {
		return Principal{}, ErrInvalidToken
	}
	if claims.IssuedAt == nil || claims.NotBefore == nil || claims.ExpiresAt == nil {
		return Principal{}, ErrInvalidToken
	}
	subject, err := uuid.Parse(claims.Subject)
	if err != nil || subject == uuid.Nil {
		return Principal{}, ErrInvalidToken
	}
	now := a.clock.Now().UTC()
	if claims.IssuedAt.Time.After(now) || claims.NotBefore.Time.After(now) || !claims.ExpiresAt.Time.After(now) {
		return Principal{}, ErrInvalidToken
	}

	return Principal{
		Subject: subject, Email: strings.ToLower(strings.TrimSpace(claims.Email)),
		AuthenticatedAt: claims.IssuedAt.Time.UTC(),
	}, nil
}

var _ Authenticator = (*RS256Authenticator)(nil)
