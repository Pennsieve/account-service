package models

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Pipeline spend limits for a compute node.
//
// Deliberately separate from the chat/LLM quota table (`chat_user_quota`).
// That table is keyed (computeNodeId, userId) — every row is about a user — but
// a node-wide pipeline cap ("this node may spend $20/day in total") is not
// per-user at all. Storing node policy on that table's `__default__` sentinel
// row would encode two entity types in one user-keyed table and require a guard
// rejecting the meaningless combination. Here the scope lives in the sort key
// instead, and the LLM path stays untouched.
//
// Layout mirrors the node usage table in workflow-service, so the limits/usage
// pair reads coherently:
//
//	pk = NODE#{nodeUuid}
//	sk = LIMITS#ALL            <- node-wide policy (all users combined)
//	     LIMITS#DEFAULT        <- default applied to any user without an override
//	     LIMITS#USER#{userId}  <- per-user override
//
// One begins_with(sk, "LIMITS#") query returns all three tiers, so resolution
// costs a single round trip.
const (
	// QuotaScopeSKNode is the node-wide policy row.
	QuotaScopeSKNode = "LIMITS#ALL"
	// QuotaScopeSKDefault is the per-user default row.
	QuotaScopeSKDefault = "LIMITS#DEFAULT"

	quotaSKPrefix     = "LIMITS#"
	quotaSKUserPrefix = "LIMITS#USER#"
)

// Scope discriminators stored on the row for readability in the console and in
// API responses.
const (
	QuotaScopeNode    = "node"
	QuotaScopeDefault = "default"
	QuotaScopeUser    = "user"
)

// NodeQuotaPK is the partition key for every limit row on a node.
func NodeQuotaPK(nodeUuid string) string {
	return "NODE#" + nodeUuid
}

// UserQuotaSK is the sort key for one user's override on a node.
func UserQuotaSK(userId string) string {
	return quotaSKUserPrefix + userId
}

// QuotaSKPrefix is the begins_with prefix that selects every limit row on a node.
func QuotaSKPrefix() string {
	return quotaSKPrefix
}

// UserIdFromQuotaSK extracts the userId from a per-user sort key, reporting
// false for the node-wide and default rows.
func UserIdFromQuotaSK(sk string) (string, bool) {
	if !strings.HasPrefix(sk, quotaSKUserPrefix) {
		return "", false
	}
	return strings.TrimPrefix(sk, quotaSKUserPrefix), true
}

// NodeQuota is one row of pipeline spend policy on a compute node. Which scope
// it applies to is determined by its sort key.
//
// Every limit field is a pointer, and nil means "this row sets no limit on this
// axis" — resolution falls through to the next tier, and if no tier sets it the
// axis is UNLIMITED.
//
// Unlimited-by-default is deliberate, and differs from the LLM axes which always
// land on a small platform safety cap. Pipeline compute runs in the node owner's
// own AWS account: defaulting a customer-owned node to a few dollars a day would
// throttle work the customer is paying for themselves. Caps apply where an
// operator sets them — for the shared platform node, that's the LIMITS#ALL and
// LIMITS#DEFAULT rows.
//
// An explicitly stored 0 is a real limit meaning "block everything", distinct
// from nil. Carrying pointers through resolution is what preserves that.
type NodeQuota struct {
	PK string `dynamodbav:"pk" json:"-"`
	SK string `dynamodbav:"sk" json:"-"`

	NodeUuid string `dynamodbav:"nodeUuid" json:"nodeUuid"`

	// Scope is "node" | "default" | "user" — redundant with the sort key, kept
	// for legibility when reading rows directly.
	Scope  string `dynamodbav:"scope" json:"scope"`
	UserId string `dynamodbav:"userId,omitempty" json:"userId,omitempty"`

	// DailyCostUsd / MonthlyCostUsd cap spend in the UTC day / month. On the
	// node-wide row these cover all users combined; on default/user rows they
	// cover a single user.
	DailyCostUsd   *float64 `dynamodbav:"dailyCostUsd,omitempty" json:"dailyCostUsd,omitempty"`
	MonthlyCostUsd *float64 `dynamodbav:"monthlyCostUsd,omitempty" json:"monthlyCostUsd,omitempty"`

	// PerRunCostUsd is the hard ceiling on a single run's total cost. Only
	// meaningful on default/user rows — a per-run ceiling is not a node-wide
	// aggregate.
	PerRunCostUsd *float64 `dynamodbav:"perRunCostUsd,omitempty" json:"perRunCostUsd,omitempty"`

	// MaxConcurrentRuns bounds simultaneous non-terminal runs. Run cost is only
	// known at finalize, so concurrent runs are invisible to a pre-flight spend
	// check; this is what bounds the resulting overrun to
	// concurrency x per-run ceiling.
	MaxConcurrentRuns *int `dynamodbav:"maxConcurrentRuns,omitempty" json:"maxConcurrentRuns,omitempty"`

	Notes     string `dynamodbav:"notes,omitempty" json:"notes,omitempty"`
	UpdatedBy string `dynamodbav:"updatedBy,omitempty" json:"updatedBy,omitempty"`
	UpdatedAt string `dynamodbav:"updatedAt,omitempty" json:"updatedAt,omitempty"`
}

func (q NodeQuota) GetKey() map[string]types.AttributeValue {
	pk, _ := attributevalue.Marshal(q.PK)
	sk, _ := attributevalue.Marshal(q.SK)
	return map[string]types.AttributeValue{"pk": pk, "sk": sk}
}

// IsEmpty reports whether the row sets no limit at all. A row that sets nothing
// is indistinguishable from an absent row during resolution, so callers can use
// this to reject a no-op write.
func (q NodeQuota) IsEmpty() bool {
	return q.DailyCostUsd == nil &&
		q.MonthlyCostUsd == nil &&
		q.PerRunCostUsd == nil &&
		q.MaxConcurrentRuns == nil
}
