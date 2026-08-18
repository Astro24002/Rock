.DEFAULT_GOAL := help

BACKEND_DIR := backend
GO := go

.PHONY: help controlplane-test controlplane-race controlplane-build controlplane-migrate controlplane-run clean

help:
	@printf '%s\n' 'Rock Control Plane commands:'
	@printf '%s\n' '  make controlplane-test   Run Phase 0 tests'
	@printf '%s\n' '  make controlplane-race   Run Phase 0 race tests'
	@printf '%s\n' '  make controlplane-build  Build both control-plane binaries'
	@printf '%s\n' '  make controlplane-migrate Apply the dedicated control-plane migration'
	@printf '%s\n' '  make controlplane-run     Run the standalone control-plane API'

controlplane-test:
	cd $(BACKEND_DIR) && $(GO) test ./internal/controlplane/... ./cmd/controlplane/... ./cmd/controlplane-bootstrap/...

controlplane-race:
	cd $(BACKEND_DIR) && $(GO) test -race ./internal/controlplane/...

controlplane-build:
	cd $(BACKEND_DIR) && $(GO) build ./cmd/controlplane ./cmd/controlplane-bootstrap

controlplane-migrate:
	@test -n "$$ROCK_CONTROL_PLANE_DATABASE_URL" || (printf '%s\n' 'ROCK_CONTROL_PLANE_DATABASE_URL is required' >&2; exit 1)
	migrate -path $(BACKEND_DIR)/controlplane/migrations -database "$$ROCK_CONTROL_PLANE_DATABASE_URL" up

controlplane-run:
	cd $(BACKEND_DIR) && $(GO) run ./cmd/controlplane

clean:
	rm -f $(BACKEND_DIR)/controlplane $(BACKEND_DIR)/controlplane-bootstrap
	rm -f $(BACKEND_DIR)/coverage.out
