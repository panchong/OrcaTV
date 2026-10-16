package handler

import (
	"strings"

	"infinite-canvas/backend/internal/app"
	"infinite-canvas/backend/internal/identity"
	"infinite-canvas/backend/internal/model"

	"github.com/gin-gonic/gin"
)

// currentUser resolves the caller for the request.
//
// In the OrcaTV (server) profile the identity middleware has already verified
// the OrcaDock-backed session and installed a Principal; that Principal is the
// only source of user identity, and client-supplied ids are ignored. In the
// desktop profile there is no session and the request scope resolves the local
// workspace owner.
func currentUser(c *gin.Context, svc *app.Service) (*model.User, error) {
	if principal, err := CurrentPrincipal(c); err == nil {
		return PrincipalUser(principal), nil
	}
	if _, exists := c.Get(workspaceContextKey); exists {
		scope, err := CurrentWorkspace(c)
		if err != nil {
			return nil, err
		}
		return svc.WorkspaceOwner(scope.ID)
	}
	return svc.LocalWorkspaceOwner()
}

// PrincipalUser projects an authenticated OrcaTV Principal onto the model.User
// shape the existing repositories filter by. The row is synthesized: OrcaTV
// stores ownership on user_id columns, not a local users table.
func PrincipalUser(principal identity.Principal) *model.User {
	userID := strings.TrimSpace(principal.UserID)
	displayName := strings.TrimSpace(principal.DisplayName)
	if displayName == "" {
		displayName = userID
	}
	role := model.UserRoleUser
	if principal.IsAdmin() {
		role = model.UserRoleAdmin
	}
	return &model.User{
		ID: userID, Username: userID, DisplayName: displayName,
		Role: role, Status: model.UserStatusActive,
	}
}
