package compute

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/pennsieve/account-service/internal/errors"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/utils"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

// accessMode controls how initQuotaContext gates the caller. Owner-only paths
// (PUT, DELETE, list, default-row reads) use AccessModeOwnerOnly. Per-user
// reads (the user's own row, their own usage, their effective limits) use
// AccessModeOwnerOrSelf — the caller is allowed in if they are the userId in
// the path AND have any access to the node (owner / shared / workspace / team).
type accessMode int

const (
	AccessModeOwnerOnly accessMode = iota
	AccessModeOwnerOrSelf
)

// MeUserSentinel is the literal value callers can pass as the userId path
// segment on self-readable endpoints to mean "my own row" without having to
// know their own user node id. Resolved to the caller's userId from the JWT
// claims before any access check or DynamoDB read.
const MeUserSentinel = "me"

// quotaContext holds resolved dependencies for chat-quota handlers. It mirrors
// secretsContext but does not require a gateway URL (these handlers write to
// account-service-owned tables directly).
type quotaContext struct {
	UserID    string
	NodeUuid  string
	TargetUid string // path param userId (may be __default__)
	Node      models.DynamoDBNode
	QuotaTbl  string
	UsageTbl  string
	DDB       *dynamodb.Client
}

// initQuotaContext extracts the (nodeId, targetUserId) path params, loads the
// node, runs the appropriate access check for `mode`, and returns ready-to-use
// stores. Errors are returned as ready-to-send HTTP responses.
//
// `targetUid == MeUserSentinel` ("me") is resolved to the caller's userId
// before the access check runs.
func initQuotaContext(ctx context.Context, request events.APIGatewayV2HTTPRequest, handlerName string, mode accessMode) (*quotaContext, *events.APIGatewayV2HTTPResponse) {
	nodeUuid := request.PathParameters["id"]
	if nodeUuid == "" {
		resp := events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMissingNodeUuid),
		}
		return nil, &resp
	}
	targetUid := request.PathParameters["userId"] // may be "" for list, "me", or a real user node id

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	userId := claims.UserClaim.NodeId

	// Resolve "me" alias before any access check so the rest of the handler
	// works with a concrete userId. Only meaningful when targetUid is set
	// (per-user endpoints); list endpoints leave targetUid empty.
	if targetUid == MeUserSentinel {
		targetUid = userId
	}

	cfg, err := utils.LoadAWSConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		resp := events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}
		return nil, &resp
	}

	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	nodesTable := os.Getenv("COMPUTE_NODES_TABLE")
	nodeStore := store_dynamodb.NewNodeDatabaseStore(dynamoDBClient, nodesTable)

	node, err := nodeStore.GetById(ctx, nodeUuid)
	if err != nil {
		log.Printf("Error getting node %s: %v", nodeUuid, err)
		resp := events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}
		return nil, &resp
	}
	if node.Uuid == "" {
		resp := events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrNotFound),
		}
		return nil, &resp
	}

	// Access decision.
	//
	// Owner check is identical to the secrets handlers (node.UserId match,
	// falling back to org-admin manage access on shared accounts). For
	// AccessModeOwnerOrSelf, if the caller is the targetUid and has any
	// access to the node (owner / shared / workspace / team), they're in —
	// users can always read their own quota state on a node they're allowed
	// to use, never anyone else's.
	isOwner := node.UserId == userId
	if !isOwner {
		isOwner = checkAdminManageAccess(ctx, cfg, dynamoDBClient, userId, node.OrganizationId, node.AccountUuid)
	}

	switch mode {
	case AccessModeOwnerOnly:
		if !isOwner {
			resp := events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusForbidden,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrOnlyOwnerCanChangePermissions),
			}
			return nil, &resp
		}
	case AccessModeOwnerOrSelf:
		// Owner always in. Otherwise the caller must be the target user AND
		// must have some access to the node. `__default__` is policy data
		// only owners get to see (don't expose the node's blanket-default
		// to random users).
		if !isOwner {
			if targetUid != userId || targetUid == models.DefaultUserSentinel {
				resp := events.APIGatewayV2HTTPResponse{
					StatusCode: http.StatusForbidden,
					Body:       errors.ComputeHandlerError(handlerName, errors.ErrForbidden),
				}
				return nil, &resp
			}
			lambdaClient := lambda.NewFromConfig(cfg)
			if errResp := checkNodeAccess(ctx, lambdaClient, dynamoDBClient, handlerName, userId, nodeUuid, node.OrganizationId); errResp != nil {
				return nil, errResp
			}
		}
	}

	quotaTbl := os.Getenv("CHAT_USER_QUOTA_TABLE")
	usageTbl := os.Getenv("CHAT_USER_USAGE_TABLE")
	if quotaTbl == "" || usageTbl == "" {
		log.Printf("CHAT_USER_QUOTA_TABLE or CHAT_USER_USAGE_TABLE env var not set")
		resp := events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}
		return nil, &resp
	}

	return &quotaContext{
		UserID:    userId,
		NodeUuid:  nodeUuid,
		TargetUid: targetUid,
		Node:      node,
		QuotaTbl:  quotaTbl,
		UsageTbl:  usageTbl,
		DDB:       dynamoDBClient,
	}, nil
}

// putUserQuotaRequest is the JSON body of PUT /compute-nodes/{id}/user-quotas/{userId}.
// Each cost-axis field is optional; an explicit nil clears the limit on that axis.
type putUserQuotaRequest struct {
	DailyCostUsd   *float64 `json:"dailyCostUsd"`
	MonthlyCostUsd *float64 `json:"monthlyCostUsd"`
	PerWorkflowUsd *float64 `json:"perWorkflowUsd"`
	Notes          string   `json:"notes,omitempty"`
}

// PutChatUserQuotaHandler creates or replaces the chat quota for (nodeId, userId).
// PUT /compute-nodes/{id}/user-quotas/{userId}
//
// Required Permissions:
// - Must be the owner of the compute node OR an org admin with manage access.
//
// The userId path parameter may be `__default__` to set the node-wide default
// applied when no per-user override exists.
func PutChatUserQuotaHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "PutChatUserQuotaHandler"

	qctx, errResp := initQuotaContext(ctx, request, handlerName, AccessModeOwnerOnly)
	if errResp != nil {
		return *errResp, nil
	}
	if qctx.TargetUid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMissingUserId),
		}, nil
	}

	var body putUserQuotaRequest
	if err := json.Unmarshal([]byte(request.Body), &body); err != nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrUnmarshaling),
		}, nil
	}

	quota := models.ChatUserQuota{
		ComputeNodeId:  qctx.NodeUuid,
		UserId:         qctx.TargetUid,
		DailyCostUsd:   body.DailyCostUsd,
		MonthlyCostUsd: body.MonthlyCostUsd,
		PerWorkflowUsd: body.PerWorkflowUsd,
		Notes:          body.Notes,
		UpdatedBy:      qctx.UserID,
		UpdatedAt:      time.Now().UTC().Format(time.RFC3339),
	}

	quotaStore := store_dynamodb.NewChatUserQuotaStore(qctx.DDB, qctx.QuotaTbl)
	if err := quotaStore.Put(ctx, quota); err != nil {
		log.Printf("Error putting chat user quota: %v", err)
		log.Printf("AUDIT action=put_chat_user_quota result=failure caller=%q node=%q target=%q error=%q",
			qctx.UserID, qctx.NodeUuid, qctx.TargetUid, err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	// Audit trail of admin actions on per-user cost caps. See
	// llm_config_handler.go for the HIPAA / NIST rationale. We log
	// the three cap axes as pointers-or-nil so the entry distinguishes
	// "explicitly set to 0" from "cleared / falls back to default".
	log.Printf("AUDIT action=put_chat_user_quota result=success caller=%q node=%q target=%q daily=%s monthly=%s perWorkflow=%s",
		qctx.UserID, qctx.NodeUuid, qctx.TargetUid,
		formatNullableUsd(quota.DailyCostUsd),
		formatNullableUsd(quota.MonthlyCostUsd),
		formatNullableUsd(quota.PerWorkflowUsd))

	out, _ := json.Marshal(quota)
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       string(out),
	}, nil
}

// formatNullableUsd renders an optional cap value for audit log lines.
// `null` means the axis is unset on this row (falls through to the next
// resolution tier); a number means an explicit cap (including $0.00).
func formatNullableUsd(v *float64) string {
	if v == nil {
		return "null"
	}
	return fmt.Sprintf("%.2f", *v)
}

// GetChatUserQuotaHandler returns the quota row for (nodeId, userId), or 404 if none.
// GET /compute-nodes/{id}/user-quotas/{userId}
//
// Required Permissions:
// - Caller is the userId in the path AND has any access to the node, OR
// - Caller is the node owner / org admin with manage access
// `userId` may be the literal "me" — resolved to the caller's userId.
// Reads of `__default__` stay owner-only (it's policy data).
func GetChatUserQuotaHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetChatUserQuotaHandler"

	qctx, errResp := initQuotaContext(ctx, request, handlerName, AccessModeOwnerOrSelf)
	if errResp != nil {
		return *errResp, nil
	}
	if qctx.TargetUid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMissingUserId),
		}, nil
	}

	quotaStore := store_dynamodb.NewChatUserQuotaStore(qctx.DDB, qctx.QuotaTbl)
	quota, err := quotaStore.Get(ctx, qctx.NodeUuid, qctx.TargetUid)
	if err != nil {
		log.Printf("Error getting chat user quota: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}
	if quota.ComputeNodeId == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrNotFound),
		}, nil
	}

	out, _ := json.Marshal(quota)
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       string(out),
	}, nil
}

// ListChatUserQuotasHandler returns all quota rows for the node (including the
// __default__ row if set).
// GET /compute-nodes/{id}/user-quotas
func ListChatUserQuotasHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "ListChatUserQuotasHandler"

	qctx, errResp := initQuotaContext(ctx, request, handlerName, AccessModeOwnerOnly)
	if errResp != nil {
		return *errResp, nil
	}

	quotaStore := store_dynamodb.NewChatUserQuotaStore(qctx.DDB, qctx.QuotaTbl)
	quotas, err := quotaStore.ListByNode(ctx, qctx.NodeUuid)
	if err != nil {
		log.Printf("Error listing chat user quotas: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	out, _ := json.Marshal(map[string]any{"quotas": quotas})
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       string(out),
	}, nil
}

// DeleteChatUserQuotaHandler removes the quota row for (nodeId, userId).
// DELETE /compute-nodes/{id}/user-quotas/{userId}
func DeleteChatUserQuotaHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "DeleteChatUserQuotaHandler"

	qctx, errResp := initQuotaContext(ctx, request, handlerName, AccessModeOwnerOnly)
	if errResp != nil {
		return *errResp, nil
	}
	if qctx.TargetUid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMissingUserId),
		}, nil
	}

	quotaStore := store_dynamodb.NewChatUserQuotaStore(qctx.DDB, qctx.QuotaTbl)
	if err := quotaStore.Delete(ctx, qctx.NodeUuid, qctx.TargetUid); err != nil {
		log.Printf("Error deleting chat user quota: %v", err)
		log.Printf("AUDIT action=delete_chat_user_quota result=failure caller=%q node=%q target=%q error=%q",
			qctx.UserID, qctx.NodeUuid, qctx.TargetUid, err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	log.Printf("AUDIT action=delete_chat_user_quota result=success caller=%q node=%q target=%q",
		qctx.UserID, qctx.NodeUuid, qctx.TargetUid)

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       `{"message":"chat user quota deleted"}`,
	}, nil
}

// GetChatUserUsageHandler returns aggregate usage rows for (nodeId, userId).
// GET /compute-nodes/{id}/user-usage/{userId}?period=day|month&date=YYYY-MM-DD
//
// If `date` is omitted, defaults to today (UTC). If `period` is omitted,
// returns both daily and monthly aggregates.
//
// Required Permissions:
// - Caller is the userId in the path AND has any access to the node, OR
// - Caller is the node owner / org admin with manage access
// `userId` may be the literal "me" — resolved to the caller's userId.
func GetChatUserUsageHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetChatUserUsageHandler"

	qctx, errResp := initQuotaContext(ctx, request, handlerName, AccessModeOwnerOrSelf)
	if errResp != nil {
		return *errResp, nil
	}
	if qctx.TargetUid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMissingUserId),
		}, nil
	}

	period := request.QueryStringParameters["period"]
	date := request.QueryStringParameters["date"]
	now := time.Now().UTC()
	if date == "" {
		date = now.Format("2006-01-02")
	}
	month := date
	if len(date) >= 7 {
		month = date[:7]
	}

	usageStore := store_dynamodb.NewChatUserUsageStore(qctx.DDB, qctx.UsageTbl)

	result := map[string]any{"computeNodeId": qctx.NodeUuid, "userId": qctx.TargetUid}

	if period == "" || period == "day" {
		dayKey := models.BuildDailyUsageKey(qctx.TargetUid, qctx.NodeUuid, date)
		dayUsage, err := usageStore.Get(ctx, dayKey)
		if err != nil {
			log.Printf("Error getting daily usage: %v", err)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
			}, nil
		}
		result["daily"] = dayUsage
	}

	if period == "" || period == "month" {
		monthKey := models.BuildMonthlyUsageKey(qctx.TargetUid, qctx.NodeUuid, month)
		monthUsage, err := usageStore.Get(ctx, monthKey)
		if err != nil {
			log.Printf("Error getting monthly usage: %v", err)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
			}, nil
		}
		result["monthly"] = monthUsage
	}

	out, _ := json.Marshal(result)
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       string(out),
	}, nil
}

// ----------------------------------------------------------------------------
// Effective-quota resolver + endpoint
// ----------------------------------------------------------------------------
//
// The resolver applies the same three-tier fallback chat-service runs at
// dispatch time:
//
//	user-override row → node __default__ row → platform safety cap (env)
//
// axis-by-axis (any nil/unset axis falls through to the next tier). The
// platform safety cap is configured via the DEFAULT_USER_*_USD env vars on
// the Lambda — same vars chat-service reads on the enforcement side.
//
// THIS FUNCTION MUST STAY IN SYNC with chat-service's
// internal/quota/quota.go:Resolve. Test plan when changing either: update
// both, deploy, verify the values returned by GET .../effective match the
// values chat-service actually enforces on a turn.

// platformSafetyCap is the final-tier fallback when neither the per-user row
// nor the __default__ row sets an axis. Values come from env; the in-code
// constants are sane fallbacks for unset env (matches the same constants in
// chat-service).
type platformSafetyCap struct {
	DailyCostUsd   float64
	MonthlyCostUsd float64
	PerWorkflowUsd float64
}

const (
	defaultSafetyDailyUsd       = 1.00
	defaultSafetyMonthlyUsd     = 10.00
	defaultSafetyPerWorkflowUsd = 0.50
)

func loadPlatformSafetyCap() platformSafetyCap {
	return platformSafetyCap{
		DailyCostUsd:   readEnvFloat("DEFAULT_USER_DAILY_COST_USD", defaultSafetyDailyUsd),
		MonthlyCostUsd: readEnvFloat("DEFAULT_USER_MONTHLY_COST_USD", defaultSafetyMonthlyUsd),
		PerWorkflowUsd: readEnvFloat("DEFAULT_USER_PER_WORKFLOW_USD", defaultSafetyPerWorkflowUsd),
	}
}

func readEnvFloat(name string, fallback float64) float64 {
	v := os.Getenv(name)
	if v == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

// limitSource is the tier that supplied each axis on a resolved limit.
type limitSource string

const (
	limitSourceUser    limitSource = "user"
	limitSourceDefault limitSource = "node-default"
	limitSourceSafety  limitSource = "platform-safety"
)

// effectiveQuotaResponse is the JSON body of GET .../effective. Combines the
// resolved limits with their source attribution and the caller's current
// per-period spend so frontends can render a "you've used $X of $Y" UI
// without doing the math themselves.
type effectiveQuotaResponse struct {
	ComputeNodeId string `json:"computeNodeId"`
	UserId        string `json:"userId"`

	// Resolved limits + which tier supplied each axis.
	DailyCostUsd   float64 `json:"dailyCostUsd"`
	MonthlyCostUsd float64 `json:"monthlyCostUsd"`
	PerWorkflowUsd float64 `json:"perWorkflowUsd"`

	DailySource       limitSource `json:"dailySource"`
	MonthlySource     limitSource `json:"monthlySource"`
	PerWorkflowSource limitSource `json:"perWorkflowSource"`

	// Current spend in each period bucket (UTC day / UTC month).
	DailySpentUsd   float64 `json:"dailySpentUsd"`
	MonthlySpentUsd float64 `json:"monthlySpentUsd"`

	// Convenience: remainingDay = max(0, DailyCostUsd - DailySpentUsd), same
	// for month. Cheap to compute server-side and saves every consumer
	// re-doing it.
	RemainingDayUsd   float64 `json:"remainingDayUsd"`
	RemainingMonthUsd float64 `json:"remainingMonthUsd"`

	// Token counts for the same buckets. Tracked alongside cost so frontends
	// can display a less in-your-face "tokens used" caption instead of
	// dollars while keeping the dollar-based enforcement axis intact.
	// Total = input + output across all turns in the period.
	DailyTokens   int64 `json:"dailyTokens"`
	MonthlyTokens int64 `json:"monthlyTokens"`
}

// GetChatUserEffectiveQuotaHandler returns the resolved limits + current
// usage for (nodeId, userId), with per-axis source attribution. Useful for
// frontends ("why was I blocked?", usage meter, etc.) and for verifying that
// owner-configured overrides have taken effect.
// GET /compute-nodes/{id}/user-quotas/{userId}/effective
//
// Required Permissions:
// - Caller is the userId in the path AND has any access to the node, OR
// - Caller is the node owner / org admin with manage access
// `userId` may be the literal "me" — resolved to the caller's userId.
// The `__default__` sentinel is owner-only here (same as on the raw GET).
func GetChatUserEffectiveQuotaHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetChatUserEffectiveQuotaHandler"

	qctx, errResp := initQuotaContext(ctx, request, handlerName, AccessModeOwnerOrSelf)
	if errResp != nil {
		return *errResp, nil
	}
	if qctx.TargetUid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMissingUserId),
		}, nil
	}

	quotaStore := store_dynamodb.NewChatUserQuotaStore(qctx.DDB, qctx.QuotaTbl)
	usageStore := store_dynamodb.NewChatUserUsageStore(qctx.DDB, qctx.UsageTbl)

	userRow, err := quotaStore.Get(ctx, qctx.NodeUuid, qctx.TargetUid)
	if err != nil {
		log.Printf("Error getting user quota row: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}
	defaultRow, err := quotaStore.Get(ctx, qctx.NodeUuid, models.DefaultUserSentinel)
	if err != nil {
		log.Printf("Error getting default quota row: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	cap := loadPlatformSafetyCap()
	resp := effectiveQuotaResponse{
		ComputeNodeId:     qctx.NodeUuid,
		UserId:            qctx.TargetUid,
		DailyCostUsd:      cap.DailyCostUsd,
		MonthlyCostUsd:    cap.MonthlyCostUsd,
		PerWorkflowUsd:    cap.PerWorkflowUsd,
		DailySource:       limitSourceSafety,
		MonthlySource:     limitSourceSafety,
		PerWorkflowSource: limitSourceSafety,
	}

	// Apply __default__ first, then user-row — user wins where set.
	if defaultRow.ComputeNodeId != "" {
		if defaultRow.DailyCostUsd != nil {
			resp.DailyCostUsd = *defaultRow.DailyCostUsd
			resp.DailySource = limitSourceDefault
		}
		if defaultRow.MonthlyCostUsd != nil {
			resp.MonthlyCostUsd = *defaultRow.MonthlyCostUsd
			resp.MonthlySource = limitSourceDefault
		}
		if defaultRow.PerWorkflowUsd != nil {
			resp.PerWorkflowUsd = *defaultRow.PerWorkflowUsd
			resp.PerWorkflowSource = limitSourceDefault
		}
	}
	if userRow.ComputeNodeId != "" {
		if userRow.DailyCostUsd != nil {
			resp.DailyCostUsd = *userRow.DailyCostUsd
			resp.DailySource = limitSourceUser
		}
		if userRow.MonthlyCostUsd != nil {
			resp.MonthlyCostUsd = *userRow.MonthlyCostUsd
			resp.MonthlySource = limitSourceUser
		}
		if userRow.PerWorkflowUsd != nil {
			resp.PerWorkflowUsd = *userRow.PerWorkflowUsd
			resp.PerWorkflowSource = limitSourceUser
		}
	}

	// Current spend.
	now := time.Now()
	dayKey := models.BuildDailyUsageKey(qctx.TargetUid, qctx.NodeUuid, now.UTC().Format("2006-01-02"))
	monthKey := models.BuildMonthlyUsageKey(qctx.TargetUid, qctx.NodeUuid, now.UTC().Format("2006-01"))

	daily, err := usageStore.Get(ctx, dayKey)
	if err != nil {
		log.Printf("Error getting daily usage: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}
	monthly, err := usageStore.Get(ctx, monthKey)
	if err != nil {
		log.Printf("Error getting monthly usage: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	resp.DailySpentUsd = daily.EstimatedCostUsd
	resp.MonthlySpentUsd = monthly.EstimatedCostUsd
	resp.RemainingDayUsd = maxFloat(0, resp.DailyCostUsd-resp.DailySpentUsd)
	resp.RemainingMonthUsd = maxFloat(0, resp.MonthlyCostUsd-resp.MonthlySpentUsd)
	resp.DailyTokens = daily.TotalInputTokens + daily.TotalOutputTokens
	resp.MonthlyTokens = monthly.TotalInputTokens + monthly.TotalOutputTokens

	out, _ := json.Marshal(resp)
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       string(out),
	}, nil
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
