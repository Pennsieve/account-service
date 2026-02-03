package internal

import (
	"context"
	"encoding/json"
	"log"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/service"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/store_postgres"
	"github.com/pennsieve/account-service/internal/utils"
)

// CheckUserNodeAccessRequest is the request structure for checking user access to a node
type CheckUserNodeAccessRequest struct {
	UserNodeId     string `json:"userNodeId"`     // User node ID in format "N:user:uuid"
	NodeUuid       string `json:"nodeUuid"`        // Compute node UUID
	OrganizationId string `json:"organizationId"`  // Organization node ID in format "N:organization:uuid"
}

// CheckUserNodeAccessResponse is the response structure for access check
type CheckUserNodeAccessResponse struct {
	HasAccess      bool   `json:"hasAccess"`
	AccessType     string `json:"accessType,omitempty"`     // "owner", "shared", "workspace", "team", or empty if no access
	AccessSource   string `json:"accessSource,omitempty"`   // "direct", "workspace", "team", or empty
	TeamId         string `json:"teamId,omitempty"`         // If access is through a team, which team
	NodeUuid       string `json:"nodeUuid"`
	UserNodeId     string `json:"userNodeId"`
	OrganizationId string `json:"organizationId"`
}

// CheckUserNodeAccessHandler is a private Lambda handler that checks if a user has access to a compute node
// This function is only invokable by other AWS services, not through API Gateway
//
// Input: CheckUserNodeAccessRequest (JSON)
// Output: CheckUserNodeAccessResponse (JSON)
//
// This handler is designed for internal service-to-service communication only.
// Authentication is handled through IAM roles and Lambda invocation permissions.
func CheckUserNodeAccessHandler(ctx context.Context, request CheckUserNodeAccessRequest) (CheckUserNodeAccessResponse, error) {
	log.Printf("CheckUserNodeAccessHandler: Checking access for user %s to node %s in org %s",
		request.UserNodeId, request.NodeUuid, request.OrganizationId)

	response := CheckUserNodeAccessResponse{
		HasAccess:      false,
		NodeUuid:       request.NodeUuid,
		UserNodeId:     request.UserNodeId,
		OrganizationId: request.OrganizationId,
	}

	// Validate input
	if request.UserNodeId == "" {
		log.Printf("CheckUserNodeAccessHandler: Missing userNodeId")
		return response, nil // Return false access, not an error
	}

	if request.NodeUuid == "" {
		log.Printf("CheckUserNodeAccessHandler: Missing nodeUuid")
		return response, nil // Return false access, not an error
	}

	// Load AWS config
	cfg, err := utils.LoadAWSConfig(ctx)
	if err != nil {
		log.Printf("CheckUserNodeAccessHandler: Error loading AWS config: %v", err)
		return response, err
	}

	// Initialize DynamoDB stores
	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	nodeAccessTable := os.Getenv("NODE_ACCESS_TABLE")
	nodeAccessStore := store_dynamodb.NewNodeAccessDatabaseStore(dynamoDBClient, nodeAccessTable)

	// Initialize container to get PostgreSQL connection for team lookups
	var teamStore store_postgres.TeamStore
	var orgStore store_postgres.OrganizationStore
	appContainer, err := utils.GetContainer(ctx, cfg)
	if err == nil && appContainer != nil {
		db := appContainer.PostgresDB()
		if db != nil {
			teamStore = store_postgres.NewPostgresTeamStore(db)
			orgStore = store_postgres.NewPostgresOrganizationStore(db)
		}
	}
	// Note: It's okay if stores are nil - it just means no team access checking

	// Initialize permission service
	permissionService := service.NewPermissionService(nodeAccessStore, teamStore)
	if orgStore != nil {
		permissionService.SetOrganizationStore(orgStore)
	}

	// Check access using the permission service
	hasAccess, err := permissionService.CheckNodeAccess(ctx, request.UserNodeId, request.NodeUuid, request.OrganizationId)
	if err != nil {
		log.Printf("CheckUserNodeAccessHandler: Error checking access: %v", err)
		return response, err
	}

	response.HasAccess = hasAccess

	// If user has access, get more details about the type of access
	if hasAccess {
		err = enrichAccessDetails(ctx, &response, nodeAccessStore, teamStore, orgStore)
		if err != nil {
			// Log but don't fail - we already know they have access
			log.Printf("CheckUserNodeAccessHandler: Error enriching access details: %v", err)
		}
	}

	log.Printf("CheckUserNodeAccessHandler: User %s has access=%v to node %s (type=%s, source=%s)",
		request.UserNodeId, response.HasAccess, request.NodeUuid, response.AccessType, response.AccessSource)

	return response, nil
}

// enrichAccessDetails adds information about how the user has access
func enrichAccessDetails(ctx context.Context, response *CheckUserNodeAccessResponse, 
	nodeAccessStore store_dynamodb.NodeAccessStore, teamStore store_postgres.TeamStore,
	orgStore store_postgres.OrganizationStore) error {
	
	nodeId := models.FormatNodeId(response.NodeUuid)
	userEntityId := models.FormatEntityId(models.EntityTypeUser, response.UserNodeId)
	
	// Check direct user access first
	hasDirectAccess, err := nodeAccessStore.HasAccess(ctx, userEntityId, nodeId)
	if err == nil && hasDirectAccess {
		// Get the actual access entry to determine if owner or shared
		accessList, err := nodeAccessStore.GetNodeAccess(ctx, response.NodeUuid)
		if err == nil {
			for _, access := range accessList {
				if access.EntityId == userEntityId {
					response.AccessType = string(access.AccessType)
					response.AccessSource = "direct"
					return nil
				}
			}
		}
	}
	
	// Check workspace access
	if response.OrganizationId != "" {
		workspaceEntityId := models.FormatEntityId(models.EntityTypeWorkspace, response.OrganizationId)
		hasWorkspaceAccess, err := nodeAccessStore.HasAccess(ctx, workspaceEntityId, nodeId)
		if err == nil && hasWorkspaceAccess {
			response.AccessType = string(models.AccessTypeWorkspace)
			response.AccessSource = "workspace"
			return nil
		}
	}
	
	// Check team access (if stores are available)
	if teamStore != nil && orgStore != nil && response.OrganizationId != "" {
		// Get numeric IDs from node IDs
		userIdInt, err := orgStore.GetUserIdByNodeId(ctx, response.UserNodeId)
		if err == nil {
			orgIdInt, err := orgStore.GetOrganizationIdByNodeId(ctx, response.OrganizationId)
			if err == nil {
				// Get all teams the user belongs to
				teams, err := teamStore.GetUserTeams(ctx, userIdInt, orgIdInt)
				if err == nil {
					// Check which team has access to the node
					for _, team := range teams {
						teamEntityId := models.FormatEntityId(models.EntityTypeTeam, team.TeamNodeId)
						hasTeamAccess, err := nodeAccessStore.HasAccess(ctx, teamEntityId, nodeId)
						if err == nil && hasTeamAccess {
							response.AccessType = string(models.AccessTypeShared)
							response.AccessSource = "team"
							response.TeamId = team.TeamNodeId
							return nil
						}
					}
				}
			}
		}
		
		// If we get here, we know they have team access but couldn't identify the specific team
		response.AccessType = string(models.AccessTypeShared)
		response.AccessSource = "team"
	}
	
	return nil
}

// LambdaHandler is the entry point for AWS Lambda
// It wraps CheckUserNodeAccessHandler to handle Lambda's event format
func LambdaHandler(ctx context.Context, event json.RawMessage) (CheckUserNodeAccessResponse, error) {
	var request CheckUserNodeAccessRequest
	
	// Try to unmarshal the event
	err := json.Unmarshal(event, &request)
	if err != nil {
		log.Printf("LambdaHandler: Error unmarshaling request: %v", err)
		return CheckUserNodeAccessResponse{}, err
	}
	
	return CheckUserNodeAccessHandler(ctx, request)
}

// APIGatewayHandler wraps the handler for API Gateway events (if needed in the future)
// This is included for completeness but should NOT be exposed through public API Gateway
func APIGatewayHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	var checkRequest CheckUserNodeAccessRequest
	
	// Parse request body
	if err := json.Unmarshal([]byte(request.Body), &checkRequest); err != nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: 400,
			Body:       `{"error": "Invalid request body"}`,
		}, nil
	}
	
	// Call the main handler
	response, err := CheckUserNodeAccessHandler(ctx, checkRequest)
	if err != nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: 500,
			Body:       `{"error": "Internal server error"}`,
		}, nil
	}
	
	// Marshal response
	responseBody, err := json.Marshal(response)
	if err != nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: 500,
			Body:       `{"error": "Error marshaling response"}`,
		}, nil
	}
	
	return events.APIGatewayV2HTTPResponse{
		StatusCode: 200,
		Body:       string(responseBody),
	}, nil
}