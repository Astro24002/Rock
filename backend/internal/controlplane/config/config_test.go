package config_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"rock/internal/controlplane/config"
)

func TestLoadRuntimeRequiresIndependentConfiguration(t *testing.T) {
	clearConfig(t)
	t.Setenv("ROCK_CONTROL_PLANE_DATABASE_DSN", "root:pass@tcp(localhost:3306)/rock_control_plane?parseTime=true")
	t.Setenv("ROCK_CONTROL_PLANE_JWT_ISSUER", "https://identity.example.com")
	t.Setenv("ROCK_CONTROL_PLANE_JWT_AUDIENCE", "rock-control-plane")
	t.Setenv("ROCK_CONTROL_PLANE_JWT_PUBLIC_KEY_FILE", writePublicKey(t))

	got, err := config.LoadRuntime()
	require.NoError(t, err)
	require.Equal(t, ":8090", got.HTTPAddress)
	require.Equal(t, 25, got.Database.MaxOpenConns)
	require.Equal(t, 5, got.Database.MaxIdleConns)
	require.Equal(t, "https://identity.example.com", got.JWTIssuer)
	require.NotNil(t, got.JWTPublicKey)
}

func TestLoadDatabaseDoesNotRequireIdentityConfiguration(t *testing.T) {
	clearConfig(t)
	t.Setenv("ROCK_CONTROL_PLANE_DATABASE_DSN", "dsn")
	t.Setenv("ROCK_CONTROL_PLANE_DATABASE_MAX_OPEN_CONNS", "40")
	t.Setenv("ROCK_CONTROL_PLANE_DATABASE_MAX_IDLE_CONNS", "8")

	got, err := config.LoadDatabase()
	require.NoError(t, err)
	require.Equal(t, "dsn", got.DSN)
	require.Equal(t, 40, got.MaxOpenConns)
	require.Equal(t, 8, got.MaxIdleConns)
}

func TestLoadRuntimeRejectsMissingRequiredValues(t *testing.T) {
	tests := []struct {
		name  string
		unset string
	}{
		{name: "database dsn", unset: "ROCK_CONTROL_PLANE_DATABASE_DSN"},
		{name: "issuer", unset: "ROCK_CONTROL_PLANE_JWT_ISSUER"},
		{name: "audience", unset: "ROCK_CONTROL_PLANE_JWT_AUDIENCE"},
		{name: "public key", unset: "ROCK_CONTROL_PLANE_JWT_PUBLIC_KEY_FILE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearConfig(t)
			t.Setenv("ROCK_CONTROL_PLANE_DATABASE_DSN", "dsn")
			t.Setenv("ROCK_CONTROL_PLANE_JWT_ISSUER", "https://identity.example.com")
			t.Setenv("ROCK_CONTROL_PLANE_JWT_AUDIENCE", "rock-control-plane")
			t.Setenv("ROCK_CONTROL_PLANE_JWT_PUBLIC_KEY_FILE", writePublicKey(t))
			t.Setenv(tt.unset, "")
			_, err := config.LoadRuntime()
			require.Error(t, err)
		})
	}
}

func TestLoadRuntimeRejectsRelativeOrMalformedPublicKey(t *testing.T) {
	clearConfig(t)
	setRequiredRuntime(t)
	t.Setenv("ROCK_CONTROL_PLANE_JWT_PUBLIC_KEY_FILE", "relative.pem")
	_, err := config.LoadRuntime()
	require.Error(t, err)

	badPath := filepath.Join(t.TempDir(), "bad.pem")
	require.NoError(t, os.WriteFile(badPath, []byte("not a PEM key"), 0o600))
	t.Setenv("ROCK_CONTROL_PLANE_JWT_PUBLIC_KEY_FILE", badPath)
	_, err = config.LoadRuntime()
	require.Error(t, err)
}

func TestLoadDatabaseRejectsInvalidPoolValues(t *testing.T) {
	clearConfig(t)
	t.Setenv("ROCK_CONTROL_PLANE_DATABASE_DSN", "dsn")
	t.Setenv("ROCK_CONTROL_PLANE_DATABASE_MAX_OPEN_CONNS", "0")
	_, err := config.LoadDatabase()
	require.Error(t, err)
}

func TestLoadRuntimeIgnoresLegacyAuthenticationEnvironment(t *testing.T) {
	clearConfig(t)
	t.Setenv("ROCK_JWT_SECRET", "legacy-secret")
	t.Setenv("ROCK_ADMIN_USERNAME", "legacy-admin")
	_, err := config.LoadRuntime()
	require.Error(t, err)
}

func setRequiredRuntime(t *testing.T) {
	t.Helper()
	t.Setenv("ROCK_CONTROL_PLANE_DATABASE_DSN", "dsn")
	t.Setenv("ROCK_CONTROL_PLANE_JWT_ISSUER", "https://identity.example.com")
	t.Setenv("ROCK_CONTROL_PLANE_JWT_AUDIENCE", "rock-control-plane")
	t.Setenv("ROCK_CONTROL_PLANE_JWT_PUBLIC_KEY_FILE", writePublicKey(t))
}

func clearConfig(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"ROCK_CONTROL_PLANE_DATABASE_DSN", "ROCK_CONTROL_PLANE_DATABASE_MAX_OPEN_CONNS",
		"ROCK_CONTROL_PLANE_DATABASE_MAX_IDLE_CONNS", "ROCK_CONTROL_PLANE_DATABASE_CONN_MAX_LIFETIME_SECONDS",
		"ROCK_CONTROL_PLANE_DATABASE_CONN_MAX_IDLE_TIME_SECONDS", "ROCK_CONTROL_PLANE_HTTP_ADDRESS",
		"ROCK_CONTROL_PLANE_JWT_ISSUER", "ROCK_CONTROL_PLANE_JWT_AUDIENCE", "ROCK_CONTROL_PLANE_JWT_PUBLIC_KEY_FILE",
	} {
		t.Setenv(key, "")
	}
}

func writePublicKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	encoded, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "public.pem")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: encoded}), 0o600))
	return path
}
