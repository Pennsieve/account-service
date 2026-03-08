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

func (m *MockOrganizationStore) CheckUserIsOrganizationAdmin(ctx context.Context, userId, organizationId int64) (bool, error) {
	args := m.Called(ctx, userId, organizationId)
	return args.Bool(0), args.Error(1)
}

func (m *MockOrganizationStore) CheckUserExistsByNodeId(ctx context.Context, nodeId string) (bool, error) {
	args := m.Called(ctx, nodeId)
	return args.Bool(0), args.Error(1)
}

func (m *MockOrganizationStore) GetOrganizationIdByNodeId(ctx context.Context, nodeId string) (int64, error) {
	args := m.Called(ctx, nodeId)
	return args.Get(0).(int64), args.Error(1)
}

func TestMinimumAdminPermission(t *testing.T) {
	assert.Equal(t, 16, MinimumAdminPermission)
}
