// Package quota resolves a compute node's effective pipeline spend limits from
// the stored policy rows.
//
// Resolution is three-tier and axis-by-axis, mirroring the shape chat-service
// uses for LLM limits:
//
//	per-user row (LIMITS#USER#{id}) -> node default (LIMITS#DEFAULT) -> unset
//
// with node-wide aggregate caps read from LIMITS#ALL, which has no per-user
// tier because it is node policy rather than an override.
//
// Unset means UNLIMITED, represented as a nil pointer all the way through. This
// differs from the LLM axes, which collapse to a float64 and always land on a
// small platform safety cap. Pipeline compute runs in the node owner's own AWS
// account, so a default cap would throttle customer-owned nodes for work the
// customer is paying for; and collapsing to float64 would make an explicit $0
// cap ("block everything") indistinguishable from "no cap configured".
package quota

import (
	"github.com/pennsieve/account-service/internal/models"
)

// Tier names which policy row supplied a resolved axis.
type Tier string

const (
	TierUser      Tier = "user"
	TierDefault   Tier = "node-default"
	TierNode      Tier = "node"
	TierUnlimited Tier = "unlimited"
)

// Limit is one resolved axis: a value plus the tier that supplied it. Value nil
// means unlimited, in which case Source is TierUnlimited.
type Limit struct {
	Usd    *float64 `json:"usd,omitempty"`
	Runs   *int     `json:"runs,omitempty"`
	Source Tier     `json:"source"`
}

// Unlimited reports whether this axis imposes no ceiling.
func (l Limit) Unlimited() bool {
	return l.Usd == nil && l.Runs == nil
}

// EffectiveLimits is the fully resolved policy the run-creation gate enforces.
//
// Node* axes bound the node as a whole and are what protect the bill on a
// shared node. User* axes bound one user and are what keep a shared pot fair —
// without them a single user could consume the node's entire daily allowance.
type EffectiveLimits struct {
	NodeDaily         Limit `json:"nodeDaily"`
	NodeMonthly       Limit `json:"nodeMonthly"`
	NodeMaxConcurrent Limit `json:"nodeMaxConcurrent"`
	UserDaily         Limit `json:"userDaily"`
	UserMonthly       Limit `json:"userMonthly"`
	UserMaxConcurrent Limit `json:"userMaxConcurrent"`
	PerRun            Limit `json:"perRun"`
}

// AnyConfigured reports whether any axis imposes a ceiling. When false the gate
// can skip enforcement entirely — the common case for customer-owned nodes,
// which have no policy rows at all.
func (e EffectiveLimits) AnyConfigured() bool {
	for _, l := range []Limit{
		e.NodeDaily, e.NodeMonthly, e.NodeMaxConcurrent,
		e.UserDaily, e.UserMonthly, e.UserMaxConcurrent, e.PerRun,
	} {
		if !l.Unlimited() {
			return true
		}
	}
	return false
}

// ResolvePipeline applies the tiers to the rows on a node.
//
// nodeRow is the LIMITS#ALL row, defaultRow the LIMITS#DEFAULT row, userRow the
// caller's LIMITS#USER#{id} row. Any may be nil (absent). Passing a nil userRow
// resolves the node-wide axes and leaves the user axes on the default tier,
// which is what an operator inspecting node policy wants to see.
func ResolvePipeline(nodeRow, defaultRow, userRow *models.NodeQuota) EffectiveLimits {
	out := EffectiveLimits{
		NodeDaily:         Limit{Source: TierUnlimited},
		NodeMonthly:       Limit{Source: TierUnlimited},
		NodeMaxConcurrent: Limit{Source: TierUnlimited},
		UserDaily:         Limit{Source: TierUnlimited},
		UserMonthly:       Limit{Source: TierUnlimited},
		UserMaxConcurrent: Limit{Source: TierUnlimited},
		PerRun:            Limit{Source: TierUnlimited},
	}

	// Node-wide aggregate caps come only from the node row — there is no
	// per-user override for "what the whole node may spend".
	if nodeRow != nil {
		if v := nodeRow.DailyCostUsd; v != nil {
			out.NodeDaily = Limit{Usd: v, Source: TierNode}
		}
		if v := nodeRow.MonthlyCostUsd; v != nil {
			out.NodeMonthly = Limit{Usd: v, Source: TierNode}
		}
		if v := nodeRow.MaxConcurrentRuns; v != nil {
			out.NodeMaxConcurrent = Limit{Runs: v, Source: TierNode}
		}
	}

	// Per-user axes: default first, then the user override wins where set.
	apply := func(row *models.NodeQuota, tier Tier) {
		if row == nil {
			return
		}
		if v := row.DailyCostUsd; v != nil {
			out.UserDaily = Limit{Usd: v, Source: tier}
		}
		if v := row.MonthlyCostUsd; v != nil {
			out.UserMonthly = Limit{Usd: v, Source: tier}
		}
		if v := row.PerRunCostUsd; v != nil {
			out.PerRun = Limit{Usd: v, Source: tier}
		}
		if v := row.MaxConcurrentRuns; v != nil {
			out.UserMaxConcurrent = Limit{Runs: v, Source: tier}
		}
	}
	apply(defaultRow, TierDefault)
	apply(userRow, TierUser)

	return out
}

// SplitRows sorts a node's policy rows (as returned by one begins_with query)
// into the three tiers for a given user. Rows for other users are ignored.
func SplitRows(rows []models.NodeQuota, userId string) (nodeRow, defaultRow, userRow *models.NodeQuota) {
	for i := range rows {
		r := &rows[i]
		switch r.SK {
		case models.QuotaScopeSKNode:
			nodeRow = r
		case models.QuotaScopeSKDefault:
			defaultRow = r
		default:
			if uid, ok := models.UserIdFromQuotaSK(r.SK); ok && uid == userId && userId != "" {
				userRow = r
			}
		}
	}
	return nodeRow, defaultRow, userRow
}
