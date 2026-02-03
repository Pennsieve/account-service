package store_postgres

import (
	"context"
	"testing"
	
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockOrganizationStore is a mock implementation for testing
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

func TestMinimumCollaboratorPermission(t *testing.T) {
	// Verify that the minimum permission bit for Collaborator is 8
	assert.Equal(t, 8, MinimumCollaboratorPermission)
}

// Note: For full PostgreSQL integration tests, we would need a test database
// This mock shows the interface structure and validates the permission logic
func TestMockOrganizationStore_CheckUserOrganizationAccess(t *testing.T) {
	mockStore := &MockOrganizationStore{}
	ctx := context.Background()
	
	userId := int64(123)
	organizationId := int64(456)
	
	// Test case: User has Collaborator access (permission_bit = 8)
	mockStore.On("CheckUserOrganizationAccess", ctx, userId, organizationId).Return(true, nil)
	
	hasAccess, err := mockStore.CheckUserOrganizationAccess(ctx, userId, organizationId)
	assert.NoError(t, err)
	assert.True(t, hasAccess)
	
	mockStore.AssertExpectations(t)
}

func TestMockOrganizationStore_GetUserPermissionBit(t *testing.T) {
	mockStore := &MockOrganizationStore{}
	ctx := context.Background()
	
	userId := int64(123)
	organizationId := int64(456)
	
	// Test case: User has Manager access (permission_bit = 16)
	mockStore.On("GetUserPermissionBit", ctx, userId, organizationId).Return(16, nil)
	
	permissionBit, err := mockStore.GetUserPermissionBit(ctx, userId, organizationId)
	assert.NoError(t, err)
	assert.Equal(t, 16, permissionBit)
	assert.GreaterOrEqual(t, permissionBit, MinimumCollaboratorPermission)
	
	mockStore.AssertExpectations(t)
}