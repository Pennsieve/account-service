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
	"github.com/pennsieve/account-service/internal/mappers"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/service"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/utils"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

// GetComputesNodesHandler retrieves a list of compute nodes
// GET /compute-nodes
//
// Required Permissions:
// - Returns only nodes that the user has access to (owner, shared, workspace, or team)
// - When account_owner=true: Returns all nodes for accounts owned by the user
// - When organization_id specified: User must be a member of that organization, returns nodes within that organization the user has access to
func GetComputesNodesHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetComputesNodesHandler"

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

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	userId := claims.UserClaim.NodeId

	// Check query parameters
	organizationId := request.QueryStringParameters["organization_id"]
	accountOwnerMode := request.QueryStringParameters["account_owner"] == "true"

	// Set up stores for access control
	nodeAccessTable := os.Getenv("NODE_ACCESS_TABLE")
	if nodeAccessTable == "" {
		log.Println("NODE_ACCESS_TABLE environment variable not set")
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}

	nodeStore := store_dynamodb.NewNodeDatabaseStore(dynamoDBClient, computeNodesTable)
	var dynamoNodes []models.DynamoDBNode

	if accountOwnerMode {
		// Account owner mode: return ALL nodes on accounts owned by the user
		log.Println("Account owner mode: returning all nodes on user-owned accounts")

		// Get accounts table configuration
		accountsTable := os.Getenv("ACCOUNTS_TABLE")
		if accountsTable == "" {
			log.Println("ACCOUNTS_TABLE environment variable not set")
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
			}, nil
		}

		// Set up account store and get user's accounts
		accountStore := store_dynamodb.NewAccountDatabaseStore(dynamoDBClient, accountsTable)
		// Type assert to access GetByUserId method not in the interface
		accountStoreImpl, ok := accountStore.(*store_dynamodb.AccountDatabaseStore)
		if !ok {
			log.Println("Failed to cast account store to implementation type")
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
			}, nil
		}
		userAccounts, err := accountStoreImpl.GetByUserId(ctx, userId)
		if err != nil {
			log.Printf("Error getting user accounts: %v", err)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
			}, nil
		}

		// Check if user has any accounts (is an account owner)
		if len(userAccounts) == 0 {
			log.Printf("User %s is not an account owner", userId)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusForbidden,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrForbidden),
			}, nil
		}

		// Get ALL nodes on user's accounts, regardless of permissions
		for _, account := range userAccounts {
			accountNodes, err := nodeStore.GetByAccount(ctx, account.Uuid)
			if err != nil {
				log.Printf("Error fetching nodes for account %s: %v", account.Uuid, err)
				continue // Skip accounts that can't be fetched
			}
			dynamoNodes = append(dynamoNodes, accountNodes...)
		}

		log.Printf("Account owner mode: found %d nodes across %d accounts", len(dynamoNodes), len(userAccounts))
	} else {
		// Permission-based access control (existing logic)
		nodeAccessStore := store_dynamodb.NewNodeAccessDatabaseStore(dynamoDBClient, nodeAccessTable)
		permissionService := service.NewPermissionService(nodeAccessStore, nil)

		if organizationId == "" {
			// No organization_id provided: return nodes owned by the user
			log.Println("No organization_id provided, returning user-owned nodes")

			// Get all access records for this user
			userEntityId := models.FormatEntityId(models.EntityTypeUser, userId)
			userAccess, err := nodeAccessStore.GetEntityAccess(ctx, userEntityId)
			if err != nil {
				log.Printf("Error getting user access: %v", err)
				return events.APIGatewayV2HTTPResponse{
					StatusCode: http.StatusInternalServerError,
					Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
				}, nil
			}

			// Filter for owned nodes (organization-independent nodes where user is owner)
			for _, access := range userAccess {
				if access.AccessType == models.AccessTypeOwner && access.IsOrganizationIndependent() {
					node, err := nodeStore.GetById(ctx, access.NodeUuid)
					if err != nil {
						log.Printf("Error fetching node %s: %v", access.NodeUuid, err)
						continue // Skip nodes that can't be fetched
					}
					if node.Uuid != "" { // Only add non-empty nodes
						dynamoNodes = append(dynamoNodes, node)
					}
				}
			}
		} else {
			// Organization_id provided: return nodes accessible to user within that workspace
			log.Println("Organization_id provided:", organizationId, "returning accessible nodes in workspace")
			
			// Validate organization membership if organization ID is provided
			if validationResponse := utils.ValidateOrganizationMembership(ctx, cfg, userId, organizationId, handlerName); validationResponse != nil {
				return *validationResponse, nil
			}

			// Use existing GetAccessibleNodes method with the specified organization
			accessibleNodeUuids, err := permissionService.GetAccessibleNodes(ctx, userId, organizationId)
			if err != nil {
				log.Printf("Error getting accessible nodes: %v", err)
				return events.APIGatewayV2HTTPResponse{
					StatusCode: http.StatusInternalServerError,
					Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
				}, nil
			}

			// Filter nodes to only include those that belong to the specified organization
			for _, nodeUuid := range accessibleNodeUuids {
				node, err := nodeStore.GetById(ctx, nodeUuid)
				if err != nil {
					log.Printf("Error fetching node %s: %v", nodeUuid, err)
					continue // Skip nodes that can't be fetched
				}
				if node.Uuid != "" && node.OrganizationId == organizationId { // Only include nodes from the specified organization
					dynamoNodes = append(dynamoNodes, node)
				}
			}
		}
	}

	// Fetch account statuses for all nodes
	accountStatusMap := make(map[string]string)
	accountsTable := os.Getenv("ACCOUNTS_TABLE")
	if accountsTable != "" {
		accountStore := store_dynamodb.NewAccountDatabaseStore(dynamoDBClient, accountsTable)

		// Get unique account UUIDs
		accountUuids := make(map[string]bool)
		for _, node := range dynamoNodes {
			accountUuids[node.AccountUuid] = true
		}

		// Fetch account statuses
		for accountUuid := range accountUuids {
			account, err := accountStore.GetById(ctx, accountUuid)
			if err != nil {
				log.Printf("Warning: could not fetch account %s for status check: %v", accountUuid, err)
				// Skip this account if we can't fetch it
				continue
			}
			accountStatusMap[accountUuid] = account.Status
		}
	}

	// Apply account status override to nodes
	jsonNodes := mappers.DynamoDBNodeToJsonNodeWithAccountStatus(dynamoNodes, accountStatusMap)

	m, err := json.Marshal(jsonNodes)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMarshaling),
		}, nil
	}
	response := events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       string(m),
	}
	return response, nil
}
