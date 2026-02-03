package service

import (
	"context"
	"fmt"
	"strconv"
	
	"github.com/pennsieve/account-service/internal/errors"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/store_postgres"
)

type PermissionService struct {
	NodeAccessStore    store_dynamodb.NodeAccessStore
	TeamStore          store_postgres.TeamStore
	OrganizationStore  store_postgres.OrganizationStore
	NodeStore          store_dynamodb.NodeStore
}

func NewPermissionService(nodeAccessStore store_dynamodb.NodeAccessStore, teamStore store_postgres.TeamStore) *PermissionService {
	return &PermissionService{
		NodeAccessStore:   nodeAccessStore,
		TeamStore:         teamStore,
		OrganizationStore: nil, // Optional - set via SetOrganizationStore if needed for user validation
		NodeStore:         nil, // Optional - set via SetNodeStore if needed
	}
}

// SetOrganizationStore allows setting the organization store for user validation and cleanup functionality
func (s *PermissionService) SetOrganizationStore(orgStore store_postgres.OrganizationStore) {
	s.OrganizationStore = orgStore
}

// SetNodeStore allows setting the node store for auto-healing functionality
func (s *PermissionService) SetNodeStore(nodeStore store_dynamodb.NodeStore) {
	s.NodeStore = nodeStore
}

// CheckNodeAccess checks if a user has access to a compute node
// For organization-independent nodes, only direct user access (owner) is checked
func (s *PermissionService) CheckNodeAccess(ctx context.Context, userId, nodeUuid, organizationId string) (bool, error) {
	nodeId := models.FormatNodeId(nodeUuid)
	
	// 1. Check direct user access (owner or shared)
	userEntityId := models.FormatEntityId(models.EntityTypeUser, userId)
	hasAccess, err := s.NodeAccessStore.HasAccess(ctx, userEntityId, nodeId)
	if err != nil {
		return false, fmt.Errorf("error checking user access: %w", err)
	}
	if hasAccess {
		return true, nil
	}
	
	// For organization-independent nodes, stop here (only owner access allowed)
	if organizationId == "" {
		return false, nil
	}
	
	// 2. Check workspace-wide access (only for organization-bound nodes)
	workspaceEntityId := models.FormatEntityId(models.EntityTypeWorkspace, organizationId)
	hasAccess, err = s.NodeAccessStore.HasAccess(ctx, workspaceEntityId, nodeId)
	if err != nil {
		return false, fmt.Errorf("error checking workspace access: %w", err)
	}
	if hasAccess {
		return true, nil
	}
	
	// 3. Check team access (only for organization-bound nodes)
	if s.TeamStore != nil {
		userIdInt, err := strconv.ParseInt(userId, 10, 64)
		if err == nil {
			orgIdInt, err := strconv.ParseInt(organizationId, 10, 64)
			if err == nil {
				teams, err := s.TeamStore.GetUserTeams(ctx, userIdInt, orgIdInt)
				if err != nil {
					// Log error but continue - team lookup failure shouldn't block access
					// In production, you'd want proper logging here
				} else {
					// Check if any of the user's teams have access
					teamEntityIds := make([]string, len(teams))
					for i, team := range teams {
						teamEntityIds[i] = models.FormatEntityId(models.EntityTypeTeam, strconv.FormatInt(team.TeamId, 10))
					}
					
					hasTeamAccess, err := s.NodeAccessStore.BatchCheckAccess(ctx, teamEntityIds, nodeId)
					if err != nil {
						return false, fmt.Errorf("error checking team access: %w", err)
					}
					if hasTeamAccess {
						return true, nil
					}
				}
			}
		}
	}
	
	return false, nil
}

// GetAccessibleNodes returns all nodes a user can access
func (s *PermissionService) GetAccessibleNodes(ctx context.Context, userId, organizationId string) ([]string, error) {
	nodeUuids := make(map[string]bool)
	
	// 1. Get direct user access
	userEntityId := models.FormatEntityId(models.EntityTypeUser, userId)
	userAccess, err := s.NodeAccessStore.GetEntityAccess(ctx, userEntityId)
	if err != nil {
		return nil, fmt.Errorf("error getting user access: %w", err)
	}
	for _, access := range userAccess {
		nodeUuids[access.NodeUuid] = true
	}
	
	// 2. Get workspace-wide accessible nodes
	workspaceNodes, err := s.NodeAccessStore.GetWorkspaceNodes(ctx, organizationId)
	if err != nil {
		return nil, fmt.Errorf("error getting workspace nodes: %w", err)
	}
	for _, access := range workspaceNodes {
		nodeUuids[access.NodeUuid] = true
	}
	
	// 3. Get team-accessible nodes
	if s.TeamStore != nil {
		userIdInt, _ := strconv.ParseInt(userId, 10, 64)
		orgIdInt, _ := strconv.ParseInt(organizationId, 10, 64)
		
		teams, err := s.TeamStore.GetUserTeams(ctx, userIdInt, orgIdInt)
		if err == nil {
			for _, team := range teams {
				teamEntityId := models.FormatEntityId(models.EntityTypeTeam, strconv.FormatInt(team.TeamId, 10))
				teamAccess, err := s.NodeAccessStore.GetEntityAccess(ctx, teamEntityId)
				if err == nil {
					for _, access := range teamAccess {
						nodeUuids[access.NodeUuid] = true
					}
				}
			}
		}
	}
	
	// Convert map to slice
	result := make([]string, 0, len(nodeUuids))
	for uuid := range nodeUuids {
		result = append(result, uuid)
	}
	
	return result, nil
}

// SetNodePermissions updates the permissions for a node
// For organization-independent nodes, only private access is allowed
func (s *PermissionService) SetNodePermissions(ctx context.Context, nodeUuid string, req models.NodeAccessRequest, ownerId, organizationId, grantedBy string) error {
	nodeId := models.FormatNodeId(nodeUuid)
	
	// Validate request for organization-independent nodes
	if organizationId == "" {
		if err := req.ValidateForOrganizationIndependent(); err != nil {
			return err
		}
	}
	
	// First, get current access to determine what needs to be removed
	currentAccess, err := s.NodeAccessStore.GetNodeAccess(ctx, nodeUuid)
	if err != nil {
		return fmt.Errorf("error getting current access: %w", err)
	}
	
	// Build map of desired access
	desiredAccess := make(map[string]bool)
	
	// Owner always has access
	ownerEntityId := models.FormatEntityId(models.EntityTypeUser, ownerId)
	desiredAccess[ownerEntityId] = true
	
	// Handle access scope settings
	switch req.AccessScope {
	case models.AccessScopeWorkspace:
		workspaceEntityId := models.FormatEntityId(models.EntityTypeWorkspace, organizationId)
		desiredAccess[workspaceEntityId] = true
		
	case models.AccessScopeShared:
		// Validate and add shared users
		for _, userId := range req.SharedWithUsers {
			// Validate user exists if we have OrganizationStore
			if s.OrganizationStore != nil {
				userIdInt, err := strconv.ParseInt(userId, 10, 64)
				if err != nil {
					return fmt.Errorf("invalid user ID format: %s", userId)
				}
				
				userExists, err := s.OrganizationStore.CheckUserExists(ctx, userIdInt)
				if err != nil {
					return fmt.Errorf("error checking if user %s exists: %w", userId, err)
				}
				
				if !userExists {
					return fmt.Errorf("user %s does not exist", userId)
				}
			}
			
			userEntityId := models.FormatEntityId(models.EntityTypeUser, userId)
			desiredAccess[userEntityId] = true
		}
		
		// Validate and add shared teams
		for _, teamId := range req.SharedWithTeams {
			// Validate team exists if we have TeamStore
			if s.TeamStore != nil {
				teamIdInt, err := strconv.ParseInt(teamId, 10, 64)
				if err != nil {
					return fmt.Errorf("invalid team ID format: %s", teamId)
				}
				
				team, err := s.TeamStore.GetTeamById(ctx, teamIdInt)
				if err != nil {
					return fmt.Errorf("error checking if team %s exists: %w", teamId, err)
				}
				
				if team == nil {
					return fmt.Errorf("team %s does not exist", teamId)
				}
			}
			
			teamEntityId := models.FormatEntityId(models.EntityTypeTeam, teamId)
			desiredAccess[teamEntityId] = true
		}
	}
	
	// Remove access that is no longer desired
	for _, access := range currentAccess {
		if !desiredAccess[access.EntityId] && access.EntityId != ownerEntityId {
			err = s.NodeAccessStore.RevokeAccess(ctx, access.EntityId, nodeId)
			if err != nil {
				return fmt.Errorf("error revoking access for %s: %w", access.EntityId, err)
			}
		}
	}
	
	// Create map of current access for comparison
	currentAccessMap := make(map[string]bool)
	for _, access := range currentAccess {
		currentAccessMap[access.EntityId] = true
	}
	
	// Grant new access
	var newAccesses []models.NodeAccess
	
	// Always ensure owner has access
	if !currentAccessMap[ownerEntityId] {
		newAccesses = append(newAccesses, models.NodeAccess{
			EntityId:       ownerEntityId,
			NodeId:         nodeId,
			EntityType:     models.EntityTypeUser,
			EntityRawId:    ownerId,
			NodeUuid:       nodeUuid,
			AccessType:     models.AccessTypeOwner,
			OrganizationId: organizationId,
			GrantedBy:      grantedBy,
		})
	}
	
	// Handle new access scope-based access
	switch req.AccessScope {
	case models.AccessScopeWorkspace:
		workspaceEntityId := models.FormatEntityId(models.EntityTypeWorkspace, organizationId)
		if !currentAccessMap[workspaceEntityId] {
			newAccesses = append(newAccesses, models.NodeAccess{
				EntityId:       workspaceEntityId,
				NodeId:         nodeId,
				EntityType:     models.EntityTypeWorkspace,
				EntityRawId:    organizationId,
				NodeUuid:       nodeUuid,
				AccessType:     models.AccessTypeWorkspace,
				OrganizationId: organizationId,
				GrantedBy:      grantedBy,
			})
		}
		
	case models.AccessScopeShared:
		// Grant access to shared users
		for _, userId := range req.SharedWithUsers {
			userEntityId := models.FormatEntityId(models.EntityTypeUser, userId)
			if !currentAccessMap[userEntityId] {
				newAccesses = append(newAccesses, models.NodeAccess{
					EntityId:       userEntityId,
					NodeId:         nodeId,
					EntityType:     models.EntityTypeUser,
					EntityRawId:    userId,
					NodeUuid:       nodeUuid,
					AccessType:     models.AccessTypeShared,
					OrganizationId: organizationId,
					GrantedBy:      grantedBy,
				})
			}
		}
		
		// Grant access to shared teams
		for _, teamId := range req.SharedWithTeams {
			teamEntityId := models.FormatEntityId(models.EntityTypeTeam, teamId)
			if !currentAccessMap[teamEntityId] {
				newAccesses = append(newAccesses, models.NodeAccess{
					EntityId:       teamEntityId,
					NodeId:         nodeId,
					EntityType:     models.EntityTypeTeam,
					EntityRawId:    teamId,
					NodeUuid:       nodeUuid,
					AccessType:     models.AccessTypeShared,
					OrganizationId: organizationId,
					GrantedBy:      grantedBy,
				})
			}
		}
	}
	
	// Batch grant new accesses
	if len(newAccesses) > 0 {
		if batchStore, ok := s.NodeAccessStore.(*store_dynamodb.NodeAccessDatabaseStore); ok {
			err = batchStore.BatchGrantAccess(ctx, newAccesses)
		} else {
			// Fall back to individual grants
			for _, access := range newAccesses {
				err = s.NodeAccessStore.GrantAccess(ctx, access)
				if err != nil {
					return fmt.Errorf("error granting access: %w", err)
				}
			}
		}
	}
	
	return nil
}

// GetNodePermissions returns the current permission settings for a node
func (s *PermissionService) GetNodePermissions(ctx context.Context, nodeUuid string) (*models.NodeAccessResponse, error) {
	accessList, err := s.NodeAccessStore.GetNodeAccess(ctx, nodeUuid)
	if err != nil {
		return nil, fmt.Errorf("error getting node access: %w", err)
	}
	
	response := &models.NodeAccessResponse{
		NodeUuid:        nodeUuid,
		AccessScope:     models.AccessScopePrivate,
		SharedWithUsers: []string{},
		SharedWithTeams: []string{},
	}
	
	for _, access := range accessList {
		switch access.EntityType {
		case models.EntityTypeUser:
			if access.AccessType == models.AccessTypeOwner {
				response.Owner = access.EntityRawId
			} else if access.AccessType == models.AccessTypeShared {
				response.SharedWithUsers = append(response.SharedWithUsers, access.EntityRawId)
			}
		case models.EntityTypeTeam:
			response.SharedWithTeams = append(response.SharedWithTeams, access.EntityRawId)
		case models.EntityTypeWorkspace:
			response.AccessScope = models.AccessScopeWorkspace
			response.OrganizationId = access.OrganizationId
		}
	}
	
	// Determine access scope - workspace takes precedence over shared
	if response.AccessScope != models.AccessScopeWorkspace {
		if len(response.SharedWithUsers) > 0 || len(response.SharedWithTeams) > 0 {
			response.AccessScope = models.AccessScopeShared
		}
	}
	
	// Auto-healing: If no owner found in access entries, look up actual node owner and restore access
	if response.Owner == "" && s.NodeStore != nil {
		// Look up the actual node to get the real owner
		node, err := s.NodeStore.GetById(ctx, nodeUuid)
		if err == nil && node.UserId != "" {
			// Node exists and has an owner, but no access entry - this is an inconsistent state
			// Create the missing owner access entry
			nodeId := models.FormatNodeId(nodeUuid)
			ownerEntityId := models.FormatEntityId(models.EntityTypeUser, node.UserId)
			
			ownerAccess := models.NodeAccess{
				EntityId:       ownerEntityId,
				NodeId:         nodeId,
				EntityType:     models.EntityTypeUser,
				EntityRawId:    node.UserId,
				NodeUuid:       nodeUuid,
				AccessType:     models.AccessTypeOwner,
				OrganizationId: node.OrganizationId,
				GrantedBy:      node.UserId, // Self-granted for auto-healing
			}
			
			// Grant the missing owner access
			err = s.NodeAccessStore.GrantAccess(ctx, ownerAccess)
			if err != nil {
				// Log but don't fail - return current state
				// In production, you'd want proper logging here
				fmt.Printf("Warning: Failed to auto-restore owner access for node %s: %v\n", nodeUuid, err)
			} else {
				// Successfully restored owner access - update response
				response.Owner = node.UserId
				if response.OrganizationId == "" {
					response.OrganizationId = node.OrganizationId
				}
			}
		}
	}
	
	// Cleanup stale user/team permissions if PostgreSQL stores are available
	var stalePermissions []models.NodeAccess
	if s.OrganizationStore != nil || s.TeamStore != nil {
		fmt.Printf("DEBUG: Starting cleanup, orgStore=%v, teamStore=%v\n", s.OrganizationStore != nil, s.TeamStore != nil)
		for _, access := range accessList {
			shouldRemove := false
			
			// Check if user permissions reference users that no longer exist
			if access.EntityType == models.EntityTypeUser && s.OrganizationStore != nil {
				fmt.Printf("DEBUG: Checking user access: EntityType=%s, AccessType=%s, EntityRawId=%s\n", access.EntityType, access.AccessType, access.EntityRawId)
				// Skip owner validation (owners should always be valid)
				if access.AccessType != models.AccessTypeOwner {
					userIdInt, err := strconv.ParseInt(access.EntityRawId, 10, 64)
					if err == nil {
						fmt.Printf("DEBUG: Calling CheckUserExists for user %d\n", userIdInt)
						userExists, err := s.OrganizationStore.CheckUserExists(ctx, userIdInt)
						if err != nil {
							// Log error but don't remove on DB error
							fmt.Printf("Warning: Error checking if user %s exists: %v\n", access.EntityRawId, err)
						} else if !userExists {
							shouldRemove = true
							fmt.Printf("Info: Removing stale permission for deleted user %s on node %s\n", access.EntityRawId, nodeUuid)
						} else {
							fmt.Printf("DEBUG: User %d exists, keeping permission\n", userIdInt)
						}
					} else {
						fmt.Printf("DEBUG: Failed to parse user ID %s: %v\n", access.EntityRawId, err)
					}
				} else {
					fmt.Printf("DEBUG: Skipping owner validation for user %s\n", access.EntityRawId)
				}
			} else {
				fmt.Printf("DEBUG: Skipping user check: EntityType=%s, hasOrgStore=%v\n", access.EntityType, s.OrganizationStore != nil)
			}
			
			// Check if team permissions reference teams that no longer exist
			if access.EntityType == models.EntityTypeTeam && s.TeamStore != nil {
				teamIdInt, err := strconv.ParseInt(access.EntityRawId, 10, 64)
				if err == nil {
					team, err := s.TeamStore.GetTeamById(ctx, teamIdInt)
					if err != nil {
						// Log other errors but don't remove on DB error
						fmt.Printf("Warning: Error checking if team %s exists: %v\n", access.EntityRawId, err)
					} else if team == nil {
						// Team doesn't exist
						shouldRemove = true
						fmt.Printf("Info: Removing stale permission for deleted team %s on node %s\n", access.EntityRawId, nodeUuid)
					}
				}
			}
			
			if shouldRemove {
				stalePermissions = append(stalePermissions, access)
			}
		}
		
		// Remove stale permissions from DynamoDB
		if len(stalePermissions) > 0 {
			nodeId := models.FormatNodeId(nodeUuid)
			for _, staleAccess := range stalePermissions {
				err := s.NodeAccessStore.RevokeAccess(ctx, staleAccess.EntityId, nodeId)
				if err != nil {
					// Log error but continue with other cleanups
					fmt.Printf("Warning: Failed to remove stale permission %s for node %s: %v\n", staleAccess.EntityId, nodeUuid, err)
				} else {
					// Remove from response lists
					if staleAccess.EntityType == models.EntityTypeUser {
						// Remove from SharedWithUsers
						for i, userId := range response.SharedWithUsers {
							if userId == staleAccess.EntityRawId {
								response.SharedWithUsers = append(response.SharedWithUsers[:i], response.SharedWithUsers[i+1:]...)
								break
							}
						}
					} else if staleAccess.EntityType == models.EntityTypeTeam {
						// Remove from SharedWithTeams
						for i, teamId := range response.SharedWithTeams {
							if teamId == staleAccess.EntityRawId {
								response.SharedWithTeams = append(response.SharedWithTeams[:i], response.SharedWithTeams[i+1:]...)
								break
							}
						}
					}
				}
			}
			
			// Recalculate access scope after cleanup
			if response.AccessScope != models.AccessScopeWorkspace {
				if len(response.SharedWithUsers) > 0 || len(response.SharedWithTeams) > 0 {
					response.AccessScope = models.AccessScopeShared
				} else {
					response.AccessScope = models.AccessScopePrivate
				}
			}
		}
	}
	
	// Set organization independent flag
	response.OrganizationIndependent = response.IsOrganizationIndependent()
	
	return response, nil
}

// AttachNodeToOrganization attaches an organization-independent node to an organization
// This allows the node to be shared with users and teams within the organization
func (s *PermissionService) AttachNodeToOrganization(ctx context.Context, nodeUuid, organizationId, userId string) error {
	// First, check if the user is the owner of the node
	userEntityId := models.FormatEntityId(models.EntityTypeUser, userId)
	nodeId := models.FormatNodeId(nodeUuid)
	
	hasAccess, err := s.NodeAccessStore.HasAccess(ctx, userEntityId, nodeId)
	if err != nil {
		return fmt.Errorf("error checking user access: %w", err)
	}
	if !hasAccess {
		return errors.ErrForbidden
	}
	
	// Get current access to verify node is organization-independent
	currentAccess, err := s.NodeAccessStore.GetNodeAccess(ctx, nodeUuid)
	if err != nil {
		return fmt.Errorf("error getting current access: %w", err)
	}
	
	// Find owner access to check if node is organization-independent
	var ownerAccess *models.NodeAccess
	for _, access := range currentAccess {
		if access.EntityType == models.EntityTypeUser && access.AccessType == models.AccessTypeOwner && access.EntityRawId == userId {
			ownerAccess = &access
			break
		}
	}
	
	if ownerAccess == nil {
		return errors.ErrForbidden // User is not the owner
	}
	
	if !ownerAccess.IsOrganizationIndependent() {
		return models.ErrCannotAttachNodeWithExistingOrganization
	}
	
	// Update the owner's access entry to include the organization
	updatedOwnerAccess := *ownerAccess
	updatedOwnerAccess.OrganizationId = organizationId
	
	// Remove old access and grant new access with organization
	err = s.NodeAccessStore.RevokeAccess(ctx, ownerAccess.EntityId, nodeId)
	if err != nil {
		return fmt.Errorf("error removing old access: %w", err)
	}
	
	err = s.NodeAccessStore.GrantAccess(ctx, updatedOwnerAccess)
	if err != nil {
		return fmt.Errorf("error granting new access with organization: %w", err)
	}
	
	return nil
}

// DetachNodeFromOrganization detaches a compute node from its organization, making it organization-independent (private)
// Note: The handler should verify the user is the account owner before calling this
func (s *PermissionService) DetachNodeFromOrganization(ctx context.Context, nodeUuid, userId string) error {
	nodeId := models.FormatNodeId(nodeUuid)
	
	// Get current access to verify node has an organization
	currentAccess, err := s.NodeAccessStore.GetNodeAccess(ctx, nodeUuid)
	if err != nil {
		return fmt.Errorf("error getting current access: %w", err)
	}
	
	// Find the owner access entry to check if node has an organization
	var ownerAccess *models.NodeAccess
	for _, access := range currentAccess {
		if access.EntityType == models.EntityTypeUser && access.AccessType == models.AccessTypeOwner {
			ownerAccess = &access
			break
		}
	}
	
	if ownerAccess == nil {
		return errors.ErrNotFound // No owner found - invalid state
	}
	
	if ownerAccess.IsOrganizationIndependent() {
		return models.ErrOrganizationIndependentNodeCannotBeShared // Node is already organization-independent
	}
	
	// Remove all current access entries (they all have organizationId)
	for _, access := range currentAccess {
		err = s.NodeAccessStore.RevokeAccess(ctx, access.EntityId, nodeId)
		if err != nil {
			return fmt.Errorf("error removing access entry: %w", err)
		}
	}
	
	// Grant new owner access without organization (making it organization-independent and private)
	newOwnerAccess := models.NodeAccess{
		EntityId:       ownerAccess.EntityId,
		NodeId:         nodeId,
		EntityType:     models.EntityTypeUser,
		EntityRawId:    ownerAccess.EntityRawId, // Use the original owner's ID
		NodeUuid:       nodeUuid,
		AccessType:     models.AccessTypeOwner,
		OrganizationId: "", // No organization - this makes it organization-independent
		GrantedBy:      ownerAccess.EntityRawId, // Owner is granting to themselves
	}
	
	err = s.NodeAccessStore.GrantAccess(ctx, newOwnerAccess)
	if err != nil {
		return fmt.Errorf("error granting new organization-independent access: %w", err)
	}
	
	return nil
}