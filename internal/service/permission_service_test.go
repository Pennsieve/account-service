package service

import (
	"context"
	"testing"

	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock implementations for testing

type MockNodeAccessStore struct {
	mock.Mock
}

func (m *MockNodeAccessStore) GrantAccess(ctx context.Context, access models.NodeAccess) error {
	args := m.Called(ctx, access)
	return args.Error(0)
}

func (m *MockNodeAccessStore) RevokeAccess(ctx context.Context, entityId, nodeId string) error {
	args := m.Called(ctx, entityId, nodeId)
	return args.Error(0)
}

func (m *MockNodeAccessStore) HasAccess(ctx context.Context, entityId, nodeId string) (bool, error) {
	args := m.Called(ctx, entityId, nodeId)
	return args.Bool(0), args.Error(1)
}

func (m *MockNodeAccessStore) GetNodeAccess(ctx context.Context, nodeUuid string) ([]models.NodeAccess, error) {
	args := m.Called(ctx, nodeUuid)
	return args.Get(0).([]models.NodeAccess), args.Error(1)
}

func (m *MockNodeAccessStore) GetEntityAccess(ctx context.Context, entityId string) ([]models.NodeAccess, error) {
	args := m.Called(ctx, entityId)
	return args.Get(0).([]models.NodeAccess), args.Error(1)
}

func (m *MockNodeAccessStore) GetWorkspaceNodes(ctx context.Context, organizationId string) ([]models.NodeAccess, error) {
	args := m.Called(ctx, organizationId)
	return args.Get(0).([]models.NodeAccess), args.Error(1)
}

func (m *MockNodeAccessStore) BatchCheckAccess(ctx context.Context, entityIds []string, nodeId string) (bool, error) {
	args := m.Called(ctx, entityIds, nodeId)
	return args.Bool(0), args.Error(1)
}

func (m *MockNodeAccessStore) RemoveAllNodeAccess(ctx context.Context, nodeUuid string) error {
	args := m.Called(ctx, nodeUuid)
	return args.Error(0)
}

func (m *MockNodeAccessStore) UpdateNodeAccessScope(ctx context.Context, nodeUuid string, accessScope models.NodeAccessScope, organizationId, grantedBy string) error {
	args := m.Called(ctx, nodeUuid, accessScope, organizationId, grantedBy)
	return args.Error(0)
}

type MockTeamStore struct {
	mock.Mock
}

func (m *MockTeamStore) GetUserTeams(ctx context.Context, userId, organizationId int64) ([]store_postgres.UserTeam, error) {
	args := m.Called(ctx, userId, organizationId)
	return args.Get(0).([]store_postgres.UserTeam), args.Error(1)
}

func (m *MockTeamStore) GetTeamById(ctx context.Context, teamId int64) (*store_postgres.Team, error) {
	args := m.Called(ctx, teamId)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*store_postgres.Team), args.Error(1)
}

func (m *MockTeamStore) GetTeamMembers(ctx context.Context, teamId int64) ([]int64, error) {
	args := m.Called(ctx, teamId)
	return args.Get(0).([]int64), args.Error(1)
}

func TestPermissionService_CheckNodeAccess_DirectUserAccess(t *testing.T) {
	mockNodeStore := new(MockNodeAccessStore)
	service := NewPermissionService(mockNodeStore, nil)

	ctx := context.Background()
	userId := "user-123"
	nodeUuid := "node-456"
	organizationId := "org-789"
	
	userEntityId := models.FormatEntityId(models.EntityTypeUser, userId)
	nodeId := models.FormatNodeId(nodeUuid)

	// Mock direct user access
	mockNodeStore.On("HasAccess", ctx, userEntityId, nodeId).Return(true, nil)

	hasAccess, err := service.CheckNodeAccess(ctx, userId, nodeUuid, organizationId)

	assert.NoError(t, err)
	assert.True(t, hasAccess)
	mockNodeStore.AssertExpectations(t)
}

func TestPermissionService_CheckNodeAccess_WorkspaceAccess(t *testing.T) {
	mockNodeStore := new(MockNodeAccessStore)
	service := NewPermissionService(mockNodeStore, nil)

	ctx := context.Background()
	userId := "user-123"
	nodeUuid := "node-456"
	organizationId := "org-789"
	
	userEntityId := models.FormatEntityId(models.EntityTypeUser, userId)
	workspaceEntityId := models.FormatEntityId(models.EntityTypeWorkspace, organizationId)
	nodeId := models.FormatNodeId(nodeUuid)

	// Mock no direct user access, but workspace access exists
	mockNodeStore.On("HasAccess", ctx, userEntityId, nodeId).Return(false, nil)
	mockNodeStore.On("HasAccess", ctx, workspaceEntityId, nodeId).Return(true, nil)

	hasAccess, err := service.CheckNodeAccess(ctx, userId, nodeUuid, organizationId)

	assert.NoError(t, err)
	assert.True(t, hasAccess)
	mockNodeStore.AssertExpectations(t)
}

func TestPermissionService_CheckNodeAccess_TeamAccess(t *testing.T) {
	mockNodeStore := new(MockNodeAccessStore)
	mockTeamStore := new(MockTeamStore)
	service := NewPermissionService(mockNodeStore, mockTeamStore)

	ctx := context.Background()
	userId := "123"
	nodeUuid := "node-456"
	organizationId := "789"
	
	userEntityId := models.FormatEntityId(models.EntityTypeUser, userId)
	workspaceEntityId := models.FormatEntityId(models.EntityTypeWorkspace, organizationId)
	nodeId := models.FormatNodeId(nodeUuid)

	// Mock no direct user access and no workspace access
	mockNodeStore.On("HasAccess", ctx, userEntityId, nodeId).Return(false, nil)
	mockNodeStore.On("HasAccess", ctx, workspaceEntityId, nodeId).Return(false, nil)

	// Mock user teams
	teams := []store_postgres.UserTeam{
		{TeamId: 100, TeamNodeId: "team-100", TeamName: "Team Alpha", UserId: 123, OrganizationId: 789},
		{TeamId: 200, TeamNodeId: "team-200", TeamName: "Team Beta", UserId: 123, OrganizationId: 789},
	}
	mockTeamStore.On("GetUserTeams", ctx, int64(123), int64(789)).Return(teams, nil)

	// Mock team access check - one team has access
	teamEntityIds := []string{
		models.FormatEntityId(models.EntityTypeTeam, "100"),
		models.FormatEntityId(models.EntityTypeTeam, "200"),
	}
	mockNodeStore.On("BatchCheckAccess", ctx, teamEntityIds, nodeId).Return(true, nil)

	hasAccess, err := service.CheckNodeAccess(ctx, userId, nodeUuid, organizationId)

	assert.NoError(t, err)
	assert.True(t, hasAccess)
	mockNodeStore.AssertExpectations(t)
	mockTeamStore.AssertExpectations(t)
}

func TestPermissionService_CheckNodeAccess_NoAccess(t *testing.T) {
	mockNodeStore := new(MockNodeAccessStore)
	mockTeamStore := new(MockTeamStore)
	service := NewPermissionService(mockNodeStore, mockTeamStore)

	ctx := context.Background()
	userId := "123"
	nodeUuid := "node-456"
	organizationId := "789"
	
	userEntityId := models.FormatEntityId(models.EntityTypeUser, userId)
	workspaceEntityId := models.FormatEntityId(models.EntityTypeWorkspace, organizationId)
	nodeId := models.FormatNodeId(nodeUuid)

	// Mock no access at any level
	mockNodeStore.On("HasAccess", ctx, userEntityId, nodeId).Return(false, nil)
	mockNodeStore.On("HasAccess", ctx, workspaceEntityId, nodeId).Return(false, nil)

	// Mock user teams
	teams := []store_postgres.UserTeam{
		{TeamId: 100, TeamNodeId: "team-100", TeamName: "Team Alpha", UserId: 123, OrganizationId: 789},
	}
	mockTeamStore.On("GetUserTeams", ctx, int64(123), int64(789)).Return(teams, nil)

	// Mock team access check - no team has access
	teamEntityIds := []string{
		models.FormatEntityId(models.EntityTypeTeam, "100"),
	}
	mockNodeStore.On("BatchCheckAccess", ctx, teamEntityIds, nodeId).Return(false, nil)

	hasAccess, err := service.CheckNodeAccess(ctx, userId, nodeUuid, organizationId)

	assert.NoError(t, err)
	assert.False(t, hasAccess)
	mockNodeStore.AssertExpectations(t)
	mockTeamStore.AssertExpectations(t)
}

func TestPermissionService_GetAccessibleNodes(t *testing.T) {
	mockNodeStore := new(MockNodeAccessStore)
	mockTeamStore := new(MockTeamStore)
	service := NewPermissionService(mockNodeStore, mockTeamStore)

	ctx := context.Background()
	userId := "123"
	organizationId := "789"
	
	userEntityId := models.FormatEntityId(models.EntityTypeUser, userId)

	// Mock direct user access
	userAccess := []models.NodeAccess{
		{NodeUuid: "user-node-1", EntityId: userEntityId, AccessType: models.AccessTypeOwner},
		{NodeUuid: "user-node-2", EntityId: userEntityId, AccessType: models.AccessTypeShared},
	}
	mockNodeStore.On("GetEntityAccess", ctx, userEntityId).Return(userAccess, nil)

	// Mock workspace nodes
	workspaceNodes := []models.NodeAccess{
		{NodeUuid: "workspace-node-1", AccessType: models.AccessTypeWorkspace},
		{NodeUuid: "workspace-node-2", AccessType: models.AccessTypeWorkspace},
		{NodeUuid: "user-node-1", AccessType: models.AccessTypeWorkspace}, // Duplicate - should be deduplicated
	}
	mockNodeStore.On("GetWorkspaceNodes", ctx, organizationId).Return(workspaceNodes, nil)

	// Mock user teams
	teams := []store_postgres.UserTeam{
		{TeamId: 100, TeamNodeId: "team-100", TeamName: "Team Alpha", UserId: 123, OrganizationId: 789},
	}
	mockTeamStore.On("GetUserTeams", ctx, int64(123), int64(789)).Return(teams, nil)

	// Mock team access
	teamEntityId := models.FormatEntityId(models.EntityTypeTeam, "100")
	teamAccess := []models.NodeAccess{
		{NodeUuid: "team-node-1", EntityId: teamEntityId, AccessType: models.AccessTypeShared},
		{NodeUuid: "user-node-2", EntityId: teamEntityId, AccessType: models.AccessTypeShared}, // Duplicate - should be deduplicated
	}
	mockNodeStore.On("GetEntityAccess", ctx, teamEntityId).Return(teamAccess, nil)

	accessibleNodes, err := service.GetAccessibleNodes(ctx, userId, organizationId)

	assert.NoError(t, err)
	assert.Len(t, accessibleNodes, 5) // Should be deduplicated

	// Convert to map for easier checking
	nodeMap := make(map[string]bool)
	for _, nodeUuid := range accessibleNodes {
		nodeMap[nodeUuid] = true
	}

	// Verify all unique nodes are present
	expectedNodes := []string{"user-node-1", "user-node-2", "workspace-node-1", "workspace-node-2", "team-node-1"}
	for _, expectedNode := range expectedNodes {
		assert.True(t, nodeMap[expectedNode], "Node %s should be accessible", expectedNode)
	}

	mockNodeStore.AssertExpectations(t)
	mockTeamStore.AssertExpectations(t)
}

func TestPermissionService_SetNodePermissions_PrivateScope(t *testing.T) {
	mockNodeStore := new(MockNodeAccessStore)
	service := NewPermissionService(mockNodeStore, nil)

	ctx := context.Background()
	nodeUuid := "node-123"
	ownerId := "owner-456"
	organizationId := "org-789"
	grantedBy := "admin-user"

	req := models.NodeAccessRequest{
		NodeUuid:    nodeUuid,
		AccessScope: models.AccessScopePrivate,
	}

	// Mock current access (has some shared users and workspace access)
	currentAccess := []models.NodeAccess{
		{
			EntityId:    models.FormatEntityId(models.EntityTypeUser, ownerId),
			NodeId:      models.FormatNodeId(nodeUuid),
			AccessType:  models.AccessTypeOwner,
			EntityType:  models.EntityTypeUser,
			EntityRawId: ownerId,
		},
		{
			EntityId:    models.FormatEntityId(models.EntityTypeUser, "shared-user"),
			NodeId:      models.FormatNodeId(nodeUuid),
			AccessType:  models.AccessTypeShared,
			EntityType:  models.EntityTypeUser,
			EntityRawId: "shared-user",
		},
		{
			EntityId:    models.FormatEntityId(models.EntityTypeWorkspace, organizationId),
			NodeId:      models.FormatNodeId(nodeUuid),
			AccessType:  models.AccessTypeWorkspace,
			EntityType:  models.EntityTypeWorkspace,
			EntityRawId: organizationId,
		},
	}
	mockNodeStore.On("GetNodeAccess", ctx, nodeUuid).Return(currentAccess, nil)

	// Should revoke shared user and workspace access, but keep owner
	mockNodeStore.On("RevokeAccess", ctx, models.FormatEntityId(models.EntityTypeUser, "shared-user"), models.FormatNodeId(nodeUuid)).Return(nil)
	mockNodeStore.On("RevokeAccess", ctx, models.FormatEntityId(models.EntityTypeWorkspace, organizationId), models.FormatNodeId(nodeUuid)).Return(nil)

	// No new access should be granted (owner already exists)

	err := service.SetNodePermissions(ctx, nodeUuid, req, ownerId, organizationId, grantedBy)

	assert.NoError(t, err)
	mockNodeStore.AssertExpectations(t)
}

func TestPermissionService_SetNodePermissions_WorkspaceScope(t *testing.T) {
	mockNodeStore := new(MockNodeAccessStore)
	service := NewPermissionService(mockNodeStore, nil)

	ctx := context.Background()
	nodeUuid := "node-123"
	ownerId := "owner-456"
	organizationId := "org-789"
	grantedBy := "admin-user"

	req := models.NodeAccessRequest{
		NodeUuid:    nodeUuid,
		AccessScope: models.AccessScopeWorkspace,
	}

	// Mock current access (only owner)
	currentAccess := []models.NodeAccess{
		{
			EntityId:    models.FormatEntityId(models.EntityTypeUser, ownerId),
			NodeId:      models.FormatNodeId(nodeUuid),
			AccessType:  models.AccessTypeOwner,
			EntityType:  models.EntityTypeUser,
			EntityRawId: ownerId,
		},
	}
	mockNodeStore.On("GetNodeAccess", ctx, nodeUuid).Return(currentAccess, nil)

	// Should grant workspace access - mock the individual GrantAccess method
	mockNodeStore.On("GrantAccess", ctx, mock.MatchedBy(func(access models.NodeAccess) bool {
		return access.EntityType == models.EntityTypeWorkspace &&
			access.AccessType == models.AccessTypeWorkspace &&
			access.EntityRawId == organizationId
	})).Return(nil)

	err := service.SetNodePermissions(ctx, nodeUuid, req, ownerId, organizationId, grantedBy)

	assert.NoError(t, err)
	mockNodeStore.AssertExpectations(t)
}

func TestPermissionService_SetNodePermissions_SharedScope(t *testing.T) {
	mockNodeStore := new(MockNodeAccessStore)
	service := NewPermissionService(mockNodeStore, nil)

	ctx := context.Background()
	nodeUuid := "node-123"
	ownerId := "owner-456"
	organizationId := "org-789"
	grantedBy := "admin-user"

	req := models.NodeAccessRequest{
		NodeUuid:        nodeUuid,
		AccessScope:     models.AccessScopeShared,
		SharedWithUsers: []string{"user-1", "user-2"},
		SharedWithTeams: []string{"team-1"},
	}

	// Mock current access (owner + some old shared users)
	currentAccess := []models.NodeAccess{
		{
			EntityId:    models.FormatEntityId(models.EntityTypeUser, ownerId),
			NodeId:      models.FormatNodeId(nodeUuid),
			AccessType:  models.AccessTypeOwner,
			EntityType:  models.EntityTypeUser,
			EntityRawId: ownerId,
		},
		{
			EntityId:    models.FormatEntityId(models.EntityTypeUser, "old-user"),
			NodeId:      models.FormatNodeId(nodeUuid),
			AccessType:  models.AccessTypeShared,
			EntityType:  models.EntityTypeUser,
			EntityRawId: "old-user",
		},
	}
	mockNodeStore.On("GetNodeAccess", ctx, nodeUuid).Return(currentAccess, nil)

	// Should revoke old shared user
	mockNodeStore.On("RevokeAccess", ctx, models.FormatEntityId(models.EntityTypeUser, "old-user"), models.FormatNodeId(nodeUuid)).Return(nil)

	// Should grant access to new shared users and teams
	mockNodeStore.On("GrantAccess", ctx, mock.MatchedBy(func(access models.NodeAccess) bool {
		return access.EntityType == models.EntityTypeUser &&
			access.AccessType == models.AccessTypeShared &&
			(access.EntityRawId == "user-1" || access.EntityRawId == "user-2")
	})).Return(nil).Twice()

	mockNodeStore.On("GrantAccess", ctx, mock.MatchedBy(func(access models.NodeAccess) bool {
		return access.EntityType == models.EntityTypeTeam &&
			access.AccessType == models.AccessTypeShared &&
			access.EntityRawId == "team-1"
	})).Return(nil)

	err := service.SetNodePermissions(ctx, nodeUuid, req, ownerId, organizationId, grantedBy)

	assert.NoError(t, err)
	mockNodeStore.AssertExpectations(t)
}

func TestPermissionService_GetNodePermissions(t *testing.T) {
	mockNodeStore := new(MockNodeAccessStore)
	service := NewPermissionService(mockNodeStore, nil)

	ctx := context.Background()
	nodeUuid := "node-123"

	// Mock access list with different types
	accessList := []models.NodeAccess{
		{
			NodeUuid:       nodeUuid,
			EntityType:     models.EntityTypeUser,
			EntityRawId:    "owner-123",
			AccessType:     models.AccessTypeOwner,
			OrganizationId: "org-456",
		},
		{
			NodeUuid:    nodeUuid,
			EntityType:  models.EntityTypeUser,
			EntityRawId: "shared-user-1",
			AccessType:  models.AccessTypeShared,
		},
		{
			NodeUuid:    nodeUuid,
			EntityType:  models.EntityTypeUser,
			EntityRawId: "shared-user-2",
			AccessType:  models.AccessTypeShared,
		},
		{
			NodeUuid:    nodeUuid,
			EntityType:  models.EntityTypeTeam,
			EntityRawId: "team-1",
			AccessType:  models.AccessTypeShared,
		},
		{
			NodeUuid:       nodeUuid,
			EntityType:     models.EntityTypeWorkspace,
			EntityRawId:    "org-456",
			AccessType:     models.AccessTypeWorkspace,
			OrganizationId: "org-456",
		},
	}
	mockNodeStore.On("GetNodeAccess", ctx, nodeUuid).Return(accessList, nil)

	permissions, err := service.GetNodePermissions(ctx, nodeUuid)

	assert.NoError(t, err)
	assert.NotNil(t, permissions)
	assert.Equal(t, nodeUuid, permissions.NodeUuid)
	assert.Equal(t, "owner-123", permissions.Owner)
	assert.Equal(t, models.AccessScopeWorkspace, permissions.AccessScope) // Workspace takes precedence
	assert.Equal(t, "org-456", permissions.OrganizationId)
	assert.Contains(t, permissions.SharedWithUsers, "shared-user-1")
	assert.Contains(t, permissions.SharedWithUsers, "shared-user-2")
	assert.Contains(t, permissions.SharedWithTeams, "team-1")
	assert.Len(t, permissions.SharedWithUsers, 2)
	assert.Len(t, permissions.SharedWithTeams, 1)

	mockNodeStore.AssertExpectations(t)
}

func TestPermissionService_GetNodePermissions_SharedScope(t *testing.T) {
	mockNodeStore := new(MockNodeAccessStore)
	service := NewPermissionService(mockNodeStore, nil)

	ctx := context.Background()
	nodeUuid := "node-123"

	// Mock access list with only shared access (no workspace)
	accessList := []models.NodeAccess{
		{
			NodeUuid:    nodeUuid,
			EntityType:  models.EntityTypeUser,
			EntityRawId: "owner-123",
			AccessType:  models.AccessTypeOwner,
		},
		{
			NodeUuid:    nodeUuid,
			EntityType:  models.EntityTypeUser,
			EntityRawId: "shared-user-1",
			AccessType:  models.AccessTypeShared,
		},
		{
			NodeUuid:    nodeUuid,
			EntityType:  models.EntityTypeTeam,
			EntityRawId: "team-1",
			AccessType:  models.AccessTypeShared,
		},
	}
	mockNodeStore.On("GetNodeAccess", ctx, nodeUuid).Return(accessList, nil)

	permissions, err := service.GetNodePermissions(ctx, nodeUuid)

	assert.NoError(t, err)
	assert.NotNil(t, permissions)
	assert.Equal(t, nodeUuid, permissions.NodeUuid)
	assert.Equal(t, "owner-123", permissions.Owner)
	assert.Equal(t, models.AccessScopeShared, permissions.AccessScope) // Should be shared since there are shared users/teams
	assert.Contains(t, permissions.SharedWithUsers, "shared-user-1")
	assert.Contains(t, permissions.SharedWithTeams, "team-1")

	mockNodeStore.AssertExpectations(t)
}

func TestPermissionService_GetNodePermissions_PrivateScope(t *testing.T) {
	mockNodeStore := new(MockNodeAccessStore)
	service := NewPermissionService(mockNodeStore, nil)

	ctx := context.Background()
	nodeUuid := "node-123"

	// Mock access list with only owner
	accessList := []models.NodeAccess{
		{
			NodeUuid:    nodeUuid,
			EntityType:  models.EntityTypeUser,
			EntityRawId: "owner-123",
			AccessType:  models.AccessTypeOwner,
		},
	}
	mockNodeStore.On("GetNodeAccess", ctx, nodeUuid).Return(accessList, nil)

	permissions, err := service.GetNodePermissions(ctx, nodeUuid)

	assert.NoError(t, err)
	assert.NotNil(t, permissions)
	assert.Equal(t, nodeUuid, permissions.NodeUuid)
	assert.Equal(t, "owner-123", permissions.Owner)
	assert.Equal(t, models.AccessScopePrivate, permissions.AccessScope) // Should be private (default)
	assert.Len(t, permissions.SharedWithUsers, 0)
	assert.Len(t, permissions.SharedWithTeams, 0)

	mockNodeStore.AssertExpectations(t)
}