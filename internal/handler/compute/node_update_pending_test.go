package compute

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/pennsieve/account-service/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memNodeStore keeps nodes in memory, for the event handlers.
type memNodeStore struct{ nodes map[string]models.DynamoDBNode }

func (m *memNodeStore) GetById(_ context.Context, id string) (models.DynamoDBNode, error) {
	return m.nodes[id], nil
}
func (m *memNodeStore) Get(context.Context, string) ([]models.DynamoDBNode, error) { return nil, nil }
func (m *memNodeStore) GetByAccount(context.Context, string) ([]models.DynamoDBNode, error) {
	return nil, nil
}
func (m *memNodeStore) GetAllEnabled(context.Context) ([]models.DynamoDBNode, error) { return nil, nil }
func (m *memNodeStore) Put(_ context.Context, n models.DynamoDBNode) error {
	m.nodes[n.Uuid] = n
	return nil
}
func (m *memNodeStore) UpdateStatus(_ context.Context, id, status string) error {
	n := m.nodes[id]
	n.Status = status
	m.nodes[id] = n
	return nil
}
func (m *memNodeStore) UpdateHealthStatus(context.Context, string, string, string) error { return nil }
func (m *memNodeStore) Delete(_ context.Context, id string) error {
	delete(m.nodes, id)
	return nil
}

func updatingNode() models.DynamoDBNode {
	return models.DynamoDBNode{
		Uuid:                       "node-1",
		Status:                     "Updating",
		ProvisionerImage:           "pennsieve/compute-node-aws-provisioner-v2",
		ProvisionerImageTag:        "v1.18.0",
		PendingProvisionerImage:    "pennsieve/compute-node-aws-provisioner-v2",
		PendingProvisionerImageTag: "v1.19.0",
	}
}

func TestUpdateCompleteAppliesThePendingVersion(t *testing.T) {
	store := &memNodeStore{nodes: map[string]models.DynamoDBNode{"node-1": updatingNode()}}
	detail, _ := json.Marshal(ComputeNodeEvent{Action: "UPDATE", ComputeNodeId: "node-1"})

	require.NoError(t, handleUpdateComplete(context.Background(), store, detail))

	n := store.nodes["node-1"]
	assert.Equal(t, "Enabled", n.Status)
	assert.Equal(t, "v1.19.0", n.ProvisionerImageTag)
	assert.Empty(t, n.PendingProvisionerImageTag)
	assert.Empty(t, n.PendingProvisionerImage)
}

func TestUpdateCompleteWithoutPendingKeepsTheVersion(t *testing.T) {
	node := updatingNode()
	node.PendingProvisionerImage, node.PendingProvisionerImageTag = "", ""
	store := &memNodeStore{nodes: map[string]models.DynamoDBNode{"node-1": node}}
	detail, _ := json.Marshal(ComputeNodeEvent{Action: "UPDATE", ComputeNodeId: "node-1"})

	require.NoError(t, handleUpdateComplete(context.Background(), store, detail))
	assert.Equal(t, "v1.18.0", store.nodes["node-1"].ProvisionerImageTag)
}

func TestUpdateErrorKeepsTheRunningVersion(t *testing.T) {
	store := &memNodeStore{nodes: map[string]models.DynamoDBNode{"node-1": updatingNode()}}
	detail, _ := json.Marshal(ComputeNodeErrorEvent{Action: "UPDATE", ComputeNodeId: "node-1", ErrorMessage: "boom"})

	require.NoError(t, handleProvisioningError(context.Background(), store, detail))

	n := store.nodes["node-1"]
	assert.Equal(t, "Failed", n.Status)
	assert.Equal(t, "v1.18.0", n.ProvisionerImageTag)
	assert.Empty(t, n.PendingProvisionerImageTag)
}
