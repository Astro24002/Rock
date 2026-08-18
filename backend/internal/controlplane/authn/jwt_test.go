package authn_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"rock/internal/controlplane/authn"
	"rock/internal/controlplane/testkit"
)

var (
	tokenNow = time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	userID   = uuid.MustParse("11111111-1111-4111-8111-111111111111")
)

func TestJWTAuthenticatorAcceptsValidIdentityToken(t *testing.T) {
	privateKey := testRSAKey(t)
	authenticator := newAuthenticator(t, &privateKey.PublicKey)
	claims := validClaims()
	claims.Email = "user@example.com"
	token := signRS256(t, privateKey, claims)

	principal, err := authenticator.Authenticate(context.Background(), token)
	require.NoError(t, err)
	require.Equal(t, userID, principal.Subject)
	require.Equal(t, "user@example.com", principal.Email)
	require.Equal(t, tokenNow.Add(-time.Minute), principal.AuthenticatedAt)
}

func TestJWTAuthenticatorIgnoresTeamAndRoleClaims(t *testing.T) {
	privateKey := testRSAKey(t)
	authenticator := newAuthenticator(t, &privateKey.PublicKey)
	claims := jwt.MapClaims{
		"sub": userID.String(), "iss": "https://identity.example.com", "aud": []string{"rock-control-plane"},
		"iat": tokenNow.Add(-time.Minute).Unix(), "nbf": tokenNow.Add(-time.Minute).Unix(), "exp": tokenNow.Add(time.Hour).Unix(),
		"team_id": uuid.NewString(), "role": "platform_admin",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := token.SignedString(privateKey)
	require.NoError(t, err)

	principal, err := authenticator.Authenticate(context.Background(), signed)
	require.NoError(t, err)
	require.Equal(t, userID, principal.Subject)
}

func TestJWTAuthenticatorRejectsInvalidTokens(t *testing.T) {
	trustedKey := testRSAKey(t)
	otherKey := testRSAKey(t)
	tests := []struct {
		name  string
		token func(*testing.T) string
	}{
		{name: "wrong signature", token: func(t *testing.T) string { return signRS256(t, otherKey, validClaims()) }},
		{name: "wrong algorithm", token: func(t *testing.T) string {
			token := jwt.NewWithClaims(jwt.SigningMethodHS256, validClaims())
			signed, err := token.SignedString([]byte("not-rsa"))
			require.NoError(t, err)
			return signed
		}},
		{name: "wrong issuer", token: func(t *testing.T) string {
			claims := validClaims()
			claims.Issuer = "https://wrong.example.com"
			return signRS256(t, trustedKey, claims)
		}},
		{name: "wrong audience", token: func(t *testing.T) string {
			claims := validClaims()
			claims.Audience = jwt.ClaimStrings{"other-service"}
			return signRS256(t, trustedKey, claims)
		}},
		{name: "non uuid subject", token: func(t *testing.T) string {
			claims := validClaims()
			claims.Subject = "user-name"
			return signRS256(t, trustedKey, claims)
		}},
		{name: "missing expiry", token: func(t *testing.T) string {
			claims := validClaims()
			claims.ExpiresAt = nil
			return signRS256(t, trustedKey, claims)
		}},
		{name: "missing not before", token: func(t *testing.T) string {
			claims := validClaims()
			claims.NotBefore = nil
			return signRS256(t, trustedKey, claims)
		}},
		{name: "missing issued at", token: func(t *testing.T) string {
			claims := validClaims()
			claims.IssuedAt = nil
			return signRS256(t, trustedKey, claims)
		}},
		{name: "expired", token: func(t *testing.T) string {
			claims := validClaims()
			claims.ExpiresAt = jwt.NewNumericDate(tokenNow)
			return signRS256(t, trustedKey, claims)
		}},
		{name: "future not before", token: func(t *testing.T) string {
			claims := validClaims()
			claims.NotBefore = jwt.NewNumericDate(tokenNow.Add(time.Minute))
			return signRS256(t, trustedKey, claims)
		}},
		{name: "future issued at", token: func(t *testing.T) string {
			claims := validClaims()
			claims.IssuedAt = jwt.NewNumericDate(tokenNow.Add(time.Minute))
			return signRS256(t, trustedKey, claims)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authenticator := newAuthenticator(t, &trustedKey.PublicKey)
			_, err := authenticator.Authenticate(context.Background(), tt.token(t))
			require.ErrorIs(t, err, authn.ErrInvalidToken)
		})
	}
}

func TestNewRS256AuthenticatorRejectsIncompleteConfig(t *testing.T) {
	key := testRSAKey(t)
	_, err := authn.NewRS256Authenticator(authn.JWTConfig{PublicKey: &key.PublicKey, Clock: testkit.FixedClock{Time: tokenNow}})
	require.Error(t, err)
}

type identityClaims struct {
	Email string `json:"email,omitempty"`
	jwt.RegisteredClaims
}

func validClaims() identityClaims {
	return identityClaims{RegisteredClaims: jwt.RegisteredClaims{
		Subject: userID.String(), Issuer: "https://identity.example.com",
		Audience:  jwt.ClaimStrings{"rock-control-plane"},
		IssuedAt:  jwt.NewNumericDate(tokenNow.Add(-time.Minute)),
		NotBefore: jwt.NewNumericDate(tokenNow.Add(-time.Minute)),
		ExpiresAt: jwt.NewNumericDate(tokenNow.Add(time.Hour)),
	}}
}

func newAuthenticator(t *testing.T, publicKey *rsa.PublicKey) authn.Authenticator {
	t.Helper()
	authenticator, err := authn.NewRS256Authenticator(authn.JWTConfig{
		Issuer: "https://identity.example.com", Audience: "rock-control-plane",
		PublicKey: publicKey, Clock: testkit.FixedClock{Time: tokenNow},
	})
	require.NoError(t, err)
	return authenticator
}

func signRS256(t *testing.T, privateKey *rsa.PrivateKey, claims jwt.Claims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := token.SignedString(privateKey)
	require.NoError(t, err)
	return signed
}

func testRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return key
}
