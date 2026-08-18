package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"rock/internal/controlplane/authn"
	"rock/internal/controlplane/config"
	"rock/internal/controlplane/httpapi"
	"rock/internal/controlplane/identity"
	"rock/internal/controlplane/persistence"
	"rock/internal/controlplane/scope"
	"rock/internal/controlplane/team"
)

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.LoadRuntime()
	if err != nil {
		return fmt.Errorf("load control-plane configuration: %w", err)
	}
	if cfg.Mode == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	db, err := openDatabase(cfg.Database)
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return errors.New("get control-plane database connection")
	}
	defer sqlDB.Close()

	clock := systemClock{}
	repository := persistence.NewRepository(db)
	authenticator, err := authn.NewRS256Authenticator(authn.JWTConfig{
		Issuer: cfg.JWTIssuer, Audience: cfg.JWTAudience, PublicKey: cfg.JWTPublicKey, Clock: clock,
	})
	if err != nil {
		return fmt.Errorf("configure identity authentication: %w", err)
	}
	router := httpapi.NewRouter(httpapi.Dependencies{
		Authenticator: authenticator,
		Identity:      identity.NewService(repository),
		Teams:         team.NewService(repository, clock),
		Scope:         scope.NewResolver(repository, clock),
		Readiness:     repository,
	})
	srv := &http.Server{
		Addr: cfg.HTTPAddress, Handler: router,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- srv.ListenAndServe() }()

	select {
	case <-ctx.Done():
	case err := <-serveErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve control-plane API: %w", err)
		}
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shut down control-plane API: %w", err)
	}
	return nil
}

func openDatabase(cfg config.Database) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(cfg.DSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Warn)})
	if err != nil {
		return nil, errors.New("open control-plane database")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, errors.New("get control-plane database connection")
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	return db, nil
}
