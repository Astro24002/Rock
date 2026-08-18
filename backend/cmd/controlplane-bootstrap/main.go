package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"rock/internal/controlplane/bootstrap"
	"rock/internal/controlplane/config"
	"rock/internal/controlplane/persistence"
)

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "control-plane bootstrap failed:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("controlplane-bootstrap", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	manifestPath := flags.String("manifest", "", "absolute path to bootstrap manifest")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse arguments: %w", err)
	}
	if flags.NArg() != 0 || *manifestPath == "" || !filepath.IsAbs(*manifestPath) {
		return errors.New("-manifest must be an absolute path and the only argument")
	}

	manifest, err := readManifest(filepath.Clean(*manifestPath))
	if err != nil {
		return err
	}
	databaseConfig, err := config.LoadDatabase()
	if err != nil {
		return err
	}
	db, err := openDatabase(databaseConfig)
	if err != nil {
		return err
	}

	result, err := bootstrap.NewService(persistence.NewRepository(db), systemClock{}).Apply(ctx, manifest)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		return fmt.Errorf("write bootstrap result: %w", err)
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

func readManifest(path string) (bootstrap.Manifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return bootstrap.Manifest{}, fmt.Errorf("open bootstrap manifest: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var manifest bootstrap.Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return bootstrap.Manifest{}, fmt.Errorf("decode bootstrap manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return bootstrap.Manifest{}, errors.New("bootstrap manifest must contain one JSON document")
		}
		return bootstrap.Manifest{}, fmt.Errorf("decode trailing bootstrap manifest data: %w", err)
	}
	return manifest, nil
}
