package service

import (
	"context"
	"testing"

	"github.com/pennsieve/account-service/internal/errors"
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

type MockNodeStore struct {
	mock.Mock
}

func (m *MockNodeStore) GetById(ctx context.Context, uuid string) (models.DynamoDBNode, error) {
	args := m.Called(ctx, uuid)
	return args.Get(0).(models.DynamoDBNode), args.Error(1)
}

func (m *MockNodeStore) Get(ctx context.Context, userId string) ([]models.DynamoDBNode, error) {
	args := m.Called(ctx, userId)
	return args.Get(0).([]models.DynamoDBNode), args.Error(1)
}

func (m *MockNodeStore) GetByAccount(ctx context.Context, accountUuid string) ([]models.DynamoDBNode, error) {
	args := m.Called(ctx, accountUuid)
	return args.Get(0).([]models.DynamoDBNode), args.Error(1)
}

func (m *MockNodeStore) Put(ctx context.Context, node models.DynamoDBNode) error {
	args := m.Called(ctx, node)
	return args.Error(0)
}

func (m *MockNodeStore) UpdateStatus(ctx context.Context, uuid, status string) error {
	args := m.Called(ctx, uuid, status)
	return args.Error(0)
}

func (m *MockNodeStore) Delete(ctx context.Context, uuid string) error {
	args := m.Called(ctx, uuid)
	return args.Error(0)
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

// Tests for organization-independent nodes
func TestPermissionService_CheckNodeAccess_OrganizationIndependentNode(t *testing.T) {
	mockNodeStore := new(MockNodeAccessStore)
	service := NewPermissionService(mockNodeStore, nil)

	ctx := context.Background()
	userId := "user-123"
	nodeUuid := "node-456"
	organizationId := "" // Organization-independent node

	userEntityId := models.FormatEntityId(models.EntityTypeUser, userId)
	nodeId := models.FormatNodeId(nodeUuid)

	// Mock direct user access - this is the only way to access organization-independent nodes
	mockNodeStore.On("HasAccess", ctx, userEntityId, nodeId).Return(true, nil)

	hasAccess, err := service.CheckNodeAccess(ctx, userId, nodeUuid, organizationId)

	assert.NoError(t, err)
	assert.True(t, hasAccess)
	mockNodeStore.AssertExpectations(t)
}

func TestPermissionService_CheckNodeAccess_OrganizationIndependentNode_NoAccess(t *testing.T) {
	mockNodeStore := new(MockNodeAccessStore)
	service := NewPermissionService(mockNodeStore, nil)

	ctx := context.Background()
	userId := "user-123"
	nodeUuid := "node-456"
	organizationId := "" // Organization-independent node

	userEntityId := models.FormatEntityId(models.EntityTypeUser, userId)
	nodeId := models.FormatNodeId(nodeUuid)

	// Mock no direct user access - should stop here for organization-independent nodes
	mockNodeStore.On("HasAccess", ctx, userEntityId, nodeId).Return(false, nil)

	hasAccess, err := service.CheckNodeAccess(ctx, userId, nodeUuid, organizationId)

	assert.NoError(t, err)
	assert.False(t, hasAccess)
	mockNodeStore.AssertExpectations(t)
}

func TestPermissionService_SetNodePermissions_OrganizationIndependentValidation(t *testing.T) {
	mockNodeStore := new(MockNodeAccessStore)
	service := NewPermissionService(mockNodeStore, nil)

	ctx := context.Background()
	nodeUuid := "node-123"
	ownerId := "owner-456"
	organizationId := "" // Organization-independent
	grantedBy := "admin-user"

	// Test that workspace scope is rejected for organization-independent nodes
	req := models.NodeAccessRequest{
		NodeUuid:    nodeUuid,
		AccessScope: models.AccessScopeWorkspace,
	}

	err := service.SetNodePermissions(ctx, nodeUuid, req, ownerId, organizationId, grantedBy)

	// Should return validation error
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "organization-independent")
}

func TestPermissionService_SetNodePermissions_OrganizationIndependentSharedRejected(t *testing.T) {
	mockNodeStore := new(MockNodeAccessStore)
	service := NewPermissionService(mockNodeStore, nil)

	ctx := context.Background()
	nodeUuid := "node-123"
	ownerId := "owner-456"
	organizationId := "" // Organization-independent
	grantedBy := "admin-user"

	// Test that shared scope is rejected for organization-independent nodes
	req := models.NodeAccessRequest{
		NodeUuid:        nodeUuid,
		AccessScope:     models.AccessScopeShared,
		SharedWithUsers: []string{"user-1"},
	}

	err := service.SetNodePermissions(ctx, nodeUuid, req, ownerId, organizationId, grantedBy)

	// Should return validation error
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "organization-independent")
}

func TestPermissionService_SetNodePermissions_OrganizationIndependentPrivateAllowed(t *testing.T) {
	mockNodeStore := new(MockNodeAccessStore)
	service := NewPermissionService(mockNodeStore, nil)

	ctx := context.Background()
	nodeUuid := "node-123"
	ownerId := "owner-456"
	organizationId := "" // Organization-independent
	grantedBy := "admin-user"

	req := models.NodeAccessRequest{
		NodeUuid:    nodeUuid,
		AccessScope: models.AccessScopePrivate, // This should be allowed
	}

	// Mock current access (only owner)
	currentAccess := []models.NodeAccess{
		{
			EntityId:       models.FormatEntityId(models.EntityTypeUser, ownerId),
			NodeId:         models.FormatNodeId(nodeUuid),
			AccessType:     models.AccessTypeOwner,
			EntityType:     models.EntityTypeUser,
			EntityRawId:    ownerId,
			OrganizationId: "", // Organization-independent
		},
	}
	mockNodeStore.On("GetNodeAccess", ctx, nodeUuid).Return(currentAccess, nil)

	// No changes needed for private scope with only owner

	err := service.SetNodePermissions(ctx, nodeUuid, req, ownerId, organizationId, grantedBy)

	assert.NoError(t, err)
	mockNodeStore.AssertExpectations(t)
}

// Tests for AttachNodeToOrganization
func TestPermissionService_AttachNodeToOrganization_Success(t *testing.T) {
	mockNodeStore := new(MockNodeAccessStore)
	service := NewPermissionService(mockNodeStore, nil)

	ctx := context.Background()
	nodeUuid := "node-123"
	organizationId := "org-456"
	userId := "user-789"

	userEntityId := models.FormatEntityId(models.EntityTypeUser, userId)
	nodeId := models.FormatNodeId(nodeUuid)

	// Mock user has access to the node
	mockNodeStore.On("HasAccess", ctx, userEntityId, nodeId).Return(true, nil)

	// Mock current access (organization-independent owner)
	currentAccess := []models.NodeAccess{
		{
			EntityId:       userEntityId,
			NodeId:         nodeId,
			EntityType:     models.EntityTypeUser,
			EntityRawId:    userId,
			AccessType:     models.AccessTypeOwner,
			OrganizationId: "", // Organization-independent
		},
	}
	mockNodeStore.On("GetNodeAccess", ctx, nodeUuid).Return(currentAccess, nil)

	// Mock removing old access and granting new access
	mockNodeStore.On("RevokeAccess", ctx, userEntityId, nodeId).Return(nil)
	mockNodeStore.On("GrantAccess", ctx, mock.MatchedBy(func(access models.NodeAccess) bool {
		return access.EntityId == userEntityId &&
			access.OrganizationId == organizationId &&
			access.AccessType == models.AccessTypeOwner
	})).Return(nil)

	err := service.AttachNodeToOrganization(ctx, nodeUuid, organizationId, userId)

	assert.NoError(t, err)
	mockNodeStore.AssertExpectations(t)
}

func TestPermissionService_AttachNodeToOrganization_UserNotOwner(t *testing.T) {
	mockNodeStore := new(MockNodeAccessStore)
	service := NewPermissionService(mockNodeStore, nil)

	ctx := context.Background()
	nodeUuid := "node-123"
	organizationId := "org-456"
	userId := "user-789"

	userEntityId := models.FormatEntityId(models.EntityTypeUser, userId)
	nodeId := models.FormatNodeId(nodeUuid)

	// Mock user does not have access to the node
	mockNodeStore.On("HasAccess", ctx, userEntityId, nodeId).Return(false, nil)

	err := service.AttachNodeToOrganization(ctx, nodeUuid, organizationId, userId)

	assert.Error(t, err)
	assert.Equal(t, errors.ErrForbidden, err)
	mockNodeStore.AssertExpectations(t)
}

func TestPermissionService_AttachNodeToOrganization_NodeAlreadyHasOrganization(t *testing.T) {
	mockNodeStore := new(MockNodeAccessStore)
	service := NewPermissionService(mockNodeStore, nil)

	ctx := context.Background()
	nodeUuid := "node-123"
	organizationId := "org-456"
	userId := "user-789"

	userEntityId := models.FormatEntityId(models.EntityTypeUser, userId)
	nodeId := models.FormatNodeId(nodeUuid)

	// Mock user has access to the node
	mockNodeStore.On("HasAccess", ctx, userEntityId, nodeId).Return(true, nil)

	// Mock current access (already has organization)
	currentAccess := []models.NodeAccess{
		{
			EntityId:       userEntityId,
			NodeId:         nodeId,
			EntityType:     models.EntityTypeUser,
			EntityRawId:    userId,
			AccessType:     models.AccessTypeOwner,
			OrganizationId: "existing-org", // Already has organization
		},
	}
	mockNodeStore.On("GetNodeAccess", ctx, nodeUuid).Return(currentAccess, nil)

	err := service.AttachNodeToOrganization(ctx, nodeUuid, organizationId, userId)

	assert.Error(t, err)
	assert.Equal(t, models.ErrCannotAttachNodeWithExistingOrganization, err)
	mockNodeStore.AssertExpectations(t)
}

// Tests for DetachNodeFromOrganization
func TestPermissionService_DetachNodeFromOrganization_Success(t *testing.T) {
	mockNodeStore := new(MockNodeAccessStore)
	service := NewPermissionService(mockNodeStore, nil)

	ctx := context.Background()
	nodeUuid := "node-123"
	userId := "user-789"

	nodeId := models.FormatNodeId(nodeUuid)
	userEntityId := models.FormatEntityId(models.EntityTypeUser, userId)

	// Mock current access (has organization and multiple access entries)
	currentAccess := []models.NodeAccess{
		{
			EntityId:       userEntityId,
			NodeId:         nodeId,
			EntityType:     models.EntityTypeUser,
			EntityRawId:    userId,
			AccessType:     models.AccessTypeOwner,
			OrganizationId: "org-456",
		},
		{
			EntityId:       models.FormatEntityId(models.EntityTypeWorkspace, "org-456"),
			NodeId:         nodeId,
			EntityType:     models.EntityTypeWorkspace,
			EntityRawId:    "org-456",
			AccessType:     models.AccessTypeWorkspace,
			OrganizationId: "org-456",
		},
		{
			EntityId:       models.FormatEntityId(models.EntityTypeUser, "shared-user"),
			NodeId:         nodeId,
			EntityType:     models.EntityTypeUser,
			EntityRawId:    "shared-user",
			AccessType:     models.AccessTypeShared,
			OrganizationId: "org-456",
		},
	}
	mockNodeStore.On("GetNodeAccess", ctx, nodeUuid).Return(currentAccess, nil)

	// Mock removing all current access entries
	for _, access := range currentAccess {
		mockNodeStore.On("RevokeAccess", ctx, access.EntityId, nodeId).Return(nil)
	}

	// Mock granting new organization-independent owner access
	mockNodeStore.On("GrantAccess", ctx, mock.MatchedBy(func(access models.NodeAccess) bool {
		return access.EntityId == userEntityId &&
			access.OrganizationId == "" &&
			access.AccessType == models.AccessTypeOwner &&
			access.EntityRawId == userId
	})).Return(nil)

	err := service.DetachNodeFromOrganization(ctx, nodeUuid, userId)

	assert.NoError(t, err)
	mockNodeStore.AssertExpectations(t)
}

func TestPermissionService_DetachNodeFromOrganization_NodeAlreadyIndependent(t *testing.T) {
	mockNodeStore := new(MockNodeAccessStore)
	service := NewPermissionService(mockNodeStore, nil)

	ctx := context.Background()
	nodeUuid := "node-123"
	userId := "user-789"

	nodeId := models.FormatNodeId(nodeUuid)
	userEntityId := models.FormatEntityId(models.EntityTypeUser, userId)

	// Mock current access (already organization-independent)
	currentAccess := []models.NodeAccess{
		{
			EntityId:       userEntityId,
			NodeId:         nodeId,
			EntityType:     models.EntityTypeUser,
			EntityRawId:    userId,
			AccessType:     models.AccessTypeOwner,
			OrganizationId: "", // Already organization-independent
		},
	}
	mockNodeStore.On("GetNodeAccess", ctx, nodeUuid).Return(currentAccess, nil)

	err := service.DetachNodeFromOrganization(ctx, nodeUuid, userId)

	assert.Error(t, err)
	assert.Equal(t, models.ErrOrganizationIndependentNodeCannotBeShared, err)
	mockNodeStore.AssertExpectations(t)
}

func TestPermissionService_DetachNodeFromOrganization_NoOwnerFound(t *testing.T) {
	mockNodeStore := new(MockNodeAccessStore)
	service := NewPermissionService(mockNodeStore, nil)

	ctx := context.Background()
	nodeUuid := "node-123"
	userId := "user-789"

	// Mock current access (no owner found - invalid state)
	currentAccess := []models.NodeAccess{
		{
			EntityId:       models.FormatEntityId(models.EntityTypeUser, "other-user"),
			NodeId:         models.FormatNodeId(nodeUuid),
			EntityType:     models.EntityTypeUser,
			EntityRawId:    "other-user",
			AccessType:     models.AccessTypeShared,
			OrganizationId: "org-456",
		},
	}
	mockNodeStore.On("GetNodeAccess", ctx, nodeUuid).Return(currentAccess, nil)

	err := service.DetachNodeFromOrganization(ctx, nodeUuid, userId)

	assert.Error(t, err)
	assert.Equal(t, errors.ErrNotFound, err)
	mockNodeStore.AssertExpectations(t)
}

func TestPermissionService_GetNodePermissions_AutoRepair_Success(t *testing.T) {
	mockNodeAccessStore := new(MockNodeAccessStore)
	mockNodeStore := new(MockNodeStore)
	service := NewPermissionService(mockNodeAccessStore, nil)
	service.SetNodeStore(mockNodeStore)

	ctx := context.Background()
	nodeUuid := "node-456"
	ownerId := "user-123"
	organizationId := "org-789"

	// Mock: GetNodeAccess returns empty list (no access entries - this triggers auto-repair)
	mockNodeAccessStore.On("GetNodeAccess", ctx, nodeUuid).Return([]models.NodeAccess{}, nil)

	// Mock: GetById returns the actual node with owner information
	node := models.DynamoDBNode{
		Uuid:           nodeUuid,
		UserId:         ownerId,
		OrganizationId: organizationId,
		Name:           "Test Node",
		Status:         "Enabled",
	}
	mockNodeStore.On("GetById", ctx, nodeUuid).Return(node, nil)

	// Mock: GrantAccess will be called to restore owner access
	expectedOwnerAccess := models.NodeAccess{
		EntityId:       models.FormatEntityId(models.EntityTypeUser, ownerId),
		NodeId:         models.FormatNodeId(nodeUuid),
		EntityType:     models.EntityTypeUser,
		EntityRawId:    ownerId,
		NodeUuid:       nodeUuid,
		AccessType:     models.AccessTypeOwner,
		OrganizationId: organizationId,
		GrantedBy:      ownerId,
	}
	mockNodeAccessStore.On("GrantAccess", ctx, expectedOwnerAccess).Return(nil)

	// Call GetNodePermissions - should trigger auto-repair
	permissions, err := service.GetNodePermissions(ctx, nodeUuid)

	// Verify results
	assert.NoError(t, err)
	assert.NotNil(t, permissions)
	assert.Equal(t, nodeUuid, permissions.NodeUuid)
	assert.Equal(t, ownerId, permissions.Owner)
	assert.Equal(t, models.AccessScopePrivate, permissions.AccessScope)
	assert.Equal(t, organizationId, permissions.OrganizationId)
	assert.Empty(t, permissions.SharedWithUsers)
	assert.Empty(t, permissions.SharedWithTeams)

	// Verify all mocks were called as expected
	mockNodeAccessStore.AssertExpectations(t)
	mockNodeStore.AssertExpectations(t)
}

func TestPermissionService_GetNodePermissions_AutoRepair_NodeNotFound(t *testing.T) {
	mockNodeAccessStore := new(MockNodeAccessStore)
	mockNodeStore := new(MockNodeStore)
	service := NewPermissionService(mockNodeAccessStore, nil)
	service.SetNodeStore(mockNodeStore)

	ctx := context.Background()
	nodeUuid := "node-456"

	// Mock: GetNodeAccess returns empty list (no access entries)
	mockNodeAccessStore.On("GetNodeAccess", ctx, nodeUuid).Return([]models.NodeAccess{}, nil)

	// Mock: GetById returns error (node not found)
	mockNodeStore.On("GetById", ctx, nodeUuid).Return(models.DynamoDBNode{}, errors.ErrNotFound)

	// Call GetNodePermissions - should not attempt repair
	permissions, err := service.GetNodePermissions(ctx, nodeUuid)

	// Verify results - should return empty permissions without error
	assert.NoError(t, err)
	assert.NotNil(t, permissions)
	assert.Equal(t, nodeUuid, permissions.NodeUuid)
	assert.Empty(t, permissions.Owner) // No owner found, no repair performed
	assert.Equal(t, models.AccessScopePrivate, permissions.AccessScope)

	// Verify mocks were called as expected (GrantAccess should NOT be called)
	mockNodeAccessStore.AssertExpectations(t)
	mockNodeStore.AssertExpectations(t)
}

func TestPermissionService_GetNodePermissions_AutoRepair_GrantAccessFails(t *testing.T) {
	mockNodeAccessStore := new(MockNodeAccessStore)
	mockNodeStore := new(MockNodeStore)
	service := NewPermissionService(mockNodeAccessStore, nil)
	service.SetNodeStore(mockNodeStore)

	ctx := context.Background()
	nodeUuid := "node-456"
	ownerId := "user-123"
	organizationId := "org-789"

	// Mock: GetNodeAccess returns empty list (no access entries)
	mockNodeAccessStore.On("GetNodeAccess", ctx, nodeUuid).Return([]models.NodeAccess{}, nil)

	// Mock: GetById returns the actual node
	node := models.DynamoDBNode{
		Uuid:           nodeUuid,
		UserId:         ownerId,
		OrganizationId: organizationId,
		Name:           "Test Node",
		Status:         "Enabled",
	}
	mockNodeStore.On("GetById", ctx, nodeUuid).Return(node, nil)

	// Mock: GrantAccess fails
	expectedOwnerAccess := models.NodeAccess{
		EntityId:       models.FormatEntityId(models.EntityTypeUser, ownerId),
		NodeId:         models.FormatNodeId(nodeUuid),
		EntityType:     models.EntityTypeUser,
		EntityRawId:    ownerId,
		NodeUuid:       nodeUuid,
		AccessType:     models.AccessTypeOwner,
		OrganizationId: organizationId,
		GrantedBy:      ownerId,
	}
	mockNodeAccessStore.On("GrantAccess", ctx, expectedOwnerAccess).Return(errors.ErrDynamoDB)

	// Call GetNodePermissions - should attempt repair but gracefully handle failure
	permissions, err := service.GetNodePermissions(ctx, nodeUuid)

	// Verify results - should not fail even if repair fails
	assert.NoError(t, err)
	assert.NotNil(t, permissions)
	assert.Equal(t, nodeUuid, permissions.NodeUuid)
	assert.Empty(t, permissions.Owner) // Repair failed, so owner is still empty
	assert.Equal(t, models.AccessScopePrivate, permissions.AccessScope)

	// Verify all mocks were called
	mockNodeAccessStore.AssertExpectations(t)
	mockNodeStore.AssertExpectations(t)
}

func TestPermissionService_GetNodePermissions_AutoRepair_NoNodeStore(t *testing.T) {
	mockNodeAccessStore := new(MockNodeAccessStore)
	service := NewPermissionService(mockNodeAccessStore, nil)
	// Note: NodeStore is nil, so auto-repair should be skipped

	ctx := context.Background()
	nodeUuid := "node-456"

	// Mock: GetNodeAccess returns empty list (no access entries)
	mockNodeAccessStore.On("GetNodeAccess", ctx, nodeUuid).Return([]models.NodeAccess{}, nil)

	// Call GetNodePermissions - should NOT trigger auto-repair (no NodeStore)
	permissions, err := service.GetNodePermissions(ctx, nodeUuid)

	// Verify results
	assert.NoError(t, err)
	assert.NotNil(t, permissions)
	assert.Equal(t, nodeUuid, permissions.NodeUuid)
	assert.Empty(t, permissions.Owner) // No repair attempted
	assert.Equal(t, models.AccessScopePrivate, permissions.AccessScope)

	// Verify mocks - NodeStore methods should NOT be called
	mockNodeAccessStore.AssertExpectations(t)
}

func TestPermissionService_GetNodePermissions_AutoRepair_OwnerAlreadyExists(t *testing.T) {
	mockNodeAccessStore := new(MockNodeAccessStore)
	mockNodeStore := new(MockNodeStore)
	service := NewPermissionService(mockNodeAccessStore, nil)
	service.SetNodeStore(mockNodeStore)

	ctx := context.Background()
	nodeUuid := "node-456"
	ownerId := "user-123"
	organizationId := "org-789"

	// Mock: GetNodeAccess returns existing owner access (no repair needed)
	existingAccess := []models.NodeAccess{
		{
			EntityId:       models.FormatEntityId(models.EntityTypeUser, ownerId),
			NodeId:         models.FormatNodeId(nodeUuid),
			EntityType:     models.EntityTypeUser,
			EntityRawId:    ownerId,
			NodeUuid:       nodeUuid,
			AccessType:     models.AccessTypeOwner,
			OrganizationId: organizationId,
		},
	}
	mockNodeAccessStore.On("GetNodeAccess", ctx, nodeUuid).Return(existingAccess, nil)

	// Call GetNodePermissions - should NOT trigger auto-repair (owner already exists)
	permissions, err := service.GetNodePermissions(ctx, nodeUuid)

	// Verify results
	assert.NoError(t, err)
	assert.NotNil(t, permissions)
	assert.Equal(t, nodeUuid, permissions.NodeUuid)
	assert.Equal(t, ownerId, permissions.Owner)
	assert.Equal(t, models.AccessScopePrivate, permissions.AccessScope)

	// Verify mocks - NodeStore methods should NOT be called
	mockNodeAccessStore.AssertExpectations(t)
	// mockNodeStore should not be called at all since owner already exists
}

// MockOrganizationStore for testing validation logic
type MockOrganizationStore struct {
	mock.Mock
}

func (m *MockOrganizationStore) CheckUserOrganizationAccess(ctx context.Context, userId, organizationId int64) (bool, error) {
	args := m.Called(ctx, userId, organizationId)
	return args.Bool(0), args.Error(1)
}

func (m *MockOrganizationStore) GetUserPermissionBit(ctx context.Context, userId, organizationId int64) (int, error) {
	args := m.Called(ctx, userId, organizationId)
	return args.Int(0), args.Error(1)
}

func (m *MockOrganizationStore) CheckUserIsOrganizationAdmin(ctx context.Context, userId, organizationId int64) (bool, error) {
	args := m.Called(ctx, userId, organizationId)
	return args.Bool(0), args.Error(1)
}

func (m *MockOrganizationStore) CheckUserExists(ctx context.Context, userId int64) (bool, error) {
	args := m.Called(ctx, userId)
	return args.Bool(0), args.Error(1)
}

// Tests for validation of non-existent users/teams when granting permissions
func TestPermissionService_SetNodePermissions_NonExistentUsersValidation(t *testing.T) {
	mockNodeStore := new(MockNodeAccessStore)
	mockTeamStore := new(MockTeamStore)
	mockOrgStore := new(MockOrganizationStore)
	service := NewPermissionService(mockNodeStore, mockTeamStore)
	service.SetOrganizationStore(mockOrgStore)

	ctx := context.Background()
	nodeUuid := "node-123"
	ownerId := "owner-456"
	organizationId := "org-789"
	grantedBy := "admin-user"

	req := models.NodeAccessRequest{
		NodeUuid:        nodeUuid,
		AccessScope:     models.AccessScopeShared,
		SharedWithUsers: []string{"123", "456", "999"}, // 999 doesn't exist
		SharedWithTeams: []string{"10"},
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

	// Mock user validation - users 123 and 456 exist, but 999 does not
	mockOrgStore.On("CheckUserExists", ctx, int64(123)).Return(true, nil)
	mockOrgStore.On("CheckUserExists", ctx, int64(456)).Return(true, nil)
	mockOrgStore.On("CheckUserExists", ctx, int64(999)).Return(false, nil)

	err := service.SetNodePermissions(ctx, nodeUuid, req, ownerId, organizationId, grantedBy)

	// Should return error for non-existent user
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "user 999 does not exist")
	mockNodeStore.AssertExpectations(t)
	mockOrgStore.AssertExpectations(t)
}

func TestPermissionService_SetNodePermissions_NonExistentTeamsValidation(t *testing.T) {
	mockNodeStore := new(MockNodeAccessStore)
	mockTeamStore := new(MockTeamStore)
	mockOrgStore := new(MockOrganizationStore)
	service := NewPermissionService(mockNodeStore, mockTeamStore)
	service.SetOrganizationStore(mockOrgStore)

	ctx := context.Background()
	nodeUuid := "node-123"
	ownerId := "owner-456"
	organizationId := "org-789"
	grantedBy := "admin-user"

	req := models.NodeAccessRequest{
		NodeUuid:        nodeUuid,
		AccessScope:     models.AccessScopeShared,
		SharedWithUsers: []string{"123"},
		SharedWithTeams: []string{"10", "20", "99"}, // 99 doesn't exist
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

	// Mock user validation - user 123 exists
	mockOrgStore.On("CheckUserExists", ctx, int64(123)).Return(true, nil)

	// Mock team validation - teams 10 and 20 exist, but 99 does not
	team10 := &store_postgres.Team{Id: 10, Name: "Team 10", NodeId: "team-10", OrganizationId: 789}
	team20 := &store_postgres.Team{Id: 20, Name: "Team 20", NodeId: "team-20", OrganizationId: 789}
	mockTeamStore.On("GetTeamById", ctx, int64(10)).Return(team10, nil)
	mockTeamStore.On("GetTeamById", ctx, int64(20)).Return(team20, nil)
	mockTeamStore.On("GetTeamById", ctx, int64(99)).Return(nil, nil) // Team doesn't exist

	err := service.SetNodePermissions(ctx, nodeUuid, req, ownerId, organizationId, grantedBy)

	// Should return error for non-existent team
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "team 99 does not exist")
	mockNodeStore.AssertExpectations(t)
	mockOrgStore.AssertExpectations(t)
	mockTeamStore.AssertExpectations(t)
}

func TestPermissionService_SetNodePermissions_InvalidUserIdFormat(t *testing.T) {
	mockNodeStore := new(MockNodeAccessStore)
	mockTeamStore := new(MockTeamStore)
	mockOrgStore := new(MockOrganizationStore)
	service := NewPermissionService(mockNodeStore, mockTeamStore)
	service.SetOrganizationStore(mockOrgStore)

	ctx := context.Background()
	nodeUuid := "node-123"
	ownerId := "owner-456"
	organizationId := "org-789"
	grantedBy := "admin-user"

	req := models.NodeAccessRequest{
		NodeUuid:        nodeUuid,
		AccessScope:     models.AccessScopeShared,
		SharedWithUsers: []string{"invalid-user-id"}, // Invalid format
		SharedWithTeams: []string{},
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

	err := service.SetNodePermissions(ctx, nodeUuid, req, ownerId, organizationId, grantedBy)

	// Should return error for invalid user ID format
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid user ID format: invalid-user-id")
	mockNodeStore.AssertExpectations(t)
}

func TestPermissionService_SetNodePermissions_InvalidTeamIdFormat(t *testing.T) {
	mockNodeStore := new(MockNodeAccessStore)
	mockTeamStore := new(MockTeamStore)
	mockOrgStore := new(MockOrganizationStore)
	service := NewPermissionService(mockNodeStore, mockTeamStore)
	service.SetOrganizationStore(mockOrgStore)

	ctx := context.Background()
	nodeUuid := "node-123"
	ownerId := "owner-456"
	organizationId := "org-789"
	grantedBy := "admin-user"

	req := models.NodeAccessRequest{
		NodeUuid:        nodeUuid,
		AccessScope:     models.AccessScopeShared,
		SharedWithUsers: []string{},
		SharedWithTeams: []string{"invalid-team-id"}, // Invalid format
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

	err := service.SetNodePermissions(ctx, nodeUuid, req, ownerId, organizationId, grantedBy)

	// Should return error for invalid team ID format
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid team ID format: invalid-team-id")
	mockNodeStore.AssertExpectations(t)
}

func TestPermissionService_SetNodePermissions_UserValidationWithoutOrgStore(t *testing.T) {
	mockNodeStore := new(MockNodeAccessStore)
	mockTeamStore := new(MockTeamStore)
	service := NewPermissionService(mockNodeStore, mockTeamStore)
	// Note: No organization store set, validation should be skipped

	ctx := context.Background()
	nodeUuid := "node-123"
	ownerId := "owner-456"
	organizationId := "org-789"
	grantedBy := "admin-user"

	req := models.NodeAccessRequest{
		NodeUuid:        nodeUuid,
		AccessScope:     models.AccessScopeShared,
		SharedWithUsers: []string{"123", "999"}, // 999 may not exist but validation is skipped
		SharedWithTeams: []string{},
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

	// Should grant access to both users since validation is skipped
	mockNodeStore.On("GrantAccess", ctx, mock.MatchedBy(func(access models.NodeAccess) bool {
		return access.EntityType == models.EntityTypeUser &&
			access.AccessType == models.AccessTypeShared &&
			(access.EntityRawId == "123" || access.EntityRawId == "999")
	})).Return(nil).Twice()

	err := service.SetNodePermissions(ctx, nodeUuid, req, ownerId, organizationId, grantedBy)

	// Should succeed without validation
	assert.NoError(t, err)
	mockNodeStore.AssertExpectations(t)
}

func TestPermissionService_SetNodePermissions_TeamValidationWithoutTeamStore(t *testing.T) {
	mockNodeStore := new(MockNodeAccessStore)
	mockOrgStore := new(MockOrganizationStore)
	service := NewPermissionService(mockNodeStore, nil) // No team store
	service.SetOrganizationStore(mockOrgStore)

	ctx := context.Background()
	nodeUuid := "node-123"
	ownerId := "owner-456"
	organizationId := "org-789"
	grantedBy := "admin-user"

	req := models.NodeAccessRequest{
		NodeUuid:        nodeUuid,
		AccessScope:     models.AccessScopeShared,
		SharedWithUsers: []string{},
		SharedWithTeams: []string{"99"}, // Team may not exist but validation is skipped
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

	// Should grant access to team since validation is skipped
	mockNodeStore.On("GrantAccess", ctx, mock.MatchedBy(func(access models.NodeAccess) bool {
		return access.EntityType == models.EntityTypeTeam &&
			access.AccessType == models.AccessTypeShared &&
			access.EntityRawId == "99"
	})).Return(nil)

	err := service.SetNodePermissions(ctx, nodeUuid, req, ownerId, organizationId, grantedBy)

	// Should succeed without validation
	assert.NoError(t, err)
	mockNodeStore.AssertExpectations(t)
}

// Tests for cleanup of stale permissions when getting permissions
func TestPermissionService_GetNodePermissions_CleanupStaleUsers(t *testing.T) {
	mockNodeAccessStore := new(MockNodeAccessStore)
	mockTeamStore := new(MockTeamStore)
	mockOrgStore := new(MockOrganizationStore)
	service := NewPermissionService(mockNodeAccessStore, mockTeamStore)
	service.SetOrganizationStore(mockOrgStore)

	ctx := context.Background()
	nodeUuid := "node-123"

	// Mock access list with valid and invalid users
	accessList := []models.NodeAccess{
		{
			NodeUuid:    nodeUuid,
			EntityType:  models.EntityTypeUser,
			EntityRawId: "owner-123",
			AccessType:  models.AccessTypeOwner,
			EntityId:    models.FormatEntityId(models.EntityTypeUser, "owner-123"),
		},
		{
			NodeUuid:    nodeUuid,
			EntityType:  models.EntityTypeUser,
			EntityRawId: "456", // This user exists
			AccessType:  models.AccessTypeShared,
			EntityId:    models.FormatEntityId(models.EntityTypeUser, "456"),
		},
		{
			NodeUuid:    nodeUuid,
			EntityType:  models.EntityTypeUser,
			EntityRawId: "999", // This user no longer exists
			AccessType:  models.AccessTypeShared,
			EntityId:    models.FormatEntityId(models.EntityTypeUser, "999"),
		},
	}
	mockNodeAccessStore.On("GetNodeAccess", ctx, nodeUuid).Return(accessList, nil)

	// Mock user existence checks - owner validation is skipped, others are checked
	mockOrgStore.On("CheckUserExists", ctx, int64(456)).Return(true, nil)  // valid user exists
	mockOrgStore.On("CheckUserExists", ctx, int64(999)).Return(false, nil) // deleted user doesn't exist

	// Mock removal of stale permission
	nodeId := models.FormatNodeId(nodeUuid)
	deletedUserEntityId := models.FormatEntityId(models.EntityTypeUser, "999")
	mockNodeAccessStore.On("RevokeAccess", ctx, deletedUserEntityId, nodeId).Return(nil)

	permissions, err := service.GetNodePermissions(ctx, nodeUuid)

	assert.NoError(t, err)
	assert.NotNil(t, permissions)
	assert.Equal(t, nodeUuid, permissions.NodeUuid)
	assert.Equal(t, "owner-123", permissions.Owner)
	assert.Equal(t, models.AccessScopeShared, permissions.AccessScope)
	
	// Should only contain valid user, not the deleted one
	assert.Contains(t, permissions.SharedWithUsers, "456")
	assert.NotContains(t, permissions.SharedWithUsers, "999")
	assert.Len(t, permissions.SharedWithUsers, 1)

	mockNodeAccessStore.AssertExpectations(t)
	mockOrgStore.AssertExpectations(t)
}

func TestPermissionService_GetNodePermissions_CleanupStaleTeams(t *testing.T) {
	mockNodeAccessStore := new(MockNodeAccessStore)
	mockTeamStore := new(MockTeamStore)
	mockOrgStore := new(MockOrganizationStore)
	service := NewPermissionService(mockNodeAccessStore, mockTeamStore)
	service.SetOrganizationStore(mockOrgStore)
	service.SetNodeStore(nil) // Disable auto-repair for this test

	ctx := context.Background()
	nodeUuid := "node-123"

	// Mock access list with valid and invalid teams
	accessList := []models.NodeAccess{
		{
			NodeUuid:    nodeUuid,
			EntityType:  models.EntityTypeUser,
			EntityRawId: "owner-123",
			AccessType:  models.AccessTypeOwner,
			EntityId:    models.FormatEntityId(models.EntityTypeUser, "owner-123"),
		},
		{
			NodeUuid:    nodeUuid,
			EntityType:  models.EntityTypeTeam,
			EntityRawId: "10", // This team exists
			AccessType:  models.AccessTypeShared,
			EntityId:    models.FormatEntityId(models.EntityTypeTeam, "10"),
		},
		{
			NodeUuid:    nodeUuid,
			EntityType:  models.EntityTypeTeam,
			EntityRawId: "99", // This team no longer exists
			AccessType:  models.AccessTypeShared,
			EntityId:    models.FormatEntityId(models.EntityTypeTeam, "99"),
		},
	}
	mockNodeAccessStore.On("GetNodeAccess", ctx, nodeUuid).Return(accessList, nil)

	// Mock team existence checks
	validTeam := &store_postgres.Team{Id: 10, Name: "Valid Team", NodeId: "team-10", OrganizationId: 789}
	mockTeamStore.On("GetTeamById", ctx, int64(10)).Return(validTeam, nil)   // valid team exists
	mockTeamStore.On("GetTeamById", ctx, int64(99)).Return(nil, nil)         // deleted team doesn't exist

	// Mock removal of stale permission
	nodeId := models.FormatNodeId(nodeUuid)
	deletedTeamEntityId := models.FormatEntityId(models.EntityTypeTeam, "99")
	mockNodeAccessStore.On("RevokeAccess", ctx, deletedTeamEntityId, nodeId).Return(nil)

	permissions, err := service.GetNodePermissions(ctx, nodeUuid)

	assert.NoError(t, err)
	assert.NotNil(t, permissions)
	assert.Equal(t, nodeUuid, permissions.NodeUuid)
	assert.Equal(t, "owner-123", permissions.Owner)
	assert.Equal(t, models.AccessScopeShared, permissions.AccessScope)
	
	// Should only contain valid team, not the deleted one
	assert.Contains(t, permissions.SharedWithTeams, "10")
	assert.NotContains(t, permissions.SharedWithTeams, "99")
	assert.Len(t, permissions.SharedWithTeams, 1)

	mockNodeAccessStore.AssertExpectations(t)
	mockTeamStore.AssertExpectations(t)
}

func TestPermissionService_GetNodePermissions_CleanupMixedStalePermissions(t *testing.T) {
	mockNodeAccessStore := new(MockNodeAccessStore)
	mockTeamStore := new(MockTeamStore)
	mockOrgStore := new(MockOrganizationStore)
	service := NewPermissionService(mockNodeAccessStore, mockTeamStore)
	service.SetOrganizationStore(mockOrgStore)
	service.SetNodeStore(nil) // Disable auto-repair for this test

	ctx := context.Background()
	nodeUuid := "node-123"

	// Mock access list with mix of valid and invalid users and teams
	accessList := []models.NodeAccess{
		{
			NodeUuid:    nodeUuid,
			EntityType:  models.EntityTypeUser,
			EntityRawId: "owner-123",
			AccessType:  models.AccessTypeOwner,
			EntityId:    models.FormatEntityId(models.EntityTypeUser, "owner-123"),
		},
		{
			NodeUuid:    nodeUuid,
			EntityType:  models.EntityTypeUser,
			EntityRawId: "456",
			AccessType:  models.AccessTypeShared,
			EntityId:    models.FormatEntityId(models.EntityTypeUser, "456"),
		},
		{
			NodeUuid:    nodeUuid,
			EntityType:  models.EntityTypeUser,
			EntityRawId: "789",
			AccessType:  models.AccessTypeShared,
			EntityId:    models.FormatEntityId(models.EntityTypeUser, "789"),
		},
		{
			NodeUuid:    nodeUuid,
			EntityType:  models.EntityTypeTeam,
			EntityRawId: "10",
			AccessType:  models.AccessTypeShared,
			EntityId:    models.FormatEntityId(models.EntityTypeTeam, "10"),
		},
		{
			NodeUuid:    nodeUuid,
			EntityType:  models.EntityTypeTeam,
			EntityRawId: "20",
			AccessType:  models.AccessTypeShared,
			EntityId:    models.FormatEntityId(models.EntityTypeTeam, "20"),
		},
	}
	mockNodeAccessStore.On("GetNodeAccess", ctx, nodeUuid).Return(accessList, nil)

	// Mock user existence checks
	mockOrgStore.On("CheckUserExists", ctx, int64(456)).Return(true, nil)  // valid user exists
	mockOrgStore.On("CheckUserExists", ctx, int64(789)).Return(false, nil) // deleted user doesn't exist

	// Mock team existence checks
	validTeam := &store_postgres.Team{Id: 10, Name: "Valid Team", NodeId: "team-10", OrganizationId: 789}
	mockTeamStore.On("GetTeamById", ctx, int64(10)).Return(validTeam, nil)   // valid team exists
	mockTeamStore.On("GetTeamById", ctx, int64(20)).Return(nil, nil)         // deleted team doesn't exist

	// Mock removal of stale permissions
	nodeId := models.FormatNodeId(nodeUuid)
	deletedUserEntityId := models.FormatEntityId(models.EntityTypeUser, "789")
	deletedTeamEntityId := models.FormatEntityId(models.EntityTypeTeam, "20")
	mockNodeAccessStore.On("RevokeAccess", ctx, deletedUserEntityId, nodeId).Return(nil)
	mockNodeAccessStore.On("RevokeAccess", ctx, deletedTeamEntityId, nodeId).Return(nil)

	permissions, err := service.GetNodePermissions(ctx, nodeUuid)

	assert.NoError(t, err)
	assert.NotNil(t, permissions)
	assert.Equal(t, nodeUuid, permissions.NodeUuid)
	assert.Equal(t, "owner-123", permissions.Owner)
	assert.Equal(t, models.AccessScopeShared, permissions.AccessScope)
	
	// Should only contain valid entities, not the deleted ones
	assert.Contains(t, permissions.SharedWithUsers, "456")
	assert.NotContains(t, permissions.SharedWithUsers, "789")
	assert.Len(t, permissions.SharedWithUsers, 1)
	
	assert.Contains(t, permissions.SharedWithTeams, "10")
	assert.NotContains(t, permissions.SharedWithTeams, "20")
	assert.Len(t, permissions.SharedWithTeams, 1)

	mockNodeAccessStore.AssertExpectations(t)
	mockOrgStore.AssertExpectations(t)
	mockTeamStore.AssertExpectations(t)
}

func TestPermissionService_GetNodePermissions_CleanupAccessScopeRecalculation(t *testing.T) {
	mockNodeAccessStore := new(MockNodeAccessStore)
	mockTeamStore := new(MockTeamStore)
	mockOrgStore := new(MockOrganizationStore)
	service := NewPermissionService(mockNodeAccessStore, mockTeamStore)
	service.SetOrganizationStore(mockOrgStore)
	service.SetNodeStore(nil) // Disable auto-repair for this test

	ctx := context.Background()
	nodeUuid := "node-123"

	// Mock access list where all shared permissions are stale (users/teams deleted)
	accessList := []models.NodeAccess{
		{
			NodeUuid:    nodeUuid,
			EntityType:  models.EntityTypeUser,
			EntityRawId: "owner-123",
			AccessType:  models.AccessTypeOwner,
			EntityId:    models.FormatEntityId(models.EntityTypeUser, "owner-123"),
		},
		{
			NodeUuid:    nodeUuid,
			EntityType:  models.EntityTypeUser,
			EntityRawId: "456", // This user no longer exists
			AccessType:  models.AccessTypeShared,
			EntityId:    models.FormatEntityId(models.EntityTypeUser, "456"),
		},
		{
			NodeUuid:    nodeUuid,
			EntityType:  models.EntityTypeTeam,
			EntityRawId: "10", // This team no longer exists
			AccessType:  models.AccessTypeShared,
			EntityId:    models.FormatEntityId(models.EntityTypeTeam, "10"),
		},
	}
	mockNodeAccessStore.On("GetNodeAccess", ctx, nodeUuid).Return(accessList, nil)

	// Mock that both user and team no longer exist
	mockOrgStore.On("CheckUserExists", ctx, int64(456)).Return(false, nil) // deleted user doesn't exist
	mockTeamStore.On("GetTeamById", ctx, int64(10)).Return(nil, nil)       // deleted team doesn't exist

	// Mock removal of both stale permissions
	nodeId := models.FormatNodeId(nodeUuid)
	deletedUserEntityId := models.FormatEntityId(models.EntityTypeUser, "456")
	deletedTeamEntityId := models.FormatEntityId(models.EntityTypeTeam, "10")
	mockNodeAccessStore.On("RevokeAccess", ctx, deletedUserEntityId, nodeId).Return(nil)
	mockNodeAccessStore.On("RevokeAccess", ctx, deletedTeamEntityId, nodeId).Return(nil)

	permissions, err := service.GetNodePermissions(ctx, nodeUuid)

	assert.NoError(t, err)
	assert.NotNil(t, permissions)
	assert.Equal(t, nodeUuid, permissions.NodeUuid)
	assert.Equal(t, "owner-123", permissions.Owner)
	
	// Access scope should be recalculated to Private since all shared permissions were removed
	assert.Equal(t, models.AccessScopePrivate, permissions.AccessScope)
	assert.Len(t, permissions.SharedWithUsers, 0)
	assert.Len(t, permissions.SharedWithTeams, 0)

	mockNodeAccessStore.AssertExpectations(t)
	mockOrgStore.AssertExpectations(t)
	mockTeamStore.AssertExpectations(t)
}

func TestPermissionService_GetNodePermissions_CleanupErrorHandling(t *testing.T) {
	mockNodeAccessStore := new(MockNodeAccessStore)
	mockTeamStore := new(MockTeamStore)
	mockOrgStore := new(MockOrganizationStore)
	service := NewPermissionService(mockNodeAccessStore, mockTeamStore)
	service.SetOrganizationStore(mockOrgStore)
	service.SetNodeStore(nil) // Disable auto-repair for this test

	ctx := context.Background()
	nodeUuid := "node-123"

	// Mock access list with user and team
	accessList := []models.NodeAccess{
		{
			NodeUuid:    nodeUuid,
			EntityType:  models.EntityTypeUser,
			EntityRawId: "owner-123",
			AccessType:  models.AccessTypeOwner,
			EntityId:    models.FormatEntityId(models.EntityTypeUser, "owner-123"),
		},
		{
			NodeUuid:    nodeUuid,
			EntityType:  models.EntityTypeUser,
			EntityRawId: "456",
			AccessType:  models.AccessTypeShared,
			EntityId:    models.FormatEntityId(models.EntityTypeUser, "456"),
		},
	}
	mockNodeAccessStore.On("GetNodeAccess", ctx, nodeUuid).Return(accessList, nil)

	// Mock database error when checking user existence
	mockOrgStore.On("CheckUserExists", ctx, int64(456)).Return(false, errors.ErrDynamoDB)

	permissions, err := service.GetNodePermissions(ctx, nodeUuid)

	// Should not fail even if user validation has DB error
	assert.NoError(t, err)
	assert.NotNil(t, permissions)
	assert.Equal(t, nodeUuid, permissions.NodeUuid)
	assert.Equal(t, "owner-123", permissions.Owner)
	
	// User should still be present since DB error prevents cleanup
	assert.Contains(t, permissions.SharedWithUsers, "456")
	assert.Len(t, permissions.SharedWithUsers, 1)

	mockNodeAccessStore.AssertExpectations(t)
	mockOrgStore.AssertExpectations(t)
}

func TestPermissionService_GetNodePermissions_NoCleanupWithoutStores(t *testing.T) {
	mockNodeAccessStore := new(MockNodeAccessStore)
	service := NewPermissionService(mockNodeAccessStore, nil) // No team or org store

	ctx := context.Background()
	nodeUuid := "node-123"

	// Mock access list with users and teams that might not exist
	accessList := []models.NodeAccess{
		{
			NodeUuid:    nodeUuid,
			EntityType:  models.EntityTypeUser,
			EntityRawId: "owner-123",
			AccessType:  models.AccessTypeOwner,
			EntityId:    models.FormatEntityId(models.EntityTypeUser, "owner-123"),
		},
		{
			NodeUuid:    nodeUuid,
			EntityType:  models.EntityTypeUser,
			EntityRawId: "456",
			AccessType:  models.AccessTypeShared,
			EntityId:    models.FormatEntityId(models.EntityTypeUser, "456"),
		},
		{
			NodeUuid:    nodeUuid,
			EntityType:  models.EntityTypeTeam,
			EntityRawId: "10",
			AccessType:  models.AccessTypeShared,
			EntityId:    models.FormatEntityId(models.EntityTypeTeam, "10"),
		},
	}
	mockNodeAccessStore.On("GetNodeAccess", ctx, nodeUuid).Return(accessList, nil)

	permissions, err := service.GetNodePermissions(ctx, nodeUuid)

	// Should succeed without any cleanup since stores are not available
	assert.NoError(t, err)
	assert.NotNil(t, permissions)
	assert.Equal(t, nodeUuid, permissions.NodeUuid)
	assert.Equal(t, "owner-123", permissions.Owner)
	assert.Equal(t, models.AccessScopeShared, permissions.AccessScope)
	
	// All permissions should remain since no cleanup can occur
	assert.Contains(t, permissions.SharedWithUsers, "456")
	assert.Len(t, permissions.SharedWithUsers, 1)
	assert.Contains(t, permissions.SharedWithTeams, "10")
	assert.Len(t, permissions.SharedWithTeams, 1)

	mockNodeAccessStore.AssertExpectations(t)
}