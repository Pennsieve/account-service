package store_dynamodb_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func jobTestNode() models.DynamoDBNode {
	return models.DynamoDBNode{
		Uuid:           uuid.New().String(),
		Name:           "job-node",
		AccountUuid:    "account-job",
		OrganizationId: test.OrgNodeId("org-job"),
		UserId:         test.UserNodeId("user-job"),
		Status:         "Enabled",
	}
}

func withJob(n models.DynamoDBNode, id, status string, startedAt int64) models.DynamoDBNode {
	n.ProvisionerJobId = id
	n.ProvisionerJobAction = "UPDATE"
	n.ProvisionerJobStartedAt = startedAt
	n.Status = status
	return n
}

// One provisioner job per node (dev 48b8ca51 had two updates applying at
// once): a job starts only while no other holds the node.
func TestNodeStore_StartProvisionerJob(t *testing.T) {
	store := setupNodeStoreTest(t)
	ctx := context.Background()
	now := time.Now().Unix()
	staleBefore := now - 3*3600

	node := jobTestNode()
	require.NoError(t, store.Put(ctx, node))

	// A free node: the job starts, with the node's changes.
	require.NoError(t, store.StartProvisionerJob(ctx, withJob(node, "job-a", "Updating", now), staleBefore))
	got, err := store.GetById(ctx, node.Uuid)
	require.NoError(t, err)
	assert.Equal(t, "job-a", got.ProvisionerJobId)
	assert.Equal(t, "UPDATE", got.ProvisionerJobAction)
	assert.Equal(t, now, got.ProvisionerJobStartedAt)
	assert.Equal(t, "Updating", got.Status)

	// Held: a second job is refused, and nothing it carried is stored.
	second := withJob(node, "job-b", "Destroying", now)
	second.Name = "renamed"
	err = store.StartProvisionerJob(ctx, second, staleBefore)
	assert.ErrorIs(t, err, store_dynamodb.ErrProvisionerJobRunning)
	got, _ = store.GetById(ctx, node.Uuid)
	assert.Equal(t, "job-a", got.ProvisionerJobId)
	assert.Equal(t, "Updating", got.Status)
	assert.Equal(t, "job-node", got.Name)

	// Nor does a start without a job (a test environment's) get through.
	assert.ErrorIs(t, store.StartProvisionerJob(ctx, node, staleBefore), store_dynamodb.ErrProvisionerJobRunning)

	// Ended (the job's event stored the node without it): the next starts.
	got.ProvisionerJobId, got.ProvisionerJobAction, got.ProvisionerJobStartedAt = "", "", 0
	require.NoError(t, store.Put(ctx, got))
	require.NoError(t, store.StartProvisionerJob(ctx, withJob(node, "job-c", "Updating", now), staleBefore))

	// A job older than the timeout never reported (its task crashed): it no
	// longer holds the node.
	require.NoError(t, store.Put(ctx, withJob(node, "job-crashed", "Updating", staleBefore-1)))
	require.NoError(t, store.StartProvisionerJob(ctx, withJob(node, "job-d", "Updating", now), staleBefore))
	got, _ = store.GetById(ctx, node.Uuid)
	assert.Equal(t, "job-d", got.ProvisionerJobId)
}

// A node that's gone (its delete completed) can't start a job: the start
// would recreate its record.
func TestNodeStore_StartProvisionerJob_NodeGone(t *testing.T) {
	store := setupNodeStoreTest(t)
	ctx := context.Background()
	node := jobTestNode()
	err := store.StartProvisionerJob(ctx, withJob(node, "job-a", "Updating", time.Now().Unix()), 0)
	assert.Error(t, err)
	got, _ := store.GetById(ctx, node.Uuid)
	assert.Empty(t, got.Uuid, "the start created the node")
}

// Requests that arrive together (two clicks, two tabs): exactly one starts.
func TestNodeStore_StartProvisionerJob_Concurrent(t *testing.T) {
	store := setupNodeStoreTest(t)
	ctx := context.Background()
	node := jobTestNode()
	require.NoError(t, store.Put(ctx, node))
	now := time.Now().Unix()

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = store.StartProvisionerJob(ctx, withJob(node, uuid.NewString(), "Updating", now), now-3600)
		}(i)
	}
	wg.Wait()

	started := 0
	for _, err := range errs {
		if err == nil {
			started++
		} else {
			assert.ErrorIs(t, err, store_dynamodb.ErrProvisionerJobRunning)
		}
	}
	assert.Equal(t, 1, started)
}
