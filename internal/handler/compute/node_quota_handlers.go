package compute

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/pennsieve/account-service/internal/errors"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/quota"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
)

// Pipeline spend policy endpoints.
//
//	GET    /compute-nodes/{id}/pipeline-quotas                    (owner)
//	PUT    /compute-nodes/{id}/pipeline-quotas/{scope}             (owner)
//	GET    /compute-nodes/{id}/pipeline-quotas/{scope}             (owner, or self for a user scope)
//	DELETE /compute-nodes/{id}/pipeline-quotas/{scope}             (owner)
//	GET    /compute-nodes/{id}/pipeline-quotas/{scope}/effective   (owner, or self)
//
// `scope` is `node`, `default`, a user node id, or `me`. Access reuses the
// chat-quota handlers' initQuotaContext so both quota families gate identically.

// resolveQuotaScopeSK maps a `scope` path segment to a sort key.
//
// `node` and `default` are node policy and stay owner-only; a user id (or `me`)
// addresses that user's override row.
func resolveQuotaScopeSK(scope, callerUserId string) (sk string, targetUser string, ok bool) {
	switch scope {
	case "node":
		return models.QuotaScopeSKNode, "", true
	case "default":
		return models.QuotaScopeSKDefault, "", true
	case "":
		return "", "", false
	case MeUserSentinel:
		return models.UserQuotaSK(callerUserId), callerUserId, true
	default:
		return models.UserQuotaSK(scope), scope, true
	}
}

// nodeQuotaCtx bundles the resolved access decision and the store.
type nodeQuotaCtx struct {
	CallerID   string
	NodeUuid   string
	Scope      string
	SK         string
	TargetUser string
	Store      store_dynamodb.NodeQuotaStore
}

// initNodeQuotaContext resolves the node, the scope, and the access decision.
//
// Node-scoped policy (`node`, `default`) is owner-only. A user scope is
// readable by that user (so they can see their own allowance) but writable only
// by an owner — a user must never be able to raise their own cap.
func initNodeQuotaContext(ctx context.Context, request events.APIGatewayV2HTTPRequest, handlerName string, write bool) (*nodeQuotaCtx, *events.APIGatewayV2HTTPResponse) {
	scope := request.PathParameters["scope"]

	// Writes and node-policy reads are owner-only. A user reading their own
	// scope goes through OwnerOrSelf, which additionally requires that they
	// have access to the node.
	mode := AccessModeOwnerOnly
	if !write && scope != "node" && scope != "default" {
		mode = AccessModeOwnerOrSelf
	}

	// initQuotaContext reads the userId path param for its self-check; the
	// pipeline routes carry the identity in `scope` instead, so mirror it
	// across before delegating.
	if mode == AccessModeOwnerOrSelf {
		if request.PathParameters == nil {
			request.PathParameters = map[string]string{}
		}
		request.PathParameters["userId"] = scope
	}

	qctx, errResp := initQuotaContext(ctx, request, handlerName, mode)
	if errResp != nil {
		return nil, errResp
	}

	sk, targetUser, ok := resolveQuotaScopeSK(scope, qctx.UserID)
	if !ok {
		resp := events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, fmt.Errorf("scope is required (node | default | <userId> | me)")),
		}
		return nil, &resp
	}

	tbl := os.Getenv("NODE_QUOTA_TABLE")
	if tbl == "" {
		log.Printf("NODE_QUOTA_TABLE env var not set")
		resp := events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}
		return nil, &resp
	}

	return &nodeQuotaCtx{
		CallerID:   qctx.UserID,
		NodeUuid:   qctx.NodeUuid,
		Scope:      scope,
		SK:         sk,
		TargetUser: targetUser,
		Store:      store_dynamodb.NewNodeQuotaStore(qctx.DDB, tbl),
	}, nil
}

// putNodeQuotaRequest is the body of PUT .../pipeline-quotas/{scope}.
//
// Every field is optional and nil clears that axis, letting it fall through to
// the next resolution tier (and to unlimited if no tier sets it). An explicit 0
// is a real cap meaning "block everything".
type putNodeQuotaRequest struct {
	DailyCostUsd      *float64 `json:"dailyCostUsd"`
	MonthlyCostUsd    *float64 `json:"monthlyCostUsd"`
	PerRunCostUsd     *float64 `json:"perRunCostUsd"`
	MaxConcurrentRuns *int     `json:"maxConcurrentRuns"`
	Notes             string   `json:"notes,omitempty"`
}

// PutNodeQuotaHandler creates or replaces one pipeline policy row.
// PUT /compute-nodes/{id}/pipeline-quotas/{scope}
//
// Required Permissions:
// - Must be the owner of the compute node OR an org admin with manage access.
//
// Owner-only for every scope, including a user scope: a user raising their own
// cap would defeat the point.
func PutNodeQuotaHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "PutNodeQuotaHandler"

	qctx, errResp := initNodeQuotaContext(ctx, request, handlerName, true)
	if errResp != nil {
		return *errResp, nil
	}

	var body putNodeQuotaRequest
	if err := json.Unmarshal([]byte(request.Body), &body); err != nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrUnmarshaling),
		}, nil
	}

	row := models.NodeQuota{
		PK:                models.NodeQuotaPK(qctx.NodeUuid),
		SK:                qctx.SK,
		NodeUuid:          qctx.NodeUuid,
		UserId:            qctx.TargetUser,
		DailyCostUsd:      body.DailyCostUsd,
		MonthlyCostUsd:    body.MonthlyCostUsd,
		PerRunCostUsd:     body.PerRunCostUsd,
		MaxConcurrentRuns: body.MaxConcurrentRuns,
		Notes:             body.Notes,
		UpdatedBy:         qctx.CallerID,
		UpdatedAt:         time.Now().UTC().Format(time.RFC3339),
	}

	switch qctx.SK {
	case models.QuotaScopeSKNode:
		row.Scope = models.QuotaScopeNode
		// A per-run ceiling is a property of a single run, not a node-wide
		// aggregate, and the resolver ignores it here. Reject rather than
		// accept a value that would silently never apply.
		if row.PerRunCostUsd != nil {
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusBadRequest,
				Body: errors.ComputeHandlerError(handlerName,
					fmt.Errorf("perRunCostUsd is not a node-wide limit; set it on the 'default' scope or a user scope")),
			}, nil
		}
	case models.QuotaScopeSKDefault:
		row.Scope = models.QuotaScopeDefault
	default:
		row.Scope = models.QuotaScopeUser
	}

	if err := qctx.Store.Put(ctx, row); err != nil {
		log.Printf("Error putting node quota: %v", err)
		log.Printf("AUDIT action=put_node_quota result=failure caller=%q node=%q scope=%q error=%q",
			qctx.CallerID, qctx.NodeUuid, qctx.Scope, err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	// Audit trail of admin changes to spend policy — same rationale as the
	// chat quota handlers. Axes are logged as value-or-null so the entry
	// distinguishes an explicit $0 (block everything) from a cleared axis
	// (falls through to the next tier).
	log.Printf("AUDIT action=put_node_quota result=success caller=%q node=%q scope=%q daily=%s monthly=%s perRun=%s maxConcurrent=%s",
		qctx.CallerID, qctx.NodeUuid, qctx.Scope,
		formatNullableUsd(row.DailyCostUsd),
		formatNullableUsd(row.MonthlyCostUsd),
		formatNullableUsd(row.PerRunCostUsd),
		formatNullableInt(row.MaxConcurrentRuns))

	out, _ := json.Marshal(row)
	return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusOK, Body: string(out)}, nil
}

// formatNullableInt renders an optional run-count limit for audit log lines.
// `null` means the axis is unset on this row; a number is an explicit limit
// (including 0, which blocks everything).
func formatNullableInt(v *int) string {
	if v == nil {
		return "null"
	}
	return fmt.Sprintf("%d", *v)
}

// GetNodeQuotaHandler returns one stored policy row, or 404 if not set.
// GET /compute-nodes/{id}/pipeline-quotas/{scope}
//
// Required Permissions:
// - `node` and `default` scopes: owner / org admin with manage access.
// - A user scope: that user (with access to the node), or an owner.
// `scope` may be the literal "me".
func GetNodeQuotaHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetNodeQuotaHandler"

	qctx, errResp := initNodeQuotaContext(ctx, request, handlerName, false)
	if errResp != nil {
		return *errResp, nil
	}

	row, err := qctx.Store.Get(ctx, qctx.NodeUuid, qctx.SK)
	if err != nil {
		log.Printf("Error getting node quota: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}
	if row.PK == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrNotFound),
		}, nil
	}

	out, _ := json.Marshal(row)
	return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusOK, Body: string(out)}, nil
}

// ListNodeQuotasHandler returns every policy row on the node.
// GET /compute-nodes/{id}/pipeline-quotas
//
// Required Permissions:
// - Must be the owner of the compute node OR an org admin with manage access.
func ListNodeQuotasHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "ListNodeQuotasHandler"

	// No scope segment on this route; initQuotaContext gates owner-only.
	qctx, errResp := initQuotaContext(ctx, request, handlerName, AccessModeOwnerOnly)
	if errResp != nil {
		return *errResp, nil
	}

	tbl := os.Getenv("NODE_QUOTA_TABLE")
	if tbl == "" {
		log.Printf("NODE_QUOTA_TABLE env var not set")
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}

	rows, err := store_dynamodb.NewNodeQuotaStore(qctx.DDB, tbl).ListForNode(ctx, qctx.NodeUuid)
	if err != nil {
		log.Printf("Error listing node quotas: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}
	if rows == nil {
		rows = []models.NodeQuota{}
	}

	out, _ := json.Marshal(map[string]interface{}{
		"nodeUuid": qctx.NodeUuid,
		"quotas":   rows,
	})
	return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusOK, Body: string(out)}, nil
}

// DeleteNodeQuotaHandler removes one policy row, restoring fallback for every
// axis it set.
// DELETE /compute-nodes/{id}/pipeline-quotas/{scope}
//
// Required Permissions:
// - Must be the owner of the compute node OR an org admin with manage access.
func DeleteNodeQuotaHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "DeleteNodeQuotaHandler"

	qctx, errResp := initNodeQuotaContext(ctx, request, handlerName, true)
	if errResp != nil {
		return *errResp, nil
	}

	if err := qctx.Store.Delete(ctx, qctx.NodeUuid, qctx.SK); err != nil {
		log.Printf("Error deleting node quota: %v", err)
		log.Printf("AUDIT action=delete_node_quota result=failure caller=%q node=%q scope=%q error=%q",
			qctx.CallerID, qctx.NodeUuid, qctx.Scope, err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	// Deleting policy loosens limits, so it is squarely audit-worthy.
	log.Printf("AUDIT action=delete_node_quota result=success caller=%q node=%q scope=%q",
		qctx.CallerID, qctx.NodeUuid, qctx.Scope)

	return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusNoContent}, nil
}

// effectivePipelineQuotaResponse is the resolved policy plus its per-axis
// source attribution. `configured: false` means the node imposes no pipeline
// ceilings at all, which is the normal state for a customer-owned node.
type effectivePipelineQuotaResponse struct {
	NodeUuid   string                `json:"nodeUuid"`
	UserId     string                `json:"userId,omitempty"`
	Configured bool                  `json:"configured"`
	Limits     quota.EffectiveLimits `json:"limits"`
}

// GetNodeEffectiveQuotaHandler returns the resolved pipeline limits for a scope,
// with per-axis source attribution.
// GET /compute-nodes/{id}/pipeline-quotas/{scope}/effective
//
// This is what the frontend reads to render a usage meter and to explain a
// blocked run, and what an operator uses to confirm an override took effect.
//
// Required Permissions:
// - `node` and `default` scopes: owner / org admin with manage access.
// - A user scope: that user (with access to the node), or an owner.
// `scope` may be the literal "me".
func GetNodeEffectiveQuotaHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetNodeEffectiveQuotaHandler"

	qctx, errResp := initNodeQuotaContext(ctx, request, handlerName, false)
	if errResp != nil {
		return *errResp, nil
	}

	// One query returns all three tiers.
	rows, err := qctx.Store.ListForNode(ctx, qctx.NodeUuid)
	if err != nil {
		log.Printf("Error listing node quotas: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	nodeRow, defaultRow, userRow := quota.SplitRows(rows, qctx.TargetUser)
	limits := quota.ResolvePipeline(nodeRow, defaultRow, userRow)

	out, _ := json.Marshal(effectivePipelineQuotaResponse{
		NodeUuid:   qctx.NodeUuid,
		UserId:     qctx.TargetUser,
		Configured: limits.AnyConfigured(),
		Limits:     limits,
	})
	return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusOK, Body: string(out)}, nil
}
