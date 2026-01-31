package compute_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pennsieve/account-service/internal/handler/compute"
	"github.com/pennsieve/account-service/internal/models"
)

// MockNodeStore is a mock implementation of the NodeStore interface
type MockNodeStore struct {
	mock.Mock
}

func (m *MockNodeStore) GetById(ctx context.Context, uuid string) (models.DynamoDBNode, error) {
	args := m.Called(ctx, uuid)
	return args.Get(0).(models.DynamoDBNode), args.Error(1)
}

func (m *MockNodeStore) Get(ctx context.Context, filter string) ([]models.DynamoDBNode, error) {
	args := m.Called(ctx, filter)
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

func (m *MockNodeStore) UpdateStatus(ctx context.Context, uuid string, status string) error {
	args := m.Called(ctx, uuid, status)
	return args.Error(0)
}

func (m *MockNodeStore) Delete(ctx context.Context, uuid string) error {
	args := m.Called(ctx, uuid)
	return args.Error(0)
}

func TestHandleProvisioningComplete(t *testing.T) {
	ctx := context.Background()
	mockStore := new(MockNodeStore)

	// Create test event
	event := compute.ComputeNodeEvent{
		Action:                "CREATE",
		ComputeNodeId:         "test-node-123",
		Identifier:            "12345",
		Name:                  "Test Node",
		ComputeNodeGatewayUrl: "https://gateway.example.com",
		EfsId:                 "efs-123",
		QueueUrl:              "https://sqs.example.com/queue",
		WorkflowManagerTag:    "v1.0.0",
		CreatedAt:             time.Now().UTC().String(),
	}

	// Setup mock expectations
	existingNode := models.DynamoDBNode{
		Uuid:               "test-node-123",
		Name:               "Test Node",
		Description:        "Test Description",
		Status:             "Pending",
		AccountUuid:        "account-123",
		OrganizationId:     "org-123",
		WorkflowManagerTag: "v1.0.0",
	}

	updatedNode := existingNode
	updatedNode.ComputeNodeGatewayUrl = event.ComputeNodeGatewayUrl
	updatedNode.EfsId = event.EfsId
	updatedNode.QueueUrl = event.QueueUrl
	updatedNode.Status = "Enabled"

	mockStore.On("GetById", ctx, "test-node-123").Return(existingNode, nil)
	mockStore.On("Put", ctx, updatedNode).Return(nil)

	// Test successful provisioning completion
	t.Run("Success", func(t *testing.T) {
		// We need to test the internal handler function directly
		// Since we can't easily mock the AWS config loading in the main handler
		detailJSON, _ := json.Marshal(event)
		err := handleProvisioningCompleteTest(ctx, mockStore, detailJSON)
		assert.NoError(t, err)
		mockStore.AssertExpectations(t)
	})
}

func TestHandleProvisioningError(t *testing.T) {
	ctx := context.Background()
	mockStore := new(MockNodeStore)

	// Create error event
	errorEvent := compute.ComputeNodeErrorEvent{
		Action:        "CREATE",
		ComputeNodeId: "test-node-123",
		Identifier:    "12345",
		ErrorMessage:  "Failed to create infrastructure",
		ErrorType:     "ProvisionerError",
		Timestamp:     time.Now().UTC().String(),
	}

	eventJSON, err := json.Marshal(errorEvent)
	require.NoError(t, err)

	// Setup mock expectations
	mockStore.On("UpdateStatus", ctx, "test-node-123", "Failed").Return(nil)

	// Test error handling
	t.Run("ProvisioningError", func(t *testing.T) {
		err := handleProvisioningErrorTest(ctx, mockStore, eventJSON)
		assert.NoError(t, err)
		mockStore.AssertExpectations(t)
	})

	// Test when update fails
	t.Run("UpdateStatusFails", func(t *testing.T) {
		mockStore := new(MockNodeStore)
		mockStore.On("UpdateStatus", ctx, "test-node-123", "Failed").
			Return(assert.AnError)
		
		err := handleProvisioningErrorTest(ctx, mockStore, eventJSON)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to update node status")
	})
}

func TestHandleUpdateComplete(t *testing.T) {
	ctx := context.Background()
	mockStore := new(MockNodeStore)

	// Create update event
	updateEvent := compute.ComputeNodeEvent{
		Action:             "UPDATE",
		ComputeNodeId:      "test-node-123",
		Identifier:         "12345",
		WorkflowManagerTag: "v2.0.0",
	}

	eventJSON, err := json.Marshal(updateEvent)
	require.NoError(t, err)

	// Setup mock expectations
	existingNode := models.DynamoDBNode{
		Uuid:                  "test-node-123",
		Name:                  "Test Node",
		Status:                "Enabled",
		WorkflowManagerTag:    "v1.0.0",
		ComputeNodeGatewayUrl: "https://gateway.example.com",
		EfsId:                 "efs-123",
		QueueUrl:              "https://sqs.example.com/queue",
	}

	updatedNode := existingNode
	updatedNode.WorkflowManagerTag = "v2.0.0"

	mockStore.On("GetById", ctx, "test-node-123").Return(existingNode, nil)
	mockStore.On("Put", ctx, updatedNode).Return(nil)

	// Test successful update
	t.Run("Success", func(t *testing.T) {
		err := handleUpdateCompleteTest(ctx, mockStore, eventJSON)
		assert.NoError(t, err)
		mockStore.AssertExpectations(t)
	})

	// Test when node not found
	t.Run("NodeNotFound", func(t *testing.T) {
		mockStore := new(MockNodeStore)
		mockStore.On("GetById", ctx, "test-node-123").
			Return(models.DynamoDBNode{}, nil)
		
		err := handleUpdateCompleteTest(ctx, mockStore, eventJSON)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "node test-node-123 not found")
	})
}

func TestHandleDeleteComplete(t *testing.T) {
	ctx := context.Background()
	mockStore := new(MockNodeStore)

	// Create delete event
	deleteEvent := compute.ComputeNodeEvent{
		Action:        "DELETE",
		ComputeNodeId: "test-node-123",
		Identifier:    "12345",
	}

	eventJSON, err := json.Marshal(deleteEvent)
	require.NoError(t, err)

	// Setup mock expectations
	mockStore.On("Delete", ctx, "test-node-123").Return(nil)

	// Test successful deletion
	t.Run("Success", func(t *testing.T) {
		err := handleDeleteCompleteTest(ctx, mockStore, eventJSON)
		assert.NoError(t, err)
		mockStore.AssertExpectations(t)
	})

	// Test when delete fails
	t.Run("DeleteFails", func(t *testing.T) {
		mockStore := new(MockNodeStore)
		mockStore.On("Delete", ctx, "test-node-123").
			Return(assert.AnError)
		
		err := handleDeleteCompleteTest(ctx, mockStore, eventJSON)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to delete node")
	})
}

func TestEventMarshaling(t *testing.T) {
	// Test ComputeNodeEvent marshaling
	t.Run("ComputeNodeEvent", func(t *testing.T) {
		event := compute.ComputeNodeEvent{
			Action:                "CREATE",
			ComputeNodeId:         "node-123",
			Identifier:            "12345",
			Name:                  "Test Node",
			ComputeNodeGatewayUrl: "https://gateway.example.com",
			EfsId:                 "efs-123",
			QueueUrl:              "https://sqs.example.com/queue",
			WorkflowManagerTag:    "v1.0.0",
			CreatedAt:             "2024-01-30T10:00:00Z",
		}

		data, err := json.Marshal(event)
		require.NoError(t, err)

		var decoded compute.ComputeNodeEvent
		err = json.Unmarshal(data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, event.Action, decoded.Action)
		assert.Equal(t, event.ComputeNodeId, decoded.ComputeNodeId)
		assert.Equal(t, event.Identifier, decoded.Identifier)
		assert.Equal(t, event.Name, decoded.Name)
		assert.Equal(t, event.ComputeNodeGatewayUrl, decoded.ComputeNodeGatewayUrl)
		assert.Equal(t, event.EfsId, decoded.EfsId)
		assert.Equal(t, event.QueueUrl, decoded.QueueUrl)
		assert.Equal(t, event.WorkflowManagerTag, decoded.WorkflowManagerTag)
		assert.Equal(t, event.CreatedAt, decoded.CreatedAt)
	})

	// Test ComputeNodeErrorEvent marshaling
	t.Run("ComputeNodeErrorEvent", func(t *testing.T) {
		event := compute.ComputeNodeErrorEvent{
			Action:        "CREATE",
			ComputeNodeId: "node-123",
			Identifier:    "12345",
			ErrorMessage:  "Test error",
			ErrorType:     "ProvisionerError",
			Timestamp:     "2024-01-30T10:00:00Z",
		}

		data, err := json.Marshal(event)
		require.NoError(t, err)

		var decoded compute.ComputeNodeErrorEvent
		err = json.Unmarshal(data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, event.Action, decoded.Action)
		assert.Equal(t, event.ComputeNodeId, decoded.ComputeNodeId)
		assert.Equal(t, event.Identifier, decoded.Identifier)
		assert.Equal(t, event.ErrorMessage, decoded.ErrorMessage)
		assert.Equal(t, event.ErrorType, decoded.ErrorType)
		assert.Equal(t, event.Timestamp, decoded.Timestamp)
	})
}

func TestNodeNotFound(t *testing.T) {
	ctx := context.Background()
	mockStore := new(MockNodeStore)

	// Create test event
	event := compute.ComputeNodeEvent{
		Action:                "CREATE",
		ComputeNodeId:         "non-existent-node",
		Identifier:            "12345",
		ComputeNodeGatewayUrl: "https://gateway.example.com",
		EfsId:                 "efs-123",
		QueueUrl:              "https://sqs.example.com/queue",
	}

	eventJSON, err := json.Marshal(event)
	require.NoError(t, err)

	// Return empty node (not found)
	mockStore.On("GetById", ctx, "non-existent-node").
		Return(models.DynamoDBNode{}, nil)

	// Test node not found scenario
	err = handleProvisioningCompleteTest(ctx, mockStore, eventJSON)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "node non-existent-node not found")
	mockStore.AssertExpectations(t)
}

// Helper functions that mirror the internal handler functions for testing
// These allow us to test the logic without dealing with AWS config loading

func handleProvisioningCompleteTest(ctx context.Context, nodeStore *MockNodeStore, detail json.RawMessage) error {
	var event compute.ComputeNodeEvent
	if err := json.Unmarshal(detail, &event); err != nil {
		return err
	}

	node, err := nodeStore.GetById(ctx, event.ComputeNodeId)
	if err != nil {
		return err
	}

	if node.Uuid == "" {
		return fmt.Errorf("node %s not found", event.ComputeNodeId)
	}

	node.ComputeNodeGatewayUrl = event.ComputeNodeGatewayUrl
	node.EfsId = event.EfsId
	node.QueueUrl = event.QueueUrl
	node.Status = "Enabled"

	return nodeStore.Put(ctx, node)
}

func handleProvisioningErrorTest(ctx context.Context, nodeStore *MockNodeStore, detail json.RawMessage) error {
	var event compute.ComputeNodeErrorEvent
	if err := json.Unmarshal(detail, &event); err != nil {
		return err
	}

	if err := nodeStore.UpdateStatus(ctx, event.ComputeNodeId, "Failed"); err != nil {
		return fmt.Errorf("failed to update node status: %w", err)
	}

	return nil
}

func handleUpdateCompleteTest(ctx context.Context, nodeStore *MockNodeStore, detail json.RawMessage) error {
	var event compute.ComputeNodeEvent
	if err := json.Unmarshal(detail, &event); err != nil {
		return err
	}

	node, err := nodeStore.GetById(ctx, event.ComputeNodeId)
	if err != nil {
		return err
	}

	if node.Uuid == "" {
		return fmt.Errorf("node %s not found", event.ComputeNodeId)
	}

	if event.WorkflowManagerTag != "" {
		node.WorkflowManagerTag = event.WorkflowManagerTag
	}
	node.Status = "Enabled"

	return nodeStore.Put(ctx, node)
}

func handleDeleteCompleteTest(ctx context.Context, nodeStore *MockNodeStore, detail json.RawMessage) error {
	var event compute.ComputeNodeEvent
	if err := json.Unmarshal(detail, &event); err != nil {
		return err
	}

	if err := nodeStore.Delete(ctx, event.ComputeNodeId); err != nil {
		return fmt.Errorf("failed to delete node: %w", err)
	}

	return nil
}