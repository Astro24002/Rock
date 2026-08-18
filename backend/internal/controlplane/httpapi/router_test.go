package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"rock/internal/controlplane/authn"
	"rock/internal/controlplane/httpapi"
	"rock/internal/controlplane/identity"
	"rock/internal/controlplane/scope"
	"rock/internal/controlplane/team"
	"rock/internal/controlplane/testkit"
)

var (
	httpNow      = time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	httpUserID   = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	httpTeamID   = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	httpMemberID = uuid.MustParse("33333333-3333-4333-8333-333333333333")
)

func TestHealthAndReadiness(t *testing.T) {
	router := newTestRouter(t, fixture{})
	health := performRequest(router, http.MethodGet, "/healthz", "", nil)
	require.Equal(t, http.StatusOK, health.Code)
	requireJSONData(t, health, "status", "ok")

	ready := performRequest(router, http.MethodGet, "/readyz", "", nil)
	require.Equal(t, http.StatusOK, ready.Code)
	requireJSONData(t, ready, "status", "ready")

	unavailable := newTestRouter(t, fixture{pingErr: errors.New("database DSN secret")})
	response := performRequest(unavailable, http.MethodGet, "/readyz", "", nil)
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	requireJSONError(t, response, "SERVICE_UNAVAILABLE")
	require.NotContains(t, response.Body.String(), "DSN")
}

func TestUnknownRoutesUseControlPlaneErrorEnvelope(t *testing.T) {
	router := newTestRouter(t, fixture{})
	missing := performRequest(router, http.MethodGet, "/v1/does-not-exist", "", map[string][]string{"X-Request-ID": {"route-404"}})
	require.Equal(t, http.StatusNotFound, missing.Code)
	require.Equal(t, "route-404", decodeBody(t, missing)["request_id"])
	requireJSONError(t, missing, "ROUTE_NOT_FOUND")

	method := performRequest(router, http.MethodPost, "/healthz", "", map[string][]string{"X-Request-ID": {"route-405"}})
	require.Equal(t, http.StatusMethodNotAllowed, method.Code)
	require.Equal(t, "route-405", decodeBody(t, method)["request_id"])
	requireJSONError(t, method, "METHOD_NOT_ALLOWED")
}

func TestAuthenticationErrors(t *testing.T) {
	tests := []struct {
		name       string
		authHeader string
		fixture    fixture
		status     int
		code       string
	}{
		{name: "missing", status: http.StatusUnauthorized, code: "AUTHENTICATION_REQUIRED"},
		{name: "wrong scheme", authHeader: "Basic abc", status: http.StatusUnauthorized, code: "AUTHENTICATION_REQUIRED"},
		{name: "invalid token", authHeader: "Bearer invalid", status: http.StatusUnauthorized, code: "INVALID_IDENTITY_TOKEN"},
		{name: "unknown identity", authHeader: "Bearer valid-token", fixture: fixture{userErr: identity.ErrUserNotFound}, status: http.StatusForbidden, code: "IDENTITY_NOT_REGISTERED"},
		{name: "inactive identity", authHeader: "Bearer valid-token", fixture: fixture{userStatus: identity.UserStatusSuspended}, status: http.StatusForbidden, code: "IDENTITY_INACTIVE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := performRequest(newTestRouter(t, tt.fixture), http.MethodGet, "/v1/me", tt.authHeader, nil)
			require.Equal(t, tt.status, response.Code)
			requireJSONError(t, response, tt.code)
		})
	}
}

func TestMeAndMyTeams(t *testing.T) {
	response := performRequest(newTestRouter(t, fixture{}), http.MethodGet, "/v1/me", "Bearer valid-token", nil)
	require.Equal(t, http.StatusOK, response.Code)
	body := decodeBody(t, response)
	data := body["data"].(map[string]any)
	require.Equal(t, httpUserID.String(), data["id"])
	require.Equal(t, "user@example.com", data["email"])

	teamsResponse := performRequest(newTestRouter(t, fixture{}), http.MethodGet, "/v1/me/teams", "Bearer valid-token", nil)
	require.Equal(t, http.StatusOK, teamsResponse.Code)
	teamsData := decodeBody(t, teamsResponse)["data"].([]any)
	require.Len(t, teamsData, 1)
	teamData := teamsData[0].(map[string]any)
	require.Equal(t, httpTeamID.String(), teamData["team_id"])
	require.Equal(t, string(team.RoleDeveloper), teamData["role"])
}

func TestTeamContextRequiresExplicitMatchingTeam(t *testing.T) {
	router := newTestRouter(t, fixture{})
	path := "/v1/teams/" + httpTeamID.String() + "/context"

	missing := performRequest(router, http.MethodGet, path, "Bearer valid-token", nil)
	require.Equal(t, http.StatusBadRequest, missing.Code)
	requireJSONError(t, missing, "TEAM_CONTEXT_REQUIRED")

	mismatch := performRequest(router, http.MethodGet, path, "Bearer valid-token", map[string][]string{
		"X-Active-Team-Id": {uuid.NewString()},
	})
	require.Equal(t, http.StatusBadRequest, mismatch.Code)
	requireJSONError(t, mismatch, "INVALID_TEAM_CONTEXT")

	duplicate := performRequest(router, http.MethodGet, path, "Bearer valid-token", map[string][]string{
		"X-Active-Team-Id": {httpTeamID.String(), httpTeamID.String()},
	})
	require.Equal(t, http.StatusBadRequest, duplicate.Code)
	requireJSONError(t, duplicate, "INVALID_TEAM_CONTEXT")

	ok := performRequest(router, http.MethodGet, path, "Bearer valid-token", map[string][]string{
		"X-Active-Team-Id": {httpTeamID.String()},
	})
	require.Equal(t, http.StatusOK, ok.Code)
	data := decodeBody(t, ok)["data"].(map[string]any)
	require.Equal(t, httpTeamID.String(), data["team_id"])
	require.Equal(t, "delivery", data["slug"])
	require.Equal(t, string(team.RoleDeveloper), data["role"])
}

func TestPlatformRoleDoesNotAuthorizeTeamContext(t *testing.T) {
	router := newTestRouter(t, fixture{membershipErr: team.ErrAccessDenied})
	response := performRequest(router, http.MethodGet, "/v1/teams/"+httpTeamID.String()+"/context", "Bearer valid-token", map[string][]string{
		"X-Active-Team-Id": {httpTeamID.String()},
	})
	require.Equal(t, http.StatusForbidden, response.Code)
	requireJSONError(t, response, "TEAM_ACCESS_DENIED")
}

func TestRequestIDPropagationAndInternalErrorRedaction(t *testing.T) {
	response := performRequest(newTestRouter(t, fixture{membershipErr: errors.New("database password secret")}), http.MethodGet,
		"/v1/me/teams", "Bearer valid-token", map[string][]string{"X-Request-ID": {"request-123"}})
	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.Equal(t, "request-123", response.Header().Get("X-Request-ID"))
	body := decodeBody(t, response)
	require.Equal(t, "request-123", body["request_id"])
	require.NotContains(t, response.Body.String(), "password")
	requireJSONError(t, response, "INTERNAL_ERROR")
}

type fixture struct {
	userStatus    identity.UserStatus
	userErr       error
	membershipErr error
	pingErr       error
}

func newTestRouter(t *testing.T, input fixture) http.Handler {
	t.Helper()
	gin.SetMode(gin.TestMode)
	status := input.userStatus
	if status == "" {
		status = identity.UserStatusActive
	}
	reader := &fakeReader{
		user:    identity.User{ID: httpUserID, Email: "user@example.com", DisplayName: "User", Status: status, ProfileVersion: 1},
		userErr: input.userErr, membershipErr: input.membershipErr,
		membership: team.MembershipContext{
			Team:       team.Team{ID: httpTeamID, Slug: "delivery", Name: "Delivery", Status: team.TeamStatusActive, ConfigVersion: 1},
			Membership: team.Membership{ID: httpMemberID, UserID: httpUserID, TeamID: httpTeamID, Role: team.RoleDeveloper, Status: team.MembershipStatusActive, EffectiveAt: httpNow.Add(-time.Hour)},
		},
	}
	clock := testkit.FixedClock{Time: httpNow}
	return httpapi.NewRouter(httpapi.Dependencies{
		Authenticator: testkit.Authenticator{Token: "valid-token", Principal: authn.Principal{Subject: httpUserID, Email: "claim@example.com", AuthenticatedAt: httpNow.Add(-time.Minute)}},
		Identity:      identity.NewService(reader), Teams: team.NewService(reader, clock), Scope: scope.NewResolver(reader, clock),
		Readiness: fakePinger{err: input.pingErr},
	})
}

type fakeReader struct {
	user          identity.User
	userErr       error
	membership    team.MembershipContext
	membershipErr error
}

func (r *fakeReader) FindUser(context.Context, uuid.UUID) (identity.User, error) {
	return r.user, r.userErr
}
func (r *fakeReader) ListEffectiveMemberships(context.Context, uuid.UUID, time.Time) ([]team.MembershipContext, error) {
	if r.membershipErr != nil {
		return nil, r.membershipErr
	}
	return []team.MembershipContext{r.membership}, nil
}
func (r *fakeReader) FindEffectiveMembership(context.Context, uuid.UUID, uuid.UUID, time.Time) (team.MembershipContext, error) {
	return r.membership, r.membershipErr
}

type fakePinger struct{ err error }

func (p fakePinger) Ping(context.Context) error { return p.err }

func performRequest(router http.Handler, method, path, authorization string, headers map[string][]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, nil)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	for name, values := range headers {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func decodeBody(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	return body
}

func requireJSONError(t *testing.T, response *httptest.ResponseRecorder, code string) {
	t.Helper()
	errorData := decodeBody(t, response)["error"].(map[string]any)
	require.Equal(t, code, errorData["code"])
}

func requireJSONData(t *testing.T, response *httptest.ResponseRecorder, key string, value any) {
	t.Helper()
	data := decodeBody(t, response)["data"].(map[string]any)
	require.Equal(t, value, data[key])
}
