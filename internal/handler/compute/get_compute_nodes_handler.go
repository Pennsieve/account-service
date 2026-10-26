package compute

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	authclient "github.com/pennsieve/account-service/internal/authorizer"
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

	// The newest provisioner release (Docker Hub, cached per warm container)
	// is looked up while the nodes are; annotateLatestVersions then reads it
	// from the cache.
	var versionLookup sync.WaitGroup
	versionLookup.Add(1)
	go func() {
		defer versionLookup.Done()
		getResolver(cfg).LatestVersion(ctx, defaultProvisionerImage)
	}()

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
		lambdaClient := lambda.NewFromConfig(cfg)
		permissionService := service.NewPermissionService(nodeAccessStore, nil)
		permissionService.SetAuthorizer(authclient.NewLambdaDirectAuthorizer(lambdaClient))

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
			var owned []string
			for _, access := range userAccess {
				if access.AccessType == models.AccessTypeOwner && access.IsOrganizationIndependent() {
					owned = append(owned, access.NodeUuid)
				}
			}
			dynamoNodes = fetchNodes(ctx, nodeStore, owned, func(models.DynamoDBNode) bool { return true })
		} else {
			// Organization_id provided: return nodes accessible to user within that workspace
			log.Println("Organization_id provided:", organizationId, "returning accessible nodes in workspace")
			
			// The membership check and the access lookup are independent
			// (neither writes), so they run together; a non-member is still
			// refused before anything is returned.
			var validationResponse *events.APIGatewayV2HTTPResponse
			var membership sync.WaitGroup
			membership.Add(1)
			go func() {
				defer membership.Done()
				validationResponse = utils.ValidateOrganizationMembership(ctx, cfg, userId, organizationId, handlerName)
			}()
			accessibleNodeUuids, err := permissionService.GetAccessibleNodes(ctx, userId, organizationId)
			membership.Wait()
			if validationResponse != nil {
				return *validationResponse, nil
			}
			if err != nil {
				log.Printf("Error getting accessible nodes: %v", err)
				return events.APIGatewayV2HTTPResponse{
					StatusCode: http.StatusInternalServerError,
					Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
				}, nil
			}

			// Only nodes that belong to the specified organization
			dynamoNodes = fetchNodes(ctx, nodeStore, accessibleNodeUuids, func(node models.DynamoDBNode) bool {
				return node.OrganizationNodeId() == organizationId
			})
		}
	}

	// Fetch account statuses and owners for all nodes
	accountStatusMap := make(map[string]string)
	accountOwnerMap := make(map[string]string)
	accountsTable := os.Getenv("ACCOUNTS_TABLE")
	if accountsTable != "" {
		accountStore := store_dynamodb.NewAccountDatabaseStore(dynamoDBClient, accountsTable)

		// Get unique account UUIDs
		accountUuids := make(map[string]bool)
		for _, node := range dynamoNodes {
			accountUuids[node.AccountUuid] = true
		}

		// Fetch account statuses and owners, all at once
		var mu sync.Mutex
		var accounts sync.WaitGroup
		for accountUuid := range accountUuids {
			accounts.Add(1)
			go func(accountUuid string) {
				defer accounts.Done()
				account, err := accountStore.GetById(ctx, accountUuid)
				if err != nil {
					log.Printf("Error: could not fetch account %s: %v", accountUuid, err)
					// Skip this account if we can't fetch it - this will result in missing accountOwnerId
					return
				}
				mu.Lock()
				accountStatusMap[accountUuid] = account.Status
				accountOwnerMap[accountUuid] = account.UserId
				mu.Unlock()
			}(accountUuid)
		}
		accounts.Wait()
		
		// Log errors for accounts that couldn't be fetched
		for _, node := range dynamoNodes {
			if _, found := accountOwnerMap[node.AccountUuid]; !found {
				log.Printf("Error: could not determine account owner for node %s (account: %s)", node.Uuid, node.AccountUuid)
			}
		}
	} else {
		log.Printf("Error: ACCOUNTS_TABLE environment variable not set")
		for _, node := range dynamoNodes {
			log.Printf("Error: could not determine account owner for node %s (account: %s) - ACCOUNTS_TABLE not configured", node.Uuid, node.AccountUuid)
		}
	}

	// Apply account status override and owner info to nodes
	jsonNodes := mappers.DynamoDBNodeToJsonNodeWithAccountInfo(dynamoNodes, accountStatusMap, accountOwnerMap, nil)

	// Annotate each node with the latest available provisioner version (from
	// Docker Hub) so the frontend can hint when a node is outdated, and report
	// the latest version at the root so new nodes can be pinned even when the
	// list is empty.
	versionLookup.Wait()
	latestVersion := annotateLatestVersions(ctx, cfg, jsonNodes)
	if jsonNodes == nil {
		jsonNodes = []models.Node{}
	}

	m, err := json.Marshal(models.NodesListResponse{Nodes: jsonNodes, LatestVersion: latestVersion})
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

// fetchNodeConcurrency bounds the node reads in flight for one list.
const fetchNodeConcurrency = 10

// fetchNodes reads the nodes by uuid, several at a time, in the uuids'
// order, keeping those that exist and pass keep. A node that can't be read is
// skipped.
func fetchNodes(ctx context.Context, store store_dynamodb.NodeStore, uuids []string, keep func(models.DynamoDBNode) bool) []models.DynamoDBNode {
	found := make([]models.DynamoDBNode, len(uuids))
	slots := make(chan struct{}, fetchNodeConcurrency)
	var wg sync.WaitGroup
	for i, uuid := range uuids {
		wg.Add(1)
		go func(i int, uuid string) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			node, err := store.GetById(ctx, uuid)
			if err != nil {
				log.Printf("Error fetching node %s: %v", uuid, err)
				return
			}
			found[i] = node
		}(i, uuid)
	}
	wg.Wait()
	var nodes []models.DynamoDBNode
	for _, node := range found {
		if node.Uuid != "" && keep(node) {
			nodes = append(nodes, node)
		}
	}
	return nodes
}
