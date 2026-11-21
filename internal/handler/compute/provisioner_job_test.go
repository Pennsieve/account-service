package compute

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func heldNode(jobId string, startedAt time.Time) models.DynamoDBNode {
	n := updatingNode()
	n.ProvisionerJobId = jobId
	n.ProvisionerJobAction = "UPDATE"
	n.ProvisionerJobStartedAt = startedAt.Unix()
	return n
}

// Two jobs for one node at once corrupted its Terraform state (dev
// 48b8ca51, 2026-11-16): a second is refused with 409 until the first ends.
func TestStartProvisionerJob(t *testing.T) {
	t.Setenv("ENV", "dev")
	ctx := context.Background()
	node := updatingNode()
	node.Status = "Enabled"
	store := &memNodeStore{nodes: map[string]models.DynamoDBNode{"node-1": node}}

	first := store.nodes["node-1"]
	first.Status = "Updating"
	require.NoError(t, startProvisionerJob(ctx, store, &first, "UPDATE"))
	assert.NotEmpty(t, first.ProvisionerJobId)
	assert.Equal(t, "UPDATE", first.ProvisionerJobAction)
	assert.Equal(t, first.ProvisionerJobId, store.nodes["node-1"].ProvisionerJobId)

	second := store.nodes["node-1"]
	err := startProvisionerJob(ctx, store, &second, "DELETE")
	assert.ErrorIs(t, err, store_dynamodb.ErrProvisionerJobRunning)
	assert.Equal(t, http.StatusConflict, provisionerJobRefused("H", err).StatusCode)
	assert.Equal(t, first.ProvisionerJobId, store.nodes["node-1"].ProvisionerJobId)

	// Another failure is the store's: 500.
	assert.Equal(t, http.StatusInternalServerError, provisionerJobRefused("H", assert.AnError).StatusCode)
}

// A job whose task stopped without reporting holds the node only until
// provisionerJobTimeout.
func TestStartProvisionerJobTakesOverACrashedJob(t *testing.T) {
	t.Setenv("ENV", "dev")
	store := &memNodeStore{nodes: map[string]models.DynamoDBNode{
		"node-1": heldNode("crashed", time.Now().Add(-provisionerJobTimeout-time.Minute)),
	}}
	n := store.nodes["node-1"]
	require.NoError(t, startProvisionerJob(context.Background(), store, &n, "UPDATE"))
	assert.NotEqual(t, "crashed", store.nodes["node-1"].ProvisionerJobId)
}

// Test environments launch no job, so hold none; they are still refused
// while a job holds the node.
func TestStartProvisionerJobInTests(t *testing.T) {
	t.Setenv("ENV", "TEST")
	ctx := context.Background()
	store := &memNodeStore{nodes: map[string]models.DynamoDBNode{"node-1": updatingNode()}}
	n := store.nodes["node-1"]
	require.NoError(t, startProvisionerJob(ctx, store, &n, "UPDATE"))
	assert.Empty(t, store.nodes["node-1"].ProvisionerJobId)

	store.nodes["node-1"] = heldNode("job-a", time.Now())
	n = store.nodes["node-1"]
	assert.ErrorIs(t, startProvisionerJob(ctx, store, &n, "UPDATE"), store_dynamodb.ErrProvisionerJobRunning)
}

// A job whose task didn't start leaves the node as it was, free.
func TestAbandonProvisionerJob(t *testing.T) {
	store := &memNodeStore{nodes: map[string]models.DynamoDBNode{}}
	n := heldNode("job-a", time.Now())
	abandonProvisionerJob(context.Background(), store, &n, "Enabled")
	got := store.nodes["node-1"]
	assert.Equal(t, "Enabled", got.Status)
	assert.Empty(t, got.ProvisionerJobId)
	assert.Zero(t, got.ProvisionerJobStartedAt)
}

func TestJobEndsWithItsEvent(t *testing.T) {
	ctx := context.Background()
	for _, jobId := range []string{"job-a", ""} { // "": a provisioner without job ids
		store := &memNodeStore{nodes: map[string]models.DynamoDBNode{"node-1": heldNode("job-a", time.Now())}}
		detail, _ := json.Marshal(ComputeNodeEvent{Action: "UPDATE", ComputeNodeId: "node-1", JobId: jobId})
		require.NoError(t, handleUpdateComplete(ctx, store, detail))
		assert.Equal(t, "Enabled", store.nodes["node-1"].Status)
		assert.Empty(t, store.nodes["node-1"].ProvisionerJobId, "job %q", jobId)

		store.nodes["node-1"] = heldNode("job-a", time.Now())
		detail, _ = json.Marshal(ComputeNodeErrorEvent{Action: "UPDATE", ComputeNodeId: "node-1", JobId: jobId, ErrorMessage: "boom"})
		require.NoError(t, handleProvisioningError(ctx, store, detail))
		assert.Equal(t, "Failed", store.nodes["node-1"].Status)
		assert.Empty(t, store.nodes["node-1"].ProvisionerJobId, "job %q", jobId)
	}

	store := &memNodeStore{nodes: map[string]models.DynamoDBNode{"node-1": heldNode("job-a", time.Now())}}
	store.nodes["node-1"] = func() models.DynamoDBNode { n := store.nodes["node-1"]; n.Status = "Pending"; return n }()
	detail, _ := json.Marshal(ComputeNodeEvent{Action: "CREATE", ComputeNodeId: "node-1", JobId: "job-a", ComputeNodeGatewayUrl: "https://gw"})
	require.NoError(t, handleProvisioningComplete(ctx, store, detail))
	assert.Equal(t, "Enabled", store.nodes["node-1"].Status)
	assert.Empty(t, store.nodes["node-1"].ProvisionerJobId)
}

// The event of a job the node no longer holds (taken over after the
// timeout) changes nothing: the job now running reports for itself.
func TestEventOfAnotherJobIsIgnored(t *testing.T) {
	ctx := context.Background()
	store := &memNodeStore{nodes: map[string]models.DynamoDBNode{"node-1": heldNode("job-b", time.Now())}}
	want := store.nodes["node-1"]

	detail, _ := json.Marshal(ComputeNodeErrorEvent{Action: "UPDATE", ComputeNodeId: "node-1", JobId: "job-a", ErrorMessage: "already exists"})
	require.NoError(t, handleProvisioningError(ctx, store, detail))
	detail, _ = json.Marshal(ComputeNodeEvent{Action: "UPDATE", ComputeNodeId: "node-1", JobId: "job-a"})
	require.NoError(t, handleUpdateComplete(ctx, store, detail))
	detail, _ = json.Marshal(ComputeNodeEvent{Action: "CREATE", ComputeNodeId: "node-1", JobId: "job-a"})
	require.NoError(t, handleProvisioningComplete(ctx, store, detail))

	assert.Equal(t, want, store.nodes["node-1"])
}

// An error for a node that's gone (deleted meanwhile) doesn't recreate it.
func TestErrorForAMissingNode(t *testing.T) {
	store := &memNodeStore{nodes: map[string]models.DynamoDBNode{}}
	detail, _ := json.Marshal(ComputeNodeErrorEvent{Action: "DELETE", ComputeNodeId: "node-1", ErrorMessage: "boom"})
	require.NoError(t, handleProvisioningError(context.Background(), store, detail))
	assert.Empty(t, store.nodes)
}
