package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"rock/internal/controlplane/team"
)

type handlers struct {
	teams     TeamQueries
	readiness Readiness
}

type userDTO struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Status      string `json:"status"`
}

type teamMembershipDTO struct {
	TeamID       string `json:"team_id"`
	Slug         string `json:"slug"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	MembershipID string `json:"membership_id"`
	Role         string `json:"role"`
}

func (h handlers) health(c *gin.Context) {
	writeSuccess(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h handlers) ready(c *gin.Context) {
	if err := h.readiness.Ping(c.Request.Context()); err != nil {
		writeError(c, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service is unavailable")
		return
	}
	writeSuccess(c, http.StatusOK, gin.H{"status": "ready"})
}

func (h handlers) me(c *gin.Context) {
	user := sessionFrom(c).User
	writeSuccess(c, http.StatusOK, userDTO{
		ID: user.ID.String(), Email: user.Email, DisplayName: user.DisplayName, Status: string(user.Status),
	})
}

func (h handlers) myTeams(c *gin.Context) {
	currentSession := sessionFrom(c)
	contexts, err := h.teams.ListEffectiveMemberships(c.Request.Context(), currentSession.User.ID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		return
	}
	result := make([]teamMembershipDTO, 0, len(contexts))
	for i := range contexts {
		result = append(result, membershipDTO(contexts[i]))
	}
	writeSuccess(c, http.StatusOK, result)
}

func (h handlers) teamContext(c *gin.Context) {
	resolvedScope := scopeFrom(c)
	if resolvedScope.ActiveTeamID == nil || resolvedScope.MembershipID == nil || resolvedScope.MembershipRole == nil {
		writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		return
	}
	context, err := h.teams.ResolveEffectiveMembership(c.Request.Context(), resolvedScope.ActorUserID, *resolvedScope.ActiveTeamID)
	if errors.Is(err, team.ErrAccessDenied) {
		writeError(c, http.StatusForbidden, "TEAM_ACCESS_DENIED", "team access is denied")
		return
	}
	if err != nil {
		writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		return
	}
	if context.Membership.ID != *resolvedScope.MembershipID || context.Membership.Role != *resolvedScope.MembershipRole {
		writeError(c, http.StatusForbidden, "TEAM_ACCESS_DENIED", "team access is denied")
		return
	}
	writeSuccess(c, http.StatusOK, membershipDTO(context))
}

func membershipDTO(context team.MembershipContext) teamMembershipDTO {
	return teamMembershipDTO{
		TeamID: context.Team.ID.String(), Slug: context.Team.Slug, Name: context.Team.Name,
		Status: string(context.Team.Status), MembershipID: context.Membership.ID.String(), Role: string(context.Membership.Role),
	}
}
