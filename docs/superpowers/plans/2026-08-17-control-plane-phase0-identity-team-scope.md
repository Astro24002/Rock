# Control Plane Phase 0 Identity And Team Scope Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a standalone Rock control-plane process that establishes UUID identity, team membership, independent platform roles, explicit team request scope, and three authenticated query endpoints without importing or calling the legacy service.

**Architecture:** Add a modular control-plane monolith under `backend/internal/controlplane`, with separate domain, persistence, authentication, scope, and HTTP packages. It owns a new MariaDB/MySQL schema and two binaries: the API runtime and an empty-schema-only bootstrap command. JWT proves only the UUID subject; every team request resolves current membership from the new database.

**Tech Stack:** Go 1.25, Gin, GORM, MySQL/MariaDB, SQLite for repository tests, `golang-jwt/jwt/v5`, `google/uuid`, `testify`.

---

## File Map

Create these files with one responsibility each:

```text
backend/cmd/controlplane/main.go
backend/cmd/controlplane-bootstrap/main.go
backend/controlplane/migrations/000001_identity_team.up.sql
backend/controlplane/migrations/000001_identity_team.down.sql
backend/controlplane/README.md
backend/internal/controlplane/config/config.go
backend/internal/controlplane/config/config_test.go
backend/internal/controlplane/authn/authenticator.go
backend/internal/controlplane/authn/jwt.go
backend/internal/controlplane/authn/jwt_test.go
backend/internal/controlplane/identity/user.go
backend/internal/controlplane/identity/service.go
backend/internal/controlplane/identity/service_test.go
backend/internal/controlplane/team/model.go
backend/internal/controlplane/team/service.go
backend/internal/controlplane/team/service_test.go
backend/internal/controlplane/scope/scope.go
backend/internal/controlplane/scope/resolver.go
backend/internal/controlplane/scope/resolver_test.go
backend/internal/controlplane/persistence/models.go
backend/internal/controlplane/persistence/repository.go
backend/internal/controlplane/persistence/repository_test.go
backend/internal/controlplane/bootstrap/manifest.go
backend/internal/controlplane/bootstrap/service.go
backend/internal/controlplane/bootstrap/service_test.go
backend/internal/controlplane/httpapi/envelope.go
backend/internal/controlplane/httpapi/middleware.go
backend/internal/controlplane/httpapi/handlers.go
backend/internal/controlplane/httpapi/router.go
backend/internal/controlplane/httpapi/router_test.go
backend/internal/controlplane/testkit/clock.go
backend/internal/controlplane/testkit/authenticator.go
```

Modify:

```text
backend/go.mod
backend/go.sum
Makefile
```

The new implementation must not import:

```text
rock/internal/service
rock/internal/repository
rock/internal/middleware
rock/internal/model
rock/pkg/response
rock/cmd/server
```

### Task 1: Domain Types And Validity Rules

**Files:**
- Create: `backend/internal/controlplane/identity/user.go`
- Create: `backend/internal/controlplane/team/model.go`
- Create: `backend/internal/controlplane/team/service_test.go`
- Create: `backend/internal/controlplane/testkit/clock.go`

- [ ] **Step 1: Write failing domain tests**

Create table-driven tests covering active users, fixed roles, and membership time boundaries:

```go
func TestMembershipEffectiveAt(t *testing.T) {
    now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
    active := team.Membership{Status: team.MembershipActive, EffectiveAt: now.Add(-time.Hour)}
    require.True(t, active.EffectiveAtTime(now))

    future := active
    future.EffectiveAt = now.Add(time.Minute)
    require.False(t, future.EffectiveAtTime(now))

    expired := active
    expiresAt := now
    expired.ExpiresAt = &expiresAt
    require.False(t, expired.EffectiveAtTime(now))
}

func TestFixedRoles(t *testing.T) {
    require.True(t, team.RoleViewer.Valid())
    require.True(t, team.RoleDeveloper.Valid())
    require.True(t, team.RoleAdmin.Valid())
    require.False(t, team.Role("owner").Valid())
    require.True(t, team.PlatformAdmin.Valid())
    require.False(t, team.PlatformRole("admin").Valid())
}
```

- [ ] **Step 2: Run the tests and verify RED**

Run: `cd backend && go test ./internal/controlplane/team -run 'TestMembershipEffectiveAt|TestFixedRoles' -v`

Expected: FAIL because the `identity` and `team` packages do not exist.

- [ ] **Step 3: Implement minimal domain types**

Implement `identity.User` with UUID, normalized email, display name, `active/suspended/disabled`, profile version, and UTC timestamps. Implement `team.Team`, `team.Membership`, `team.MembershipContext`, and `team.PlatformRoleBinding` with these fixed enums:

```go
const (
    RoleViewer    Role = "viewer"
    RoleDeveloper Role = "developer"
    RoleAdmin     Role = "admin"

    PlatformAdmin PlatformRole = "platform_admin"
    AssetOperator PlatformRole = "asset_operator"
    Auditor       PlatformRole = "auditor"
)

func (m Membership) EffectiveAtTime(now time.Time) bool {
    if m.Status != MembershipActive || now.Before(m.EffectiveAt) {
        return false
    }
    return m.ExpiresAt == nil || now.Before(*m.ExpiresAt)
}
```

Define `testkit.FixedClock`:

```go
type FixedClock struct{ Time time.Time }
func (c FixedClock) Now() time.Time { return c.Time }
```

- [ ] **Step 4: Run domain tests and verify GREEN**

Run: `cd backend && go test ./internal/controlplane/team ./internal/controlplane/identity -v`

Expected: PASS.

- [ ] **Step 5: Commit the domain types**

```bash
git add backend/internal/controlplane/identity backend/internal/controlplane/team backend/internal/controlplane/testkit/clock.go
git commit -m "feat: add control plane identity and team domain"
```

### Task 2: Independent Schema And Read Repository

**Files:**
- Create: `backend/controlplane/migrations/000001_identity_team.up.sql`
- Create: `backend/controlplane/migrations/000001_identity_team.down.sql`
- Create: `backend/internal/controlplane/persistence/models.go`
- Create: `backend/internal/controlplane/persistence/repository.go`
- Create: `backend/internal/controlplane/persistence/repository_test.go`
- Modify: `backend/go.mod`
- Modify: `backend/go.sum`

- [ ] **Step 1: Write failing repository tests**

Use a fresh named in-memory SQLite database per test, enable foreign keys, migrate only the new persistence rows, and assert:

```go
func TestRepositoryListsOnlyEffectiveTeamsInSlugOrder(t *testing.T) {
    db := newTestDB(t)
    repo := persistence.NewRepository(db)
    now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
    user := seedUser(t, db, identity.UserActive)
    zeta := seedTeam(t, db, "zeta", team.TeamActive)
    alpha := seedTeam(t, db, "alpha", team.TeamActive)
    suspended := seedTeam(t, db, "hidden", team.TeamSuspended)
    seedMembership(t, db, user.ID, zeta.ID, team.RoleDeveloper, team.MembershipActive, now.Add(-time.Hour), nil)
    seedMembership(t, db, user.ID, alpha.ID, team.RoleViewer, team.MembershipActive, now.Add(-time.Hour), nil)
    seedMembership(t, db, user.ID, suspended.ID, team.RoleAdmin, team.MembershipActive, now.Add(-time.Hour), nil)

    got, err := repo.ListEffectiveMemberships(context.Background(), user.ID, now)
    require.NoError(t, err)
    require.Equal(t, []string{"alpha", "zeta"}, []string{got[0].Team.Slug, got[1].Team.Slug})
}

func TestRepositoryDoesNotSynthesizeTeamAccessFromPlatformRole(t *testing.T) {
    db := newTestDB(t)
    repo := persistence.NewRepository(db)
    user := seedUser(t, db, identity.UserActive)
    target := seedTeam(t, db, "target", team.TeamActive)
    seedPlatformRole(t, db, user.ID, team.PlatformAdmin)

    _, err := repo.FindEffectiveMembership(context.Background(), user.ID, target.ID, time.Now().UTC())
    require.ErrorIs(t, err, team.ErrAccessDenied)
}
```

Also test duplicate email, duplicate slug, foreign keys, and a second active membership for the same user/team.

- [ ] **Step 2: Run repository tests and verify RED**

Run: `cd backend && go test ./internal/controlplane/persistence -v`

Expected: FAIL because the persistence package does not exist.

- [ ] **Step 3: Add persistence rows and mapping**

Define unexported GORM rows with `TableName()` methods for exactly `users`, `teams`, `team_memberships`, and `platform_role_bindings`. UUID columns use `char(36)` and times use UTC. Export only these test/bootstrap helpers from persistence:

```go
func AutoMigrateForTest(db *gorm.DB) error
func NewRepository(db *gorm.DB) *Repository
```

`Repository` implements:

```go
FindUser(ctx context.Context, id uuid.UUID) (identity.User, error)
ListEffectiveMemberships(ctx context.Context, userID uuid.UUID, now time.Time) ([]team.MembershipContext, error)
FindEffectiveMembership(ctx context.Context, userID, teamID uuid.UUID, now time.Time) (team.MembershipContext, error)
Ping(ctx context.Context) error
```

Every effective membership query must join `users` and `teams`, require all three statuses to be active, require `effective_at <= now`, and require `expires_at IS NULL OR expires_at > now`.

For SQLite tests, `AutoMigrateForTest` must add a partial unique index on `(user_id, team_id) WHERE status = 'active'`. Production MySQL enforces the same invariant with the generated key below.

- [ ] **Step 4: Add production migration SQL**

Create `000001_identity_team.up.sql` with four new tables, foreign keys, status/role CHECK constraints, UTC-capable timestamp columns, and these keys:

```sql
UNIQUE KEY uk_users_email (email),
UNIQUE KEY uk_teams_slug (slug),
KEY idx_memberships_user_status (user_id, status, effective_at, expires_at),
KEY idx_memberships_team_status (team_id, status),
active_membership_key VARCHAR(73)
  GENERATED ALWAYS AS (
    CASE WHEN status = 'active' THEN CONCAT(user_id, ':', team_id) ELSE NULL END
  ) STORED,
UNIQUE KEY uk_memberships_one_active (active_membership_key)
```

Create the down migration in reverse foreign-key order. Neither migration may reference any legacy table.

- [ ] **Step 5: Run repository tests and verify GREEN**

Run: `cd backend && go test ./internal/controlplane/persistence -v`

Expected: PASS.

- [ ] **Step 6: Normalize direct dependencies**

Run: `cd backend && go mod tidy`

Expected: `github.com/google/uuid` is a direct dependency and no unrelated dependency is added.

- [ ] **Step 7: Verify migration isolation**

Run:

```bash
rg -n "applications|pipelines|environments|k8s_clusters|artifact" backend/controlplane/migrations backend/internal/controlplane/persistence
```

Expected: no output.

- [ ] **Step 8: Commit persistence**

```bash
git add backend/controlplane/migrations backend/internal/controlplane/persistence backend/go.mod backend/go.sum
git commit -m "feat: add independent control plane identity schema"
```

### Task 3: Empty-Schema Bootstrap Transaction

**Files:**
- Create: `backend/internal/controlplane/bootstrap/manifest.go`
- Create: `backend/internal/controlplane/bootstrap/service.go`
- Create: `backend/internal/controlplane/bootstrap/service_test.go`
- Create: `backend/cmd/controlplane-bootstrap/main.go`
- Modify: `backend/internal/controlplane/persistence/repository.go`

- [ ] **Step 1: Write failing bootstrap tests**

Test one successful transaction, refusal on any non-empty identity table, invalid role/email/time input, and full rollback on a duplicate constraint:

```go
func TestServiceBootstrapsEmptySchemaOnce(t *testing.T) {
    db := newBootstrapDB(t)
    repo := persistence.NewRepository(db)
    svc := bootstrap.NewService(repo, testkit.FixedClock{Time: fixedNow})
    result, err := svc.Apply(context.Background(), validManifest())
    require.NoError(t, err)
    require.Equal(t, validManifest().User.ID, result.UserID)
    require.Equal(t, int64(1), tableCount(t, db, "users"))
    require.Equal(t, int64(1), tableCount(t, db, "teams"))
    require.Equal(t, int64(1), tableCount(t, db, "team_memberships"))

    _, err = svc.Apply(context.Background(), validManifest())
    require.ErrorIs(t, err, bootstrap.ErrSchemaNotEmpty)
}
```

- [ ] **Step 2: Run bootstrap tests and verify RED**

Run: `cd backend && go test ./internal/controlplane/bootstrap -v`

Expected: FAIL because bootstrap types do not exist.

- [ ] **Step 3: Implement manifest validation and transaction**

Use this input shape:

```go
type Manifest struct {
    User struct {
        ID          uuid.UUID `json:"id"`
        Email       string    `json:"email"`
        DisplayName string    `json:"display_name"`
    } `json:"user"`
    Team struct {
        ID   uuid.UUID `json:"id"`
        Slug string    `json:"slug"`
        Name string    `json:"name"`
    } `json:"team"`
    PlatformAdmin bool   `json:"platform_admin"`
    GrantReference string `json:"grant_reference"`
}
```

Normalize email and slug before validation. Define a narrow bootstrap-owned store contract:

```go
type Store interface {
    ApplyBootstrap(context.Context, Seed) (Result, error)
}
```

`bootstrap.Service` validates the manifest, constructs a complete `Seed`, and calls the store. Add `ApplyBootstrap` to `persistence.Repository`; in one GORM transaction it lock/checks all four tables are empty, inserts active User, active Team, active admin Membership, and optionally active `platform_admin` Binding. Use the injected clock in the service to populate all timestamps before the store call. Return only created UUIDs.

- [ ] **Step 4: Run bootstrap tests and verify GREEN**

Run: `cd backend && go test ./internal/controlplane/bootstrap -v`

Expected: PASS.

- [ ] **Step 5: Add the bootstrap CLI**

The command accepts only `-manifest /absolute/path/bootstrap.json`. At this task it reads only `ROCK_CONTROL_PLANE_DATABASE_DSN` directly, opens the new database, parses one JSON document with `DisallowUnknownFields`, applies the transaction, and prints a JSON result without credentials. A missing flag, relative path, invalid JSON, non-empty schema, or database error exits nonzero. Task 7 replaces the direct environment read with `config.LoadDatabase`.

- [ ] **Step 6: Build and commit bootstrap**

Run: `cd backend && go build ./cmd/controlplane-bootstrap`

Expected: PASS.

```bash
git add backend/internal/controlplane/bootstrap backend/cmd/controlplane-bootstrap
git commit -m "feat: add one-time control plane bootstrap"
```

### Task 4: Strict RS256 JWT Authentication

**Files:**
- Create: `backend/internal/controlplane/authn/authenticator.go`
- Create: `backend/internal/controlplane/authn/jwt.go`
- Create: `backend/internal/controlplane/authn/jwt_test.go`

- [ ] **Step 1: Write failing JWT tests**

Generate an RSA key in the test and issue tokens. Cover valid claims plus wrong algorithm, signature, issuer, audience, non-UUID subject, missing `exp`, missing `nbf`, missing `iat`, expired token, and future token:

```go
func TestJWTAuthenticatorAcceptsValidIdentityToken(t *testing.T) {
    now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
    privateKey := testRSAKey(t)
    auth, err := authn.NewRS256Authenticator(authn.JWTConfig{
        Issuer: "https://identity.example.com",
        Audience: "rock-control-plane",
        PublicKey: &privateKey.PublicKey,
        Clock: testkit.FixedClock{Time: now},
    })
    require.NoError(t, err)

    token := signToken(t, privateKey, jwt.RegisteredClaims{
        Subject: userID.String(), Issuer: "https://identity.example.com",
        Audience: jwt.ClaimStrings{"rock-control-plane"},
        IssuedAt: jwt.NewNumericDate(now.Add(-time.Minute)),
        NotBefore: jwt.NewNumericDate(now.Add(-time.Minute)),
        ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
    })
    principal, err := auth.Authenticate(context.Background(), token)
    require.NoError(t, err)
    require.Equal(t, userID, principal.Subject)
}
```

- [ ] **Step 2: Run JWT tests and verify RED**

Run: `cd backend && go test ./internal/controlplane/authn -v`

Expected: FAIL because `NewRS256Authenticator` does not exist.

- [ ] **Step 3: Implement strict authentication**

Define:

```go
type Principal struct {
    Subject         uuid.UUID
    Email           string
    AuthenticatedAt time.Time
}

type Authenticator interface {
    Authenticate(context.Context, string) (Principal, error)
}
```

Use `jwt.ParseWithClaims` with `jwt.WithValidMethods([]string{"RS256"})`, `jwt.WithIssuer`, `jwt.WithAudience`, and `jwt.WithTimeFunc(clock.Now)`. After library validation, explicitly require non-nil `IssuedAt`, `NotBefore`, and `ExpiresAt`, parse `Subject` as UUID, and ignore all role/team claims. Return a single sentinel `ErrInvalidToken` for every token validation failure.

- [ ] **Step 4: Run JWT tests and verify GREEN**

Run: `cd backend && go test ./internal/controlplane/authn -v`

Expected: PASS.

- [ ] **Step 5: Commit authentication**

```bash
git add backend/internal/controlplane/authn
git commit -m "feat: verify control plane identity tokens"
```

### Task 5: Identity Queries And Request Scope Resolution

**Files:**
- Create: `backend/internal/controlplane/identity/service.go`
- Create: `backend/internal/controlplane/identity/service_test.go`
- Create: `backend/internal/controlplane/team/service.go`
- Update: `backend/internal/controlplane/team/service_test.go`
- Create: `backend/internal/controlplane/scope/scope.go`
- Create: `backend/internal/controlplane/scope/resolver.go`
- Create: `backend/internal/controlplane/scope/resolver_test.go`

- [ ] **Step 1: Write failing service and scope tests**

Use fakes, not GORM. Cover unknown/inactive User, effective membership list, active team resolution, path/header mismatch, and isolation of platform roles:

```go
func TestResolverBuildsOneTeamScope(t *testing.T) {
    resolver := scope.NewResolver(fakeTeamReader{context: membershipContext}, fixedClock)
    got, err := resolver.ResolveTeam(context.Background(), scope.TeamRequest{
        RequestID: "req-1", TraceID: "trace-1", ActorUserID: userID,
        HeaderTeamID: teamID.String(), PathTeamID: teamID.String(),
        AuthenticatedAt: fixedNow.Add(-time.Minute),
    })
    require.NoError(t, err)
    require.Equal(t, scope.ScopeTeam, got.ScopeType)
    require.Equal(t, teamID, *got.ActiveTeamID)
    require.Equal(t, team.RoleDeveloper, *got.MembershipRole)
}

func TestResolverRejectsPathHeaderMismatchBeforeLookup(t *testing.T) {
    reader := &countingTeamReader{}
    resolver := scope.NewResolver(reader, fixedClock)
    _, err := resolver.ResolveTeam(context.Background(), scope.TeamRequest{
        ActorUserID: userID, HeaderTeamID: teamA.String(), PathTeamID: teamB.String(),
    })
    require.ErrorIs(t, err, scope.ErrInvalidTeamContext)
    require.Zero(t, reader.calls)
}
```

- [ ] **Step 2: Run tests and verify RED**

Run: `cd backend && go test ./internal/controlplane/identity ./internal/controlplane/team ./internal/controlplane/scope -v`

Expected: FAIL because services and resolver do not exist.

- [ ] **Step 3: Implement query services**

`identity.Service.ResolveActiveUser` loads by UUID and maps missing to `ErrNotRegistered` and non-active to `ErrInactive`. `team.Service.ListEffectiveMemberships` and `ResolveEffectiveMembership` delegate to a narrow `Reader` interface and never inspect platform role bindings.

- [ ] **Step 4: Implement immutable scope values**

Define `ScopeType` values `team` and `platform`, `RequestScope`, and `TeamRequest`. `ResolveTeam` must:

1. require exactly one canonical UUID header with no comma or surrounding whitespace;
2. require path team equality when present;
3. query current membership using the injected clock;
4. populate all team fields together;
5. map missing/inactive teams and memberships to `ErrTeamAccessDenied`.

- [ ] **Step 5: Run tests and verify GREEN**

Run: `cd backend && go test ./internal/controlplane/identity ./internal/controlplane/team ./internal/controlplane/scope -v`

Expected: PASS.

- [ ] **Step 6: Commit services and scope**

```bash
git add backend/internal/controlplane/identity backend/internal/controlplane/team backend/internal/controlplane/scope
git commit -m "feat: resolve control plane request scope"
```

### Task 6: Standalone HTTP API

**Files:**
- Create: `backend/internal/controlplane/httpapi/envelope.go`
- Create: `backend/internal/controlplane/httpapi/middleware.go`
- Create: `backend/internal/controlplane/httpapi/handlers.go`
- Create: `backend/internal/controlplane/httpapi/router.go`
- Create: `backend/internal/controlplane/httpapi/router_test.go`
- Create: `backend/internal/controlplane/testkit/authenticator.go`

- [ ] **Step 1: Write failing route tests**

Build the router with fake authenticator/readers and cover all specified errors and success responses. At minimum:

```go
func TestTeamContextRequiresExplicitMatchingTeam(t *testing.T) {
    router := newTestRouter(t)

    missing := request(router, http.MethodGet, "/v1/teams/"+teamID.String()+"/context", validBearer, nil)
    require.Equal(t, http.StatusBadRequest, missing.Code)
    requireJSONError(t, missing, "TEAM_CONTEXT_REQUIRED")

    mismatch := request(router, http.MethodGet, "/v1/teams/"+teamID.String()+"/context", validBearer,
        map[string]string{"X-Active-Team-Id": otherTeamID.String()})
    require.Equal(t, http.StatusBadRequest, mismatch.Code)
    requireJSONError(t, mismatch, "INVALID_TEAM_CONTEXT")

    ok := request(router, http.MethodGet, "/v1/teams/"+teamID.String()+"/context", validBearer,
        map[string]string{"X-Active-Team-Id": teamID.String()})
    require.Equal(t, http.StatusOK, ok.Code)
}

func TestPlatformRoleDoesNotAuthorizeTeamContext(t *testing.T) {
    router := newTestRouterWithPlatformAdminOnly(t)
    response := request(router, http.MethodGet, "/v1/teams/"+teamID.String()+"/context", validBearer,
        map[string]string{"X-Active-Team-Id": teamID.String()})
    require.Equal(t, http.StatusForbidden, response.Code)
    requireJSONError(t, response, "TEAM_ACCESS_DENIED")
}
```

Also test `/healthz`, `/readyz`, `/v1/me`, `/v1/me/teams`, invalid bearer forms, inactive users, duplicate team headers, stable team ordering, request ID propagation, and error redaction.

- [ ] **Step 2: Run route tests and verify RED**

Run: `cd backend && go test ./internal/controlplane/httpapi -v`

Expected: FAIL because router and middleware do not exist.

- [ ] **Step 3: Implement envelopes and middleware**

Create independent success/error envelopes exactly matching the spec. Middleware order:

```text
RequestID -> Recovery -> Authentication -> optional TeamScope -> Handler
```

Authentication accepts exactly one `Authorization: Bearer <token>` header, calls `Authenticator`, resolves an active User, and stores a typed session in Gin context. TeamScope reads all `X-Active-Team-Id` header values, rejects zero/multiple/comma values, resolves `RequestScope`, and stores it under a private context key. RequestID uses a supplied `X-Request-ID` or generates a UUID; TraceID uses a supplied `X-Trace-ID` or defaults to RequestID.

- [ ] **Step 4: Implement handlers and routes**

Return DTOs, not persistence rows. Route groups:

```go
r.GET("/healthz", handlers.Health)
r.GET("/readyz", handlers.Ready)
authenticated := r.Group("/v1", authenticationMiddleware)
authenticated.GET("/me", handlers.Me)
authenticated.GET("/me/teams", handlers.MyTeams)
teamScoped := authenticated.Group("/teams/:team_id", teamScopeMiddleware)
teamScoped.GET("/context", handlers.TeamContext)
```

- [ ] **Step 5: Run route tests and verify GREEN**

Run: `cd backend && go test ./internal/controlplane/httpapi -v`

Expected: PASS with no log output containing bearer tokens.

- [ ] **Step 6: Commit HTTP API**

```bash
git add backend/internal/controlplane/httpapi backend/internal/controlplane/testkit/authenticator.go
git commit -m "feat: expose scoped control plane queries"
```

### Task 7: Runtime Configuration And Independent Binaries

**Files:**
- Create: `backend/internal/controlplane/config/config.go`
- Create: `backend/internal/controlplane/config/config_test.go`
- Create: `backend/cmd/controlplane/main.go`
- Update: `backend/cmd/controlplane-bootstrap/main.go`

- [ ] **Step 1: Write failing config tests**

Use `t.Setenv` and test complete valid configuration, missing DSN, missing issuer/audience/public key file, malformed RSA public key, and defaults:

```go
func TestLoadRuntimeRequiresIndependentConfiguration(t *testing.T) {
    t.Setenv("ROCK_CONTROL_PLANE_DATABASE_DSN", "root:pass@tcp(localhost:3306)/rock_control_plane?parseTime=true")
    t.Setenv("ROCK_CONTROL_PLANE_JWT_ISSUER", "https://identity.example.com")
    t.Setenv("ROCK_CONTROL_PLANE_JWT_AUDIENCE", "rock-control-plane")
    t.Setenv("ROCK_CONTROL_PLANE_JWT_PUBLIC_KEY_FILE", writePublicKey(t))
    got, err := config.LoadRuntime()
    require.NoError(t, err)
    require.Equal(t, ":8090", got.HTTPAddress)
}
```

- [ ] **Step 2: Run config tests and verify RED**

Run: `cd backend && go test ./internal/controlplane/config -v`

Expected: FAIL because config package does not exist.

- [ ] **Step 3: Implement strict config loading**

Use only `ROCK_CONTROL_PLANE_*` environment variables. `LoadDatabase` requires only the DSN and returns connection-pool defaults; `LoadRuntime` additionally requires JWT issuer, audience, and absolute public-key file. Defaults may exist only for HTTP address and connection-pool numbers. Parse only PKIX or PKCS#1 RSA public keys. Never read `config.yaml`, `.env`, `ROCK_JWT_SECRET`, or `ROCK_ADMIN_*`.

- [ ] **Step 4: Run config tests and verify GREEN**

Run: `cd backend && go test ./internal/controlplane/config -v`

Expected: PASS.

- [ ] **Step 5: Wire the API process**

`cmd/controlplane/main.go` must load runtime config, open MySQL through a new local function, configure the pool, create persistence repository/services/authenticator/router, serve with timeouts, and shut down on SIGINT/SIGTERM. It must not call any legacy constructor or global variable.

Update `cmd/controlplane-bootstrap/main.go` to call `config.LoadDatabase` so both binaries share only the new database configuration contract.

Use explicit server timeouts:

```go
srv := &http.Server{
    Addr:              cfg.HTTPAddress,
    Handler:           router,
    ReadHeaderTimeout: 5 * time.Second,
    ReadTimeout:       15 * time.Second,
    WriteTimeout:      30 * time.Second,
    IdleTimeout:       60 * time.Second,
}
```

- [ ] **Step 6: Build both binaries**

Run: `cd backend && go build ./cmd/controlplane ./cmd/controlplane-bootstrap`

Expected: PASS.

- [ ] **Step 7: Commit runtime wiring**

```bash
git add backend/internal/controlplane/config backend/cmd/controlplane backend/cmd/controlplane-bootstrap
git commit -m "feat: wire standalone control plane runtime"
```

### Task 8: Developer Commands, Documentation, And Isolation Verification

**Files:**
- Create: `backend/controlplane/README.md`
- Modify: `Makefile`

- [ ] **Step 1: Add independent Make targets**

Add:

```make
controlplane-test:
	cd backend && go test ./internal/controlplane/... ./cmd/controlplane/... ./cmd/controlplane-bootstrap/...

controlplane-build:
	cd backend && go build ./cmd/controlplane ./cmd/controlplane-bootstrap

controlplane-migrate:
	migrate -path backend/controlplane/migrations -database "$$ROCK_CONTROL_PLANE_DATABASE_URL" up

controlplane-run:
	cd backend && go run ./cmd/controlplane
```

Require `ROCK_CONTROL_PLANE_DATABASE_URL` for migration and do not fall back to the legacy `DATABASE_URL`.

- [ ] **Step 2: Document the standalone workflow**

Document:

1. creation of a dedicated `rock_control_plane` database;
2. all required `ROCK_CONTROL_PLANE_*` variables;
3. migration command;
4. exact bootstrap manifest shape and one-time behavior;
5. runtime command;
6. curl examples for `/v1/me`, `/v1/me/teams`, and team context;
7. explicit statement that no legacy service/table is required.

- [ ] **Step 3: Run focused verification**

Run:

```bash
cd backend && go test -race ./internal/controlplane/...
cd backend && go build ./cmd/controlplane ./cmd/controlplane-bootstrap
git diff --check
```

Expected: all commands PASS.

- [ ] **Step 4: Verify import and migration isolation**

Run:

```bash
rg -n 'rock/internal/(service|repository|middleware|model)|rock/pkg/response|cmd/server' \
  backend/cmd/controlplane backend/cmd/controlplane-bootstrap backend/internal/controlplane
rg -n 'applications|pipelines|environments|k8s_clusters|artifact' \
  backend/controlplane/migrations backend/internal/controlplane
```

Expected: both commands produce no output.

- [ ] **Step 5: Run repository-wide backend regression**

Run: `cd backend && go test ./...`

Expected: PASS. Legacy tests are verification only; the new control plane still has no imports or runtime dependency on legacy code.

- [ ] **Step 6: Commit developer workflow**

```bash
git add Makefile backend/controlplane/README.md
git commit -m "docs: add standalone control plane workflow"
```

## Final Acceptance

After all tasks, verify the design requirements together:

```bash
cd backend && go test -race ./internal/controlplane/...
cd backend && go test ./...
cd backend && go build ./cmd/controlplane ./cmd/controlplane-bootstrap
git diff --check
```

Then inspect `git status --short` and confirm only intentional user-owned architecture documents remain uncommitted. Do not stage or modify those files as part of this implementation.
