package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"rock/internal/controlplane/authn"
	"rock/internal/controlplane/identity"
	controlscope "rock/internal/controlplane/scope"
)

const (
	headerRequestID = "X-Request-ID"
	headerTraceID   = "X-Trace-ID"
	headerTeamID    = "X-Active-Team-Id"

	contextRequestID = "controlplane.request_id"
	contextTraceID   = "controlplane.trace_id"
	contextSession   = "controlplane.session"
	contextScope     = "controlplane.scope"
)

type session struct {
	Principal authn.Principal
	User      identity.User
}

func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := safeCorrelationID(c.Request.Header.Values(headerRequestID))
		if requestID == "" {
			requestID = uuid.NewString()
		}
		traceID := safeCorrelationID(c.Request.Header.Values(headerTraceID))
		if traceID == "" {
			traceID = requestID
		}
		c.Set(contextRequestID, requestID)
		c.Set(contextTraceID, traceID)
		c.Header(headerRequestID, requestID)
		c.Header(headerTraceID, traceID)
		c.Next()
	}
}

func recoveryMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recover() != nil {
				if !c.Writer.Written() {
					writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
				}
				c.Abort()
			}
		}()
		c.Next()
	}
}

func authenticationMiddleware(authenticator authn.Authenticator, users IdentityQueries) gin.HandlerFunc {
	return func(c *gin.Context) {
		values := c.Request.Header.Values("Authorization")
		if len(values) != 1 {
			writeError(c, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "authentication is required")
			c.Abort()
			return
		}
		value := values[0]
		parts := strings.Split(value, " ")
		if len(parts) != 2 || parts[0] != "Bearer" || parts[1] == "" || strings.TrimSpace(parts[1]) != parts[1] {
			writeError(c, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "authentication is required")
			c.Abort()
			return
		}

		principal, err := authenticator.Authenticate(c.Request.Context(), parts[1])
		if err != nil {
			if errors.Is(err, authn.ErrInvalidToken) {
				writeError(c, http.StatusUnauthorized, "INVALID_IDENTITY_TOKEN", "identity token is invalid")
			} else {
				writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
			}
			c.Abort()
			return
		}
		user, err := users.ResolveActiveUser(c.Request.Context(), principal.Subject)
		switch {
		case errors.Is(err, identity.ErrNotRegistered):
			writeError(c, http.StatusForbidden, "IDENTITY_NOT_REGISTERED", "identity is not registered")
			c.Abort()
			return
		case errors.Is(err, identity.ErrInactive):
			writeError(c, http.StatusForbidden, "IDENTITY_INACTIVE", "identity is inactive")
			c.Abort()
			return
		case err != nil:
			writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
			c.Abort()
			return
		}
		c.Set(contextSession, session{Principal: principal, User: user})
		c.Next()
	}
}

func teamScopeMiddleware(resolver TeamScopeResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		values := c.Request.Header.Values(headerTeamID)
		if len(values) > 1 {
			writeError(c, http.StatusBadRequest, "INVALID_TEAM_CONTEXT", "active team context is invalid")
			c.Abort()
			return
		}
		rawTeamID := ""
		if len(values) == 1 {
			rawTeamID = values[0]
		}
		currentSession := sessionFrom(c)
		resolved, err := resolver.ResolveTeam(c.Request.Context(), controlscope.TeamRequest{
			RequestID: requestIDFrom(c), TraceID: traceIDFrom(c), ActorUserID: currentSession.User.ID,
			HeaderTeamID: rawTeamID, PathTeamID: c.Param("team_id"), AuthenticatedAt: currentSession.Principal.AuthenticatedAt,
		})
		switch {
		case errors.Is(err, controlscope.ErrTeamContextRequired):
			writeError(c, http.StatusBadRequest, "TEAM_CONTEXT_REQUIRED", "active team context is required")
		case errors.Is(err, controlscope.ErrInvalidTeamContext):
			writeError(c, http.StatusBadRequest, "INVALID_TEAM_CONTEXT", "active team context is invalid")
		case errors.Is(err, controlscope.ErrTeamAccessDenied):
			writeError(c, http.StatusForbidden, "TEAM_ACCESS_DENIED", "team access is denied")
		case err != nil:
			writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		default:
			c.Set(contextScope, resolved)
			c.Next()
			return
		}
		c.Abort()
	}
}

func safeCorrelationID(values []string) string {
	if len(values) != 1 {
		return ""
	}
	value := strings.TrimSpace(values[0])
	if value == "" || len(value) > 128 || strings.ContainsAny(value, "\r\n") {
		return ""
	}
	return value
}

func requestIDFrom(c *gin.Context) string {
	value, _ := c.Get(contextRequestID)
	result, _ := value.(string)
	return result
}

func traceIDFrom(c *gin.Context) string {
	value, _ := c.Get(contextTraceID)
	result, _ := value.(string)
	return result
}

func sessionFrom(c *gin.Context) session {
	value, ok := c.Get(contextSession)
	if !ok {
		panic("authenticated session missing")
	}
	result, ok := value.(session)
	if !ok {
		panic("authenticated session has invalid type")
	}
	return result
}

func scopeFrom(c *gin.Context) controlscope.RequestScope {
	value, ok := c.Get(contextScope)
	if !ok {
		panic("request scope missing")
	}
	result, ok := value.(controlscope.RequestScope)
	if !ok {
		panic("request scope has invalid type")
	}
	return result
}
