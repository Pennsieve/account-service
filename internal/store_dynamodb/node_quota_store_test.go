package store_dynamodb_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/quota"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupNodeQuotaStoreTest(t *testing.T) store_dynamodb.NodeQuotaStore {
	// Unique node uuids per test keep rows isolated on the shared table.
	return store_dynamodb.NewNodeQuotaStore(test.GetTestClient(), test.TEST_NODE_QUOTA_TABLE)
}

func f64(v float64) *float64 { return &v }
func i(v int) *int           { return &v }

func TestNodeQuotaStore_PutGet(t *testing.T) {
	store := setupNodeQuotaStoreTest(t)
	ctx := context.Background()
	nodeUuid := uuid.NewString()

	row := models.NodeQuota{
		PK:             models.NodeQuotaPK(nodeUuid),
		SK:             models.QuotaScopeSKNode,
		NodeUuid:       nodeUuid,
		Scope:          models.QuotaScopeNode,
		DailyCostUsd:   f64(20),
		MonthlyCostUsd: f64(200),
		UpdatedBy:      "admin-1",
	}
	require.NoError(t, store.Put(ctx, row))

	got, err := store.Get(ctx, nodeUuid, models.QuotaScopeSKNode)
	require.NoError(t, err)
	assert.Equal(t, nodeUuid, got.NodeUuid)
	assert.Equal(t, models.QuotaScopeNode, got.Scope)
	require.NotNil(t, got.DailyCostUsd)
	assert.Equal(t, 20.0, *got.DailyCostUsd)
	require.NotNil(t, got.MonthlyCostUsd)
	assert.Equal(t, 200.0, *got.MonthlyCostUsd)
	assert.Equal(t, "admin-1", got.UpdatedBy)
}

// An absent policy row is a valid state meaning "unlimited", so a miss must be
// the zero value with no error — not a 500 that would block every run.
func TestNodeQuotaStore_GetMissingIsZeroValue(t *testing.T) {
	store := setupNodeQuotaStoreTest(t)

	got, err := store.Get(context.Background(), uuid.NewString(), models.QuotaScopeSKNode)
	require.NoError(t, err)
	assert.Empty(t, got.PK)
	assert.Nil(t, got.DailyCostUsd)
}

// A nil axis must round-trip as absent, not as zero — otherwise "no cap on this
// axis" would come back as "block everything".
func TestNodeQuotaStore_NilAxisRoundTrips(t *testing.T) {
	store := setupNodeQuotaStoreTest(t)
	ctx := context.Background()
	nodeUuid := uuid.NewString()

	require.NoError(t, store.Put(ctx, models.NodeQuota{
		PK:           models.NodeQuotaPK(nodeUuid),
		SK:           models.QuotaScopeSKDefault,
		NodeUuid:     nodeUuid,
		Scope:        models.QuotaScopeDefault,
		DailyCostUsd: f64(5),
		// MonthlyCostUsd, PerRunCostUsd, MaxConcurrentRuns deliberately nil
	}))

	got, err := store.Get(ctx, nodeUuid, models.QuotaScopeSKDefault)
	require.NoError(t, err)
	require.NotNil(t, got.DailyCostUsd)
	assert.Equal(t, 5.0, *got.DailyCostUsd)
	assert.Nil(t, got.MonthlyCostUsd, "unset axis must stay unset")
	assert.Nil(t, got.PerRunCostUsd)
	assert.Nil(t, got.MaxConcurrentRuns)
}

// An explicit $0 must survive the round trip as 0, distinct from nil.
func TestNodeQuotaStore_ExplicitZeroRoundTrips(t *testing.T) {
	store := setupNodeQuotaStoreTest(t)
	ctx := context.Background()
	nodeUuid := uuid.NewString()

	require.NoError(t, store.Put(ctx, models.NodeQuota{
		PK:           models.NodeQuotaPK(nodeUuid),
		SK:           models.QuotaScopeSKDefault,
		NodeUuid:     nodeUuid,
		DailyCostUsd: f64(0),
	}))

	got, err := store.Get(ctx, nodeUuid, models.QuotaScopeSKDefault)
	require.NoError(t, err)
	require.NotNil(t, got.DailyCostUsd, "$0 must not be stored as absent")
	assert.Equal(t, 0.0, *got.DailyCostUsd)
}

// ListForNode is how resolution gets all three tiers in one round trip.
func TestNodeQuotaStore_ListForNodeReturnsAllTiers(t *testing.T) {
	store := setupNodeQuotaStoreTest(t)
	ctx := context.Background()
	nodeUuid := uuid.NewString()

	require.NoError(t, store.Put(ctx, models.NodeQuota{
		PK: models.NodeQuotaPK(nodeUuid), SK: models.QuotaScopeSKNode,
		NodeUuid: nodeUuid, Scope: models.QuotaScopeNode, DailyCostUsd: f64(20),
	}))
	require.NoError(t, store.Put(ctx, models.NodeQuota{
		PK: models.NodeQuotaPK(nodeUuid), SK: models.QuotaScopeSKDefault,
		NodeUuid: nodeUuid, Scope: models.QuotaScopeDefault, DailyCostUsd: f64(2),
	}))
	require.NoError(t, store.Put(ctx, models.NodeQuota{
		PK: models.NodeQuotaPK(nodeUuid), SK: models.UserQuotaSK("user-1"),
		NodeUuid: nodeUuid, Scope: models.QuotaScopeUser, UserId: "user-1", DailyCostUsd: f64(5),
	}))

	rows, err := store.ListForNode(ctx, nodeUuid)
	require.NoError(t, err)
	require.Len(t, rows, 3)

	nodeRow, defaultRow, userRow := quota.SplitRows(rows, "user-1")
	require.NotNil(t, nodeRow)
	require.NotNil(t, defaultRow)
	require.NotNil(t, userRow)

	limits := quota.ResolvePipeline(nodeRow, defaultRow, userRow)
	assert.Equal(t, 20.0, *limits.NodeDaily.Usd)
	assert.Equal(t, 5.0, *limits.UserDaily.Usd)
	assert.Equal(t, quota.TierUser, limits.UserDaily.Source)
}

// Policy on one node must never be read as policy on another.
func TestNodeQuotaStore_ListForNodeIsolatesNodes(t *testing.T) {
	store := setupNodeQuotaStoreTest(t)
	ctx := context.Background()
	nodeA, nodeB := uuid.NewString(), uuid.NewString()

	require.NoError(t, store.Put(ctx, models.NodeQuota{
		PK: models.NodeQuotaPK(nodeA), SK: models.QuotaScopeSKNode,
		NodeUuid: nodeA, DailyCostUsd: f64(20),
	}))

	rows, err := store.ListForNode(ctx, nodeB)
	require.NoError(t, err)
	assert.Empty(t, rows, "node B must not see node A's policy")
}

func TestNodeQuotaStore_ListForNodeEmpty(t *testing.T) {
	store := setupNodeQuotaStoreTest(t)

	rows, err := store.ListForNode(context.Background(), uuid.NewString())
	require.NoError(t, err)
	assert.Empty(t, rows)

	// A node with no rows must resolve to no ceilings — the customer-owned case.
	nr, dr, ur := quota.SplitRows(rows, "user-1")
	assert.False(t, quota.ResolvePipeline(nr, dr, ur).AnyConfigured())
}

func TestNodeQuotaStore_Delete(t *testing.T) {
	store := setupNodeQuotaStoreTest(t)
	ctx := context.Background()
	nodeUuid := uuid.NewString()

	require.NoError(t, store.Put(ctx, models.NodeQuota{
		PK: models.NodeQuotaPK(nodeUuid), SK: models.UserQuotaSK("user-1"),
		NodeUuid: nodeUuid, UserId: "user-1", DailyCostUsd: f64(5),
	}))
	require.NoError(t, store.Delete(ctx, nodeUuid, models.UserQuotaSK("user-1")))

	got, err := store.Get(ctx, nodeUuid, models.UserQuotaSK("user-1"))
	require.NoError(t, err)
	assert.Empty(t, got.PK)
}

// Deleting an absent row is not an error — makes the endpoint idempotent.
func TestNodeQuotaStore_DeleteMissingIsNoError(t *testing.T) {
	store := setupNodeQuotaStoreTest(t)
	err := store.Delete(context.Background(), uuid.NewString(), models.QuotaScopeSKDefault)
	assert.NoError(t, err)
}

// Put replaces the row wholesale, so clearing an axis actually clears it rather
// than leaving the old ceiling in place.
func TestNodeQuotaStore_PutReplacesRow(t *testing.T) {
	store := setupNodeQuotaStoreTest(t)
	ctx := context.Background()
	nodeUuid := uuid.NewString()

	require.NoError(t, store.Put(ctx, models.NodeQuota{
		PK: models.NodeQuotaPK(nodeUuid), SK: models.QuotaScopeSKDefault,
		NodeUuid: nodeUuid, DailyCostUsd: f64(5), MonthlyCostUsd: f64(50),
	}))
	require.NoError(t, store.Put(ctx, models.NodeQuota{
		PK: models.NodeQuotaPK(nodeUuid), SK: models.QuotaScopeSKDefault,
		NodeUuid: nodeUuid, DailyCostUsd: f64(9),
	}))

	got, err := store.Get(ctx, nodeUuid, models.QuotaScopeSKDefault)
	require.NoError(t, err)
	assert.Equal(t, 9.0, *got.DailyCostUsd)
	assert.Nil(t, got.MonthlyCostUsd, "cleared axis must not survive the replace")
}

func TestNodeQuotaStore_PutRequiresKeys(t *testing.T) {
	store := setupNodeQuotaStoreTest(t)
	err := store.Put(context.Background(), models.NodeQuota{NodeUuid: "n1"})
	assert.Error(t, err, "a row without pk/sk must be rejected")
}

func TestNodeQuotaStore_ConcurrencyAxisRoundTrips(t *testing.T) {
	store := setupNodeQuotaStoreTest(t)
	ctx := context.Background()
	nodeUuid := uuid.NewString()

	require.NoError(t, store.Put(ctx, models.NodeQuota{
		PK: models.NodeQuotaPK(nodeUuid), SK: models.QuotaScopeSKNode,
		NodeUuid: nodeUuid, MaxConcurrentRuns: i(10),
	}))

	got, err := store.Get(ctx, nodeUuid, models.QuotaScopeSKNode)
	require.NoError(t, err)
	require.NotNil(t, got.MaxConcurrentRuns)
	assert.Equal(t, 10, *got.MaxConcurrentRuns)
}
