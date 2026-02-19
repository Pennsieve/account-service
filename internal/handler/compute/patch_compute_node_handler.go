package compute

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pennsieve/account-service/internal/errors"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/utils"
)

type ComputeNodeUpdateRequest struct {
	Status      *string `json:"status,omitempty"`
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

// PatchComputeNodeHandler updates compute node details (status, name, description)
// PATCH /compute-nodes/{id}
//
// Required Permissions:
// - Must be the owner of the compute node
//
// Restrictions:
// - Cannot change status of nodes in "Pending" state
// - Cannot enable nodes when the account is "Paused"
func PatchComputeNodeHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "PatchComputeNodeHandler"

	// Get compute node UUID from path
	nodeUuid := request.PathParameters["id"]
	if nodeUuid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMissingNodeUuid),
		}, nil
	}

	// Parse request body
	var updateReq ComputeNodeUpdateRequest
	if err := json.Unmarshal([]byte(request.Body), &updateReq); err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrUnmarshaling),
		}, nil
	}

	// Validate status value if provided
	if updateReq.Status != nil && *updateReq.Status != "Enabled" && *updateReq.Status != "Paused" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrInvalidStatus),
		}, nil
	}

	// Check that at least one field is being updated
	if updateReq.Status == nil && updateReq.Name == nil && updateReq.Description == nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrUnmarshaling),
		}, nil
	}

	userId, err := utils.GetUserIdFromRequest(request)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}

	cfg, err := utils.LoadAWSConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}

	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	computeNodesTable := os.Getenv("COMPUTE_NODES_TABLE")
	nodesStore := store_dynamodb.NewNodeDatabaseStore(dynamoDBClient, computeNodesTable)

	// Get the compute node
	node, err := nodesStore.GetById(ctx, nodeUuid)
	if err != nil {
		log.Printf("error getting compute node: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	// Check if node exists
	if node.Uuid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrNotFound),
		}, nil
	}

	// Only the owner can update the node
	if node.UserId != userId {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrForbidden),
		}, nil
	}

	// Check if node is in Pending status and trying to change status
	if node.Status == "Pending" && updateReq.Status != nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrNodePending),
		}, nil
	}

	// Check if trying to enable a node while account is paused
	if updateReq.Status != nil && *updateReq.Status == "Enabled" {
		accountsTable := os.Getenv("ACCOUNTS_TABLE")
		if accountsTable != "" {
			accountStore := store_dynamodb.NewAccountDatabaseStore(dynamoDBClient, accountsTable)
			account, err := accountStore.GetById(ctx, node.AccountUuid)
			if err != nil {
				log.Printf("Warning: could not fetch account %s for status check: %v", node.AccountUuid, err)
				// Continue if we can't fetch the account
			} else if account.Status == "Paused" {
				return events.APIGatewayV2HTTPResponse{
					StatusCode: http.StatusBadRequest,
					Body:       errors.ComputeHandlerError(handlerName, errors.ErrCannotEnableNodeAccountPaused),
				}, nil
			}
		}
	}

	// Update only the fields that were provided
	if updateReq.Status != nil {
		node.Status = *updateReq.Status
	}
	if updateReq.Name != nil {
		node.Name = *updateReq.Name
	}
	if updateReq.Description != nil {
		node.Description = *updateReq.Description
	}

	// Update the node in the database
	err = nodesStore.Put(ctx, node)
	if err != nil {
		log.Printf("error updating compute node: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	// Convert INDEPENDENT back to empty string for API response consistency
	responseOrganizationId := node.OrganizationId
	if node.OrganizationId == "INDEPENDENT" {
		responseOrganizationId = ""
	}

	// Return updated node
	m, err := json.Marshal(models.Node{
		Uuid:        node.Uuid,
		Name:        node.Name,
		Description: node.Description,
		QueueUrl:    node.QueueUrl,
		Account: models.NodeAccount{
			Uuid:        node.AccountUuid,
			AccountId:   node.AccountId,
			AccountType: node.AccountType,
		},
		CreatedAt:          node.CreatedAt,
		OrganizationId:     responseOrganizationId,
		OwnerId:            node.UserId,
		Identifier:         node.Identifier,
		WorkflowManagerTag: node.WorkflowManagerTag,
		DeploymentMode:     node.DeploymentMode,
		Status:             node.Status,
	})
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMarshaling),
		}, nil
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       string(m),
	}, nil
}
