# Rock Control Plane

This repository now contains only the standalone Rock control-plane Phase 0 implementation and its architecture documents.

Phase 0 provides:

- UUID-based user identity
- Teams and time-bounded memberships
- Independent platform roles
- Explicit team request scope
- RS256 identity-token verification
- Read-only identity and team-context APIs
- One-time empty-schema bootstrap

The removed legacy application service, frontend, workflow service, artifact service, CI foundation, old migrations, and old database are intentionally not part of this repository state. The control plane does not import, start, or inspect them.

## Repository Layout

```text
backend/
  cmd/controlplane/              API runtime
  cmd/controlplane-bootstrap/    one-time governance bootstrap
  controlplane/migrations/       independent schema migrations
  internal/controlplane/         domain, auth, scope, persistence, HTTP
  controlplane/README.md         deployment and API workflow
docs/superpowers/specs/          architecture design
docs/superpowers/plans/          Phase 0 implementation plan
```

## Requirements

- Go 1.25+
- MariaDB 11+ or compatible MySQL
- `migrate` CLI for production schema migration
- An external identity provider issuing RS256 JWTs

## Verify

```bash
make controlplane-test
make controlplane-race
make controlplane-build
git diff --check
```

## Run

Configure the `ROCK_CONTROL_PLANE_*` variables documented in [backend/controlplane/README.md](backend/controlplane/README.md), migrate the dedicated database, bootstrap the first governance user/team, then run:

```bash
make controlplane-run
```

Architecture source: [Phase 0 identity and team scope design](docs/superpowers/specs/2026-08-17-control-plane-phase0-identity-team-scope-design.md).
