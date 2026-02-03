package compute

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pennsieve/account-service/internal/errors"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/service"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/store_postgres"
	"github.com/pennsieve/account-service/internal/utils"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

// SetNodeAccessScopeHandler sets the access scope for a compute node
// PUT /compute-nodes/{id}/permissions
//
// Required Permissions:
// - Must be the owner of the compute node
func SetNodeAccessScopeHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "SetNodeAccessScopeHandler"
	
	// Get node UUID from path
	nodeUuid := request.PathParameters["id"]
	if nodeUuid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMissingNodeUuid),
		}, nil
	}
	
	// Parse request body
	var scopeReq struct {
		AccessScope models.NodeAccessScope `json:"accessScope"`
	}
	if err := json.Unmarshal([]byte(request.Body), &scopeReq); err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrUnmarshaling),
		}, nil
	}
	
	// Validate access scope value
	switch scopeReq.AccessScope {
	case models.AccessScopePrivate, models.AccessScopeWorkspace, models.AccessScopeShared:
		// Valid scope, continue
	default:
		log.Printf("invalid access scope: %s", scopeReq.AccessScope)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrInvalidAccessScope),
		}, nil
	}
	
	// Get user claims
	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	userId := claims.UserClaim.NodeId
	
	// Load AWS config
	cfg, err := utils.LoadAWSConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}
	
	// Initialize stores
	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	nodesTable := os.Getenv("COMPUTE_NODES_TABLE")
	nodeAccessTable := os.Getenv("NODE_ACCESS_TABLE")
	
	nodesStore := store_dynamodb.NewNodeDatabaseStore(dynamoDBClient, nodesTable)
	nodeAccessStore := store_dynamodb.NewNodeAccessDatabaseStore(dynamoDBClient, nodeAccessTable)
	
	// Initialize container to get PostgreSQL connection
	appContainer, err := utils.GetContainer(ctx, cfg)
	if err != nil {
		log.Printf("Error getting container: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}

	db := appContainer.PostgresDB()
	if db == nil {
		log.Printf("PostgreSQL connection required but unavailable for permission operations (handler=%s, nodeId=%s)", 
			handlerName, nodeUuid)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}
	
	teamStore := store_postgres.NewPostgresTeamStore(db)
	orgStore := store_postgres.NewPostgresOrganizationStore(db)
	
	permissionService := service.NewPermissionService(nodeAccessStore, teamStore)
	permissionService.SetNodeStore(nodesStore)
	permissionService.SetOrganizationStore(orgStore)
	
	// Check if the node exists and user owns it
	node, err := nodesStore.GetById(ctx, nodeUuid)
	if err != nil {
		log.Printf("error getting node: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}
	
	if node.Uuid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrNotFound),
		}, nil
	}
	
	// Only the owner can change access scope
	if node.UserId != userId {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrOnlyOwnerCanChangePermissions),
		}, nil
	}
	
	// Create NodeAccessRequest for the new access scope
	accessRequest := models.NodeAccessRequest{
		NodeUuid:        nodeUuid,
		AccessScope:     scopeReq.AccessScope,
		SharedWithUsers: []string{}, // Empty when just setting scope
		SharedWithTeams: []string{}, // Empty when just setting scope
	}
	
	// Update access scope using the permission service to ensure owner access is preserved
	err = permissionService.SetNodePermissions(ctx, nodeUuid, accessRequest, node.UserId, node.OrganizationId, userId)
	if err != nil {
		log.Printf("error updating access scope: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrUpdatingPermissions),
		}, nil
	}
	
	// Get updated permissions
	permissions, err := permissionService.GetNodePermissions(ctx, nodeUuid)
	if err != nil {
		log.Printf("error getting updated permissions: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrGettingPermissions),
		}, nil
	}
	
	response, err := json.Marshal(permissions)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMarshaling),
		}, nil
	}
	
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       string(response),
	}, nil
}

// GrantUserAccessHandler grants access to a specific user
// POST /compute-nodes/{id}/permissions/users
//
// Required Permissions:
// - Must be the owner of the compute node
func GrantUserAccessHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GrantUserAccessHandler"
	
	// Get node UUID from path
	nodeUuid := request.PathParameters["id"]
	if nodeUuid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMissingNodeUuid),
		}, nil
	}
	
	// Parse request body
	var grantReq struct {
		UserId string `json:"userId"`
	}
	if err := json.Unmarshal([]byte(request.Body), &grantReq); err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrUnmarshaling),
		}, nil
	}
	
	if grantReq.UserId == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMissingUserId),
		}, nil
	}
	
	return grantEntityAccess(ctx, request, nodeUuid, models.EntityTypeUser, grantReq.UserId, handlerName)
}

// RevokeUserAccessHandler revokes access from a specific user
// DELETE /compute-nodes/{id}/permissions/users/{userId}
//
// Required Permissions:
// - Must be the owner of the compute node
// - Cannot revoke access from the node owner
func RevokeUserAccessHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "RevokeUserAccessHandler"
	
	// Get node UUID and user ID from path
	nodeUuid := request.PathParameters["id"]
	targetUserId := request.PathParameters["userId"]
	
	if nodeUuid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMissingNodeUuid),
		}, nil
	}
	
	if targetUserId == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMissingUserId),
		}, nil
	}
	
	return revokeEntityAccess(ctx, request, nodeUuid, models.EntityTypeUser, targetUserId, handlerName)
}

// GrantTeamAccessHandler grants access to a specific team
// POST /compute-nodes/{id}/permissions/teams
//
// Required Permissions:
// - Must be the owner of the compute node
// - Organization-independent nodes cannot share with teams
func GrantTeamAccessHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GrantTeamAccessHandler"
	
	// Get node UUID from path
	nodeUuid := request.PathParameters["id"]
	if nodeUuid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMissingNodeUuid),
		}, nil
	}
	
	// Parse request body
	var grantReq struct {
		TeamId string `json:"teamId"`
	}
	if err := json.Unmarshal([]byte(request.Body), &grantReq); err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrUnmarshaling),
		}, nil
	}
	
	if grantReq.TeamId == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMissingTeamId),
		}, nil
	}
	
	return grantEntityAccess(ctx, request, nodeUuid, models.EntityTypeTeam, grantReq.TeamId, handlerName)
}

// RevokeTeamAccessHandler revokes access from a specific team
// DELETE /compute-nodes/{id}/permissions/teams/{teamId}
//
// Required Permissions:
// - Must be the owner of the compute node
func RevokeTeamAccessHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "RevokeTeamAccessHandler"
	
	// Get node UUID and team ID from path
	nodeUuid := request.PathParameters["id"]
	targetTeamId := request.PathParameters["teamId"]
	
	if nodeUuid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMissingNodeUuid),
		}, nil
	}
	
	if targetTeamId == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMissingTeamId),
		}, nil
	}
	
	return revokeEntityAccess(ctx, request, nodeUuid, models.EntityTypeTeam, targetTeamId, handlerName)
}

// grantEntityAccess is a helper function to grant access to a user or team
func grantEntityAccess(ctx context.Context, request events.APIGatewayV2HTTPRequest, nodeUuid string, entityType models.EntityType, entityId, handlerName string) (events.APIGatewayV2HTTPResponse, error) {
	// Get user claims
	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	userId := claims.UserClaim.NodeId
	organizationId := claims.OrgClaim.NodeId
	
	// Load AWS config
	cfg, err := utils.LoadAWSConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}
	
	// Initialize stores
	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	nodesTable := os.Getenv("COMPUTE_NODES_TABLE")
	nodeAccessTable := os.Getenv("NODE_ACCESS_TABLE")
	
	nodesStore := store_dynamodb.NewNodeDatabaseStore(dynamoDBClient, nodesTable)
	nodeAccessStore := store_dynamodb.NewNodeAccessDatabaseStore(dynamoDBClient, nodeAccessTable)
	
	// Check if the node exists and user owns it
	node, err := nodesStore.GetById(ctx, nodeUuid)
	if err != nil {
		log.Printf("error getting node: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}
	
	if node.Uuid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrNotFound),
		}, nil
	}
	
	// Only the owner can grant access
	if node.UserId != userId {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrOnlyOwnerCanChangePermissions),
		}, nil
	}
	
	// Check if organization-independent nodes cannot be shared with teams
	if node.OrganizationId == "" && entityType == models.EntityTypeTeam {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrOrganizationIndependentNodeCannotBeShared),
		}, nil
	}

	// Initialize container to get PostgreSQL connection
	appContainer, err := utils.GetContainer(ctx, cfg)
	if err != nil {
		log.Printf("Error getting container: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}

	db := appContainer.PostgresDB()
	if db == nil {
		log.Printf("PostgreSQL connection required but unavailable for permission operations (handler=%s, nodeId=%s)", 
			handlerName, nodeUuid)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}

	// Validate entity exists before granting access
	if entityType == models.EntityTypeUser {
		// Validate user exists using node ID format (e.g., "N:user:uuid")
		orgStore := store_postgres.NewPostgresOrganizationStore(db)
		userExists, err := orgStore.CheckUserExistsByNodeId(ctx, entityId)
		if err != nil {
			log.Printf("Error checking if user exists: %v", err)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
			}, nil
		}
		
		if !userExists {
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusBadRequest,
				Body:       fmt.Sprintf("{\"error\": \"User %s does not exist\"}", entityId),
			}, nil
		}
	} else if entityType == models.EntityTypeTeam {
		// Validate team exists using node ID format (e.g., "N:team:uuid")
		teamStore := store_postgres.NewPostgresTeamStore(db)
		team, err := teamStore.GetTeamByNodeId(ctx, entityId)
		if err != nil {
			log.Printf("Error checking if team exists: %v", err)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
			}, nil
		}
		
		if team == nil {
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusBadRequest,
				Body:       fmt.Sprintf("{\"error\": \"Team %s does not exist\"}", entityId),
			}, nil
		}
	}
	
	// Create the access entry
	nodeId := models.FormatNodeId(nodeUuid)
	entityFormattedId := models.FormatEntityId(entityType, entityId)
	
	access := models.NodeAccess{
		EntityId:       entityFormattedId,
		NodeId:         nodeId,
		EntityType:     entityType,
		EntityRawId:    entityId,
		NodeUuid:       nodeUuid,
		AccessType:     models.AccessTypeShared,
		OrganizationId: organizationId,
		GrantedBy:      userId,
	}
	
	// Grant access
	err = nodeAccessStore.GrantAccess(ctx, access)
	if err != nil {
		log.Printf("error granting access: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrGrantingAccess),
		}, nil
	}
	
	// Prepare response
	actionResponse := struct {
		Message    string `json:"message"`
		NodeUuid   string `json:"nodeUuid"`
		Action     string `json:"action"`
		EntityType string `json:"entityType"`
		EntityId   string `json:"entityId"`
	}{
		Message:    fmt.Sprintf("Successfully granted access to %s %s", entityType, entityId),
		NodeUuid:   nodeUuid,
		Action:     "granted",
		EntityType: string(entityType),
		EntityId:   entityId,
	}
	
	response, err := json.Marshal(actionResponse)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMarshaling),
		}, nil
	}
	
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusCreated,
		Body:       string(response),
	}, nil
}

// revokeEntityAccess is a helper function to revoke access from a user or team
func revokeEntityAccess(ctx context.Context, request events.APIGatewayV2HTTPRequest, nodeUuid string, entityType models.EntityType, entityId, handlerName string) (events.APIGatewayV2HTTPResponse, error) {
	// Get user claims
	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	userId := claims.UserClaim.NodeId
	
	// Load AWS config
	cfg, err := utils.LoadAWSConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}
	
	// Initialize stores
	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	nodesTable := os.Getenv("COMPUTE_NODES_TABLE")
	nodeAccessTable := os.Getenv("NODE_ACCESS_TABLE")
	
	nodesStore := store_dynamodb.NewNodeDatabaseStore(dynamoDBClient, nodesTable)
	nodeAccessStore := store_dynamodb.NewNodeAccessDatabaseStore(dynamoDBClient, nodeAccessTable)
	
	// Check if the node exists and user owns it
	node, err := nodesStore.GetById(ctx, nodeUuid)
	if err != nil {
		log.Printf("error getting node: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}
	
	if node.Uuid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrNotFound),
		}, nil
	}
	
	// Only the owner can revoke access
	if node.UserId != userId {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrOnlyOwnerCanChangePermissions),
		}, nil
	}
	
	// Cannot revoke access from the owner
	if entityType == models.EntityTypeUser && entityId == node.UserId {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrCannotRevokeOwnerAccess),
		}, nil
	}
	
	// Revoke access
	nodeId := models.FormatNodeId(nodeUuid)
	entityFormattedId := models.FormatEntityId(entityType, entityId)
	
	err = nodeAccessStore.RevokeAccess(ctx, entityFormattedId, nodeId)
	if err != nil {
		log.Printf("error revoking access: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrRevokingAccess),
		}, nil
	}
	
	// Prepare response
	actionResponse := struct {
		Message    string `json:"message"`
		NodeUuid   string `json:"nodeUuid"`
		Action     string `json:"action"`
		EntityType string `json:"entityType"`
		EntityId   string `json:"entityId"`
	}{
		Message:    fmt.Sprintf("Successfully revoked access from %s %s", entityType, entityId),
		NodeUuid:   nodeUuid,
		Action:     "revoked",
		EntityType: string(entityType),
		EntityId:   entityId,
	}
	
	response, err := json.Marshal(actionResponse)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMarshaling),
		}, nil
	}
	
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       string(response),
	}, nil
}