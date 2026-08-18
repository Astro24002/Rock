package config

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Database struct {
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

type Runtime struct {
	Database     Database
	HTTPAddress  string
	Mode         string
	LogLevel     string
	JWTIssuer    string
	JWTAudience  string
	JWTPublicKey *rsa.PublicKey
}

func LoadDatabase() (Database, error) {
	dsn := strings.TrimSpace(os.Getenv("ROCK_CONTROL_PLANE_DATABASE_DSN"))
	if dsn == "" {
		return Database{}, errors.New("ROCK_CONTROL_PLANE_DATABASE_DSN is required")
	}
	maxOpen, err := positiveInt("ROCK_CONTROL_PLANE_DATABASE_MAX_OPEN_CONNS", 25)
	if err != nil {
		return Database{}, err
	}
	maxIdle, err := positiveInt("ROCK_CONTROL_PLANE_DATABASE_MAX_IDLE_CONNS", 5)
	if err != nil {
		return Database{}, err
	}
	if maxIdle > maxOpen {
		return Database{}, errors.New("database max idle connections cannot exceed max open connections")
	}
	maxLifetime, err := positiveInt("ROCK_CONTROL_PLANE_DATABASE_CONN_MAX_LIFETIME_SECONDS", 300)
	if err != nil {
		return Database{}, err
	}
	maxIdleTime, err := positiveInt("ROCK_CONTROL_PLANE_DATABASE_CONN_MAX_IDLE_TIME_SECONDS", 60)
	if err != nil {
		return Database{}, err
	}
	return Database{
		DSN: dsn, MaxOpenConns: maxOpen, MaxIdleConns: maxIdle,
		ConnMaxLifetime: time.Duration(maxLifetime) * time.Second,
		ConnMaxIdleTime: time.Duration(maxIdleTime) * time.Second,
	}, nil
}

func LoadRuntime() (Runtime, error) {
	database, err := LoadDatabase()
	if err != nil {
		return Runtime{}, err
	}
	issuer := strings.TrimSpace(os.Getenv("ROCK_CONTROL_PLANE_JWT_ISSUER"))
	audience := strings.TrimSpace(os.Getenv("ROCK_CONTROL_PLANE_JWT_AUDIENCE"))
	keyPath := strings.TrimSpace(os.Getenv("ROCK_CONTROL_PLANE_JWT_PUBLIC_KEY_FILE"))
	if issuer == "" || audience == "" || keyPath == "" {
		return Runtime{}, errors.New("control-plane JWT issuer, audience, and public key file are required")
	}
	if !filepath.IsAbs(keyPath) {
		return Runtime{}, errors.New("ROCK_CONTROL_PLANE_JWT_PUBLIC_KEY_FILE must be absolute")
	}
	publicKey, err := readRSAPublicKey(filepath.Clean(keyPath))
	if err != nil {
		return Runtime{}, err
	}

	address := strings.TrimSpace(os.Getenv("ROCK_CONTROL_PLANE_HTTP_ADDRESS"))
	if address == "" {
		address = ":8090"
	}
	mode := strings.TrimSpace(os.Getenv("ROCK_CONTROL_PLANE_MODE"))
	if mode == "" {
		mode = "release"
	}
	if mode != "release" && mode != "debug" {
		return Runtime{}, errors.New("ROCK_CONTROL_PLANE_MODE must be release or debug")
	}
	logLevel := strings.TrimSpace(os.Getenv("ROCK_CONTROL_PLANE_LOG_LEVEL"))
	if logLevel == "" {
		logLevel = "info"
	}
	switch logLevel {
	case "debug", "info", "warn", "error":
	default:
		return Runtime{}, errors.New("ROCK_CONTROL_PLANE_LOG_LEVEL is invalid")
	}

	return Runtime{
		Database: database, HTTPAddress: address, Mode: mode, LogLevel: logLevel,
		JWTIssuer: issuer, JWTAudience: audience, JWTPublicKey: publicKey,
	}, nil
}

func positiveInt(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return value, nil
}

func readRSAPublicKey(path string) (*rsa.PublicKey, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read JWT public key: %w", err)
	}
	block, rest := pem.Decode(contents)
	if block == nil || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, errors.New("JWT public key file must contain exactly one PEM block")
	}
	if parsed, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		key, ok := parsed.(*rsa.PublicKey)
		if !ok {
			return nil, errors.New("JWT public key must be RSA")
		}
		return key, nil
	}
	key, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err != nil {
		return nil, errors.New("JWT public key must be PKIX or PKCS#1 RSA")
	}
	return key, nil
}
