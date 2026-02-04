package checkaccess

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/pennsieve/account-service/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock for NodeAccessStore
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

func TestCheckUserNodeAccessHandler_Success(t *testing.T) {
	// Skip this test in unit test mode since it requires full AWS environment
	// This test requires DynamoDB tables to be configured
	t.Skip("Skipping integration test that requires full AWS environment")
	
	ctx := context.Background()
	
	request := CheckUserNodeAccessRequest{
		UserNodeId:     "N:user:test-user-123",
		NodeUuid:       "node-uuid-456",
		OrganizationId: "N:organization:org-789",
	}
	
	// Note: In real tests, you would need to set up the environment
	// and mock the permission service dependencies
	// For now, this is a basic structure test
	
	response, err := CheckUserNodeAccessHandler(ctx, request)
	
	// The handler should not return an error even if access is false
	assert.NoError(t, err)
	assert.Equal(t, request.NodeUuid, response.NodeUuid)
	assert.Equal(t, request.UserNodeId, response.UserNodeId)
	assert.Equal(t, request.OrganizationId, response.OrganizationId)
}

func TestCheckUserNodeAccessHandler_MissingUserNodeId(t *testing.T) {
	ctx := context.Background()
	
	request := CheckUserNodeAccessRequest{
		UserNodeId:     "", // Missing
		NodeUuid:       "node-uuid-456",
		OrganizationId: "N:organization:org-789",
	}
	
	response, err := CheckUserNodeAccessHandler(ctx, request)
	
	// Should not error, but should return false access
	assert.NoError(t, err)
	assert.False(t, response.HasAccess)
	assert.Equal(t, request.NodeUuid, response.NodeUuid)
}

func TestCheckUserNodeAccessHandler_MissingNodeUuid(t *testing.T) {
	ctx := context.Background()
	
	request := CheckUserNodeAccessRequest{
		UserNodeId:     "N:user:test-user-123",
		NodeUuid:       "", // Missing
		OrganizationId: "N:organization:org-789",
	}
	
	response, err := CheckUserNodeAccessHandler(ctx, request)
	
	// Should not error, but should return false access
	assert.NoError(t, err)
	assert.False(t, response.HasAccess)
	assert.Equal(t, request.UserNodeId, response.UserNodeId)
}

func TestLambdaHandler(t *testing.T) {
	// Skip this test in unit test mode since it requires full AWS environment
	// This test requires DynamoDB tables to be configured
	t.Skip("Skipping integration test that requires full AWS environment")
	
	ctx := context.Background()
	
	request := CheckUserNodeAccessRequest{
		UserNodeId:     "N:user:test-user-123",
		NodeUuid:       "node-uuid-456",
		OrganizationId: "N:organization:org-789",
	}
	
	// Marshal request to JSON
	requestJSON, err := json.Marshal(request)
	assert.NoError(t, err)
	
	// Test Lambda handler wrapper
	response, err := LambdaHandler(ctx, requestJSON)
	
	// Should handle the request (even if it returns false due to missing setup)
	assert.NoError(t, err)
	assert.Equal(t, request.NodeUuid, response.NodeUuid)
	assert.Equal(t, request.UserNodeId, response.UserNodeId)
}

func TestLambdaHandler_InvalidJSON(t *testing.T) {
	ctx := context.Background()
	
	// Invalid JSON
	invalidJSON := json.RawMessage(`{"invalid json}`)
	
	_, err := LambdaHandler(ctx, invalidJSON)
	
	// Should return an error for invalid JSON
	assert.Error(t, err)
}

func TestEnrichAccessDetails(t *testing.T) {
	ctx := context.Background()
	mockStore := new(MockNodeAccessStore)
	
	response := &CheckUserNodeAccessResponse{
		HasAccess:      true,
		NodeUuid:       "node-123",
		UserNodeId:     "N:user:user-456",
		OrganizationId: "N:organization:org-789",
	}
	
	nodeId := models.FormatNodeId(response.NodeUuid)
	userEntityId := models.FormatEntityId(models.EntityTypeUser, response.UserNodeId)
	
	// Mock direct user access
	mockStore.On("HasAccess", ctx, userEntityId, nodeId).Return(true, nil)
	
	// Mock getting node access details
	accessList := []models.NodeAccess{
		{
			EntityId:   userEntityId,
			NodeId:     nodeId,
			AccessType: models.AccessTypeOwner,
		},
	}
	mockStore.On("GetNodeAccess", ctx, response.NodeUuid).Return(accessList, nil)
	
	err := enrichAccessDetails(ctx, response, mockStore, nil, nil)
	
	assert.NoError(t, err)
	assert.Equal(t, string(models.AccessTypeOwner), response.AccessType)
	assert.Equal(t, "direct", response.AccessSource)
	
	mockStore.AssertExpectations(t)
}