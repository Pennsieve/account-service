package quota

import (
	"testing"

	"github.com/pennsieve/account-service/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func usd(v float64) *float64 { return &v }
func runs(v int) *int        { return &v }

func userRow(nodeUuid, userId string) *models.NodeQuota {
	return &models.NodeQuota{
		PK:       models.NodeQuotaPK(nodeUuid),
		SK:       models.UserQuotaSK(userId),
		NodeUuid: nodeUuid,
		Scope:    models.QuotaScopeUser,
		UserId:   userId,
	}
}

func defaultRow(nodeUuid string) *models.NodeQuota {
	return &models.NodeQuota{
		PK:       models.NodeQuotaPK(nodeUuid),
		SK:       models.QuotaScopeSKDefault,
		NodeUuid: nodeUuid,
		Scope:    models.QuotaScopeDefault,
	}
}

func nodeRow(nodeUuid string) *models.NodeQuota {
	return &models.NodeQuota{
		PK:       models.NodeQuotaPK(nodeUuid),
		SK:       models.QuotaScopeSKNode,
		NodeUuid: nodeUuid,
		Scope:    models.QuotaScopeNode,
	}
}

// The single most important case: a node with no policy rows must resolve to
// unlimited on every axis. Customer-owned nodes have no rows and their owners
// pay their own AWS bills — if absent policy resolved to a default cap, shipping
// the gate would throttle every existing node on the platform.
func TestNoRowsMeansUnlimited(t *testing.T) {
	limits := ResolvePipeline(nil, nil, nil)

	assert.False(t, limits.AnyConfigured(),
		"a node with no policy rows must impose no ceilings")

	for name, l := range map[string]Limit{
		"nodeDaily":         limits.NodeDaily,
		"nodeMonthly":       limits.NodeMonthly,
		"nodeMaxConcurrent": limits.NodeMaxConcurrent,
		"userDaily":         limits.UserDaily,
		"userMonthly":       limits.UserMonthly,
		"userMaxConcurrent": limits.UserMaxConcurrent,
		"perRun":            limits.PerRun,
	} {
		assert.True(t, l.Unlimited(), "%s should be unlimited", name)
		assert.Equal(t, TierUnlimited, l.Source, "%s source", name)
	}
}

// An explicitly stored $0 is a real cap meaning "block everything" and must not
// be confused with "no cap configured". Collapsing the pointer to a float64
// anywhere in resolution would lose this.
func TestExplicitZeroIsARealLimit(t *testing.T) {
	nr := nodeRow("node-1")
	nr.DailyCostUsd = usd(0)

	limits := ResolvePipeline(nr, nil, nil)

	require.NotNil(t, limits.NodeDaily.Usd)
	assert.Equal(t, 0.0, *limits.NodeDaily.Usd)
	assert.False(t, limits.NodeDaily.Unlimited(), "$0 is a limit, not the absence of one")
	assert.True(t, limits.AnyConfigured())
	assert.Equal(t, TierNode, limits.NodeDaily.Source)
}

func TestNodeRowSuppliesNodeAxesOnly(t *testing.T) {
	nr := nodeRow("node-1")
	nr.DailyCostUsd = usd(20)
	nr.MonthlyCostUsd = usd(200)
	nr.MaxConcurrentRuns = runs(10)

	limits := ResolvePipeline(nr, nil, nil)

	assert.Equal(t, 20.0, *limits.NodeDaily.Usd)
	assert.Equal(t, 200.0, *limits.NodeMonthly.Usd)
	assert.Equal(t, 10, *limits.NodeMaxConcurrent.Runs)

	// A node-wide row says nothing about what one user may spend.
	assert.True(t, limits.UserDaily.Unlimited())
	assert.True(t, limits.UserMonthly.Unlimited())
	assert.True(t, limits.PerRun.Unlimited())
}

// A per-run ceiling is not a node-wide aggregate, so it is only read from the
// default and user rows. Setting it on the node row is ignored rather than
// silently treated as node policy.
func TestPerRunIgnoredOnNodeRow(t *testing.T) {
	nr := nodeRow("node-1")
	nr.PerRunCostUsd = usd(5)

	limits := ResolvePipeline(nr, nil, nil)
	assert.True(t, limits.PerRun.Unlimited(),
		"perRun on the node-wide row should not resolve")
}

func TestDefaultRowSuppliesUserAxes(t *testing.T) {
	dr := defaultRow("node-1")
	dr.DailyCostUsd = usd(2)
	dr.MonthlyCostUsd = usd(20)
	dr.PerRunCostUsd = usd(1)
	dr.MaxConcurrentRuns = runs(2)

	limits := ResolvePipeline(nil, dr, nil)

	assert.Equal(t, 2.0, *limits.UserDaily.Usd)
	assert.Equal(t, TierDefault, limits.UserDaily.Source)
	assert.Equal(t, 20.0, *limits.UserMonthly.Usd)
	assert.Equal(t, 1.0, *limits.PerRun.Usd)
	assert.Equal(t, 2, *limits.UserMaxConcurrent.Runs)

	// The default row is a per-user default, not a node-wide cap.
	assert.True(t, limits.NodeDaily.Unlimited())
}

func TestUserRowOverridesDefault(t *testing.T) {
	dr := defaultRow("node-1")
	dr.DailyCostUsd = usd(2)

	ur := userRow("node-1", "user-1")
	ur.DailyCostUsd = usd(50)

	limits := ResolvePipeline(nil, dr, ur)

	assert.Equal(t, 50.0, *limits.UserDaily.Usd)
	assert.Equal(t, TierUser, limits.UserDaily.Source)
}

// Fallback is per-axis, not per-row: a user row that sets only one axis must
// not wipe out the default row's other axes.
func TestFallbackIsAxisByAxis(t *testing.T) {
	dr := defaultRow("node-1")
	dr.DailyCostUsd = usd(2)
	dr.MonthlyCostUsd = usd(20)
	dr.PerRunCostUsd = usd(1)

	ur := userRow("node-1", "user-1")
	ur.DailyCostUsd = usd(50) // raises only the daily axis

	limits := ResolvePipeline(nil, dr, ur)

	assert.Equal(t, 50.0, *limits.UserDaily.Usd)
	assert.Equal(t, TierUser, limits.UserDaily.Source)

	assert.Equal(t, 20.0, *limits.UserMonthly.Usd)
	assert.Equal(t, TierDefault, limits.UserMonthly.Source)

	assert.Equal(t, 1.0, *limits.PerRun.Usd)
	assert.Equal(t, TierDefault, limits.PerRun.Source)
}

// A user row can also *lower* its own ceiling below the default.
func TestUserRowCanTighten(t *testing.T) {
	dr := defaultRow("node-1")
	dr.DailyCostUsd = usd(10)

	ur := userRow("node-1", "user-1")
	ur.DailyCostUsd = usd(1)

	limits := ResolvePipeline(nil, dr, ur)
	assert.Equal(t, 1.0, *limits.UserDaily.Usd)
}

func TestAllThreeTiersTogether(t *testing.T) {
	nr := nodeRow("node-1")
	nr.DailyCostUsd = usd(20)

	dr := defaultRow("node-1")
	dr.DailyCostUsd = usd(2)
	dr.PerRunCostUsd = usd(1)

	ur := userRow("node-1", "user-1")
	ur.DailyCostUsd = usd(5)

	limits := ResolvePipeline(nr, dr, ur)

	assert.Equal(t, 20.0, *limits.NodeDaily.Usd, "node cap unaffected by user override")
	assert.Equal(t, TierNode, limits.NodeDaily.Source)
	assert.Equal(t, 5.0, *limits.UserDaily.Usd)
	assert.Equal(t, TierUser, limits.UserDaily.Source)
	assert.Equal(t, 1.0, *limits.PerRun.Usd)
	assert.Equal(t, TierDefault, limits.PerRun.Source)
}

func TestAnyConfiguredDetectsSingleAxis(t *testing.T) {
	dr := defaultRow("node-1")
	dr.MaxConcurrentRuns = runs(3)

	limits := ResolvePipeline(nil, dr, nil)
	assert.True(t, limits.AnyConfigured(),
		"a concurrency-only policy still counts as configured")
}

func TestSplitRows(t *testing.T) {
	rows := []models.NodeQuota{
		*nodeRow("node-1"),
		*defaultRow("node-1"),
		*userRow("node-1", "user-1"),
		*userRow("node-1", "user-2"),
	}

	nr, dr, ur := SplitRows(rows, "user-1")

	require.NotNil(t, nr)
	assert.Equal(t, models.QuotaScopeSKNode, nr.SK)
	require.NotNil(t, dr)
	assert.Equal(t, models.QuotaScopeSKDefault, dr.SK)
	require.NotNil(t, ur)
	assert.Equal(t, "user-1", ur.UserId, "must pick this user's row, not another's")
}

// Another user's override must never leak into this user's resolution.
func TestSplitRowsIgnoresOtherUsers(t *testing.T) {
	other := userRow("node-1", "user-2")
	other.DailyCostUsd = usd(999)

	rows := []models.NodeQuota{*defaultRow("node-1"), *other}

	_, dr, ur := SplitRows(rows, "user-1")
	assert.NotNil(t, dr)
	assert.Nil(t, ur, "user-1 has no override row")

	limits := ResolvePipeline(nil, dr, ur)
	assert.True(t, limits.UserDaily.Unlimited(),
		"user-2's generous cap must not apply to user-1")
}

// Resolving with no user (an operator inspecting node policy) must not
// accidentally match a user row.
func TestSplitRowsWithEmptyUserId(t *testing.T) {
	rows := []models.NodeQuota{*nodeRow("node-1"), *userRow("node-1", "user-1")}

	nr, _, ur := SplitRows(rows, "")
	assert.NotNil(t, nr)
	assert.Nil(t, ur)
}

func TestUserIdFromQuotaSK(t *testing.T) {
	uid, ok := models.UserIdFromQuotaSK(models.UserQuotaSK("N:user:abc"))
	assert.True(t, ok)
	assert.Equal(t, "N:user:abc", uid)

	_, ok = models.UserIdFromQuotaSK(models.QuotaScopeSKNode)
	assert.False(t, ok, "node-wide row is not a user row")

	_, ok = models.UserIdFromQuotaSK(models.QuotaScopeSKDefault)
	assert.False(t, ok, "default row is not a user row")
}

func TestIsEmpty(t *testing.T) {
	assert.True(t, defaultRow("node-1").IsEmpty())

	dr := defaultRow("node-1")
	dr.DailyCostUsd = usd(0)
	assert.False(t, dr.IsEmpty(), "an explicit $0 cap is not an empty row")
}
