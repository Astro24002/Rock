# Rock Control Plane Backend

The backend directory contains only the standalone control-plane Phase 0 service.

## Components

```text
cmd/controlplane/              HTTP API runtime
cmd/controlplane-bootstrap/    one-time empty-schema bootstrap
controlplane/migrations/       users, teams, memberships, platform roles
internal/controlplane/
  authn/                        RS256 identity-token verification
  bootstrap/                    manifest validation and seed construction
  config/                       independent ROCK_CONTROL_PLANE_* config
  httpapi/                      routes, middleware, handlers, envelopes
  identity/                     user domain and identity service
  persistence/                  control-plane repository and rows
  scope/                        explicit team request scope resolution
  team/                         team and membership domain
```

The backend has no legacy server entrypoint, legacy business models, legacy migrations, or dependency on the old application service.

See [controlplane/README.md](controlplane/README.md) for database setup, bootstrap, runtime configuration, and curl examples.

## Commands

```bash
go test ./internal/controlplane/... ./cmd/controlplane/... ./cmd/controlplane-bootstrap/...
go test -race ./internal/controlplane/...
go build ./cmd/controlplane ./cmd/controlplane-bootstrap
```
