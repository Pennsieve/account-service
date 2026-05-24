package models

import (
	"fmt"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// DefaultUserSentinel is the SK value used in the chat_user_quota table to
// represent a node-wide default that applies to any user without an explicit
// override.
const DefaultUserSentinel = "__default__"

// ChatUserQuota is a per-(computeNode, user) cost ceiling for chat & workflow LLM spend.
// A row with UserId == DefaultUserSentinel is the node-wide default for that node.
// Any of the limit fields left nil means "no limit on that axis".
type ChatUserQuota struct {
	ComputeNodeId   string   `dynamodbav:"computeNodeId" json:"computeNodeId"`
	UserId          string   `dynamodbav:"userId" json:"userId"`
	DailyCostUsd    *float64 `dynamodbav:"dailyCostUsd,omitempty" json:"dailyCostUsd,omitempty"`
	MonthlyCostUsd  *float64 `dynamodbav:"monthlyCostUsd,omitempty" json:"monthlyCostUsd,omitempty"`
	PerWorkflowUsd  *float64 `dynamodbav:"perWorkflowUsd,omitempty" json:"perWorkflowUsd,omitempty"`
	Notes           string   `dynamodbav:"notes,omitempty" json:"notes,omitempty"`
	UpdatedBy       string   `dynamodbav:"updatedBy,omitempty" json:"updatedBy,omitempty"`
	UpdatedAt       string   `dynamodbav:"updatedAt,omitempty" json:"updatedAt,omitempty"`
}

func (q ChatUserQuota) GetKey() map[string]types.AttributeValue {
	nodeId, _ := attributevalue.Marshal(q.ComputeNodeId)
	userId, _ := attributevalue.Marshal(q.UserId)
	return map[string]types.AttributeValue{
		"computeNodeId": nodeId,
		"userId":        userId,
	}
}

// ChatUserUsage is an aggregate row of chat & workflow LLM cost for a single
// (user, computeNode) pair over a single time bucket (day or month).
//
// The hash key UserNodeDate is composed as:
//   - Daily:   "{userId}#{computeNodeId}#YYYY-MM-DD"
//   - Monthly: "{userId}#{computeNodeId}#YYYY-MM"
//
// The sort key RecordType is always "AGGREGATE" for v1 (mirrors the governor's
// pattern; leaves room for per-execution breakdowns later).
type ChatUserUsage struct {
	UserNodeDate      string  `dynamodbav:"userNodeDate" json:"userNodeDate"`
	RecordType        string  `dynamodbav:"recordType" json:"recordType"`
	UserId            string  `dynamodbav:"userId" json:"userId"`
	ComputeNodeId     string  `dynamodbav:"computeNodeId" json:"computeNodeId"`
	Period            string  `dynamodbav:"period" json:"period"`
	TotalInputTokens  int64   `dynamodbav:"totalInputTokens" json:"totalInputTokens"`
	TotalOutputTokens int64   `dynamodbav:"totalOutputTokens" json:"totalOutputTokens"`
	EstimatedCostUsd  float64 `dynamodbav:"estimatedCostUsd" json:"estimatedCostUsd"`
	RequestCount      int64   `dynamodbav:"requestCount" json:"requestCount"`
	LastModel         string  `dynamodbav:"lastModel,omitempty" json:"lastModel,omitempty"`
	UpdatedAt         string  `dynamodbav:"updatedAt" json:"updatedAt"`
	ExpiresAt         int64   `dynamodbav:"expiresAt,omitempty" json:"expiresAt,omitempty"`
}

// AggregateRecordType is the sort key for rolled-up usage rows.
const AggregateRecordType = "AGGREGATE"

func (u ChatUserUsage) GetKey() map[string]types.AttributeValue {
	hash, _ := attributevalue.Marshal(u.UserNodeDate)
	sort, _ := attributevalue.Marshal(u.RecordType)
	return map[string]types.AttributeValue{
		"userNodeDate": hash,
		"recordType":   sort,
	}
}

// BuildDailyUsageKey builds the daily-aggregate hash key for a given user/node/date.
// Date should be formatted as "YYYY-MM-DD".
func BuildDailyUsageKey(userId, computeNodeId, date string) string {
	return fmt.Sprintf("%s#%s#%s", userId, computeNodeId, date)
}

// BuildMonthlyUsageKey builds the monthly-aggregate hash key for a given user/node/month.
// Month should be formatted as "YYYY-MM".
func BuildMonthlyUsageKey(userId, computeNodeId, month string) string {
	return fmt.Sprintf("%s#%s#%s", userId, computeNodeId, month)
}
