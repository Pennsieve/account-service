package compute

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pennsieve/account-service/internal/errors"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/utils"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

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
// node, runs the owner check, and returns ready-to-use stores. Errors are
// returned as ready-to-send HTTP responses.
func initQuotaContext(ctx context.Context, request events.APIGatewayV2HTTPRequest, handlerName string, requireOwner bool) (*quotaContext, *events.APIGatewayV2HTTPResponse) {
	nodeUuid := request.PathParameters["id"]
	if nodeUuid == "" {
		resp := events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMissingNodeUuid),
		}
		return nil, &resp
	}
	targetUid := request.PathParameters["userId"] // may be "" for list, or the path value

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	userId := claims.UserClaim.NodeId

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

	if requireOwner {
		canManage := node.UserId == userId
		if !canManage {
			canManage = checkAdminManageAccess(ctx, cfg, dynamoDBClient, userId, node.OrganizationId, node.AccountUuid)
		}
		if !canManage {
			resp := events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusForbidden,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrOnlyOwnerCanChangePermissions),
			}
			return nil, &resp
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

	qctx, errResp := initQuotaContext(ctx, request, handlerName, true)
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
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	out, _ := json.Marshal(quota)
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       string(out),
	}, nil
}

// GetChatUserQuotaHandler returns the quota row for (nodeId, userId), or 404 if none.
// GET /compute-nodes/{id}/user-quotas/{userId}
func GetChatUserQuotaHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetChatUserQuotaHandler"

	qctx, errResp := initQuotaContext(ctx, request, handlerName, true)
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

	qctx, errResp := initQuotaContext(ctx, request, handlerName, true)
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

	qctx, errResp := initQuotaContext(ctx, request, handlerName, true)
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
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

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
func GetChatUserUsageHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetChatUserUsageHandler"

	qctx, errResp := initQuotaContext(ctx, request, handlerName, true)
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
