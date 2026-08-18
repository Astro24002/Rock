package httpapi

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"rock/internal/controlplane/authn"
	"rock/internal/controlplane/identity"
	controlscope "rock/internal/controlplane/scope"
	"rock/internal/controlplane/team"
)

type IdentityQueries interface {
	ResolveActiveUser(context.Context, uuid.UUID) (identity.User, error)
}

type TeamQueries interface {
	ListEffectiveMemberships(context.Context, uuid.UUID) ([]team.MembershipContext, error)
	ResolveEffectiveMembership(context.Context, uuid.UUID, uuid.UUID) (team.MembershipContext, error)
}

type TeamScopeResolver interface {
	ResolveTeam(context.Context, controlscope.TeamRequest) (controlscope.RequestScope, error)
}

type Readiness interface {
	Ping(context.Context) error
}

type Dependencies struct {
	Authenticator authn.Authenticator
	Identity      IdentityQueries
	Teams         TeamQueries
	Scope         TeamScopeResolver
	Readiness     Readiness
}

func NewRouter(dependencies Dependencies) *gin.Engine {
	router := gin.New()
	router.HandleMethodNotAllowed = true
	router.Use(requestIDMiddleware(), recoveryMiddleware())
	h := handlers{teams: dependencies.Teams, readiness: dependencies.Readiness}
	router.GET("/healthz", h.health)
	router.GET("/readyz", h.ready)

	authenticated := router.Group("/v1", authenticationMiddleware(dependencies.Authenticator, dependencies.Identity))
	authenticated.GET("/me", h.me)
	authenticated.GET("/me/teams", h.myTeams)
	teamScoped := authenticated.Group("/teams/:team_id", teamScopeMiddleware(dependencies.Scope))
	teamScoped.GET("/context", h.teamContext)
	router.NoRoute(func(c *gin.Context) {
		writeError(c, http.StatusNotFound, "ROUTE_NOT_FOUND", "route not found")
	})
	router.NoMethod(func(c *gin.Context) {
		writeError(c, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
	})
	return router
}
