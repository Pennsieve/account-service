package compute

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pennsieve/account-service/internal/errors"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/service"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/store_postgres"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

// UpdateNodePermissionsHandler updates the permissions for a compute node
// PATCH /compute-nodes/{id}/permissions
func UpdateNodePermissionsHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "UpdateNodePermissionsHandler"
	
	// Get node UUID from path
	nodeUuid := request.PathParameters["id"]
	if nodeUuid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMissingNodeUuid),
		}, nil
	}
	
	// Parse request body
	var permissionReq models.NodeAccessRequest
	if err := json.Unmarshal([]byte(request.Body), &permissionReq); err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrUnmarshaling),
		}, nil
	}
	
	// Get user claims
	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	userId := claims.UserClaim.NodeId
	organizationId := claims.OrgClaim.NodeId
	
	// Load AWS config
	cfg, err := config.LoadDefaultConfig(ctx)
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
	
	// Initialize PostgreSQL if available (for team lookups)
	var teamStore store_postgres.TeamStore
	if pgHost := os.Getenv("POSTGRES_HOST"); pgHost != "" {
		pgConnStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
			pgHost,
			os.Getenv("POSTGRES_PORT"),
			os.Getenv("POSTGRES_USER"),
			os.Getenv("POSTGRES_PASSWORD"),
			os.Getenv("POSTGRES_DB"),
		)
		db, err := sql.Open("postgres", pgConnStr)
		if err == nil {
			teamStore = store_postgres.NewPostgresTeamStore(db)
			defer db.Close()
		}
	}
	
	permissionService := service.NewPermissionService(nodeAccessStore, teamStore)
	
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
	
	// Only the owner can change permissions
	if node.UserId != userId {
		// Check if user has access to the node
		hasAccess, err := permissionService.CheckNodeAccess(ctx, userId, nodeUuid, organizationId)
		if err != nil || !hasAccess {
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusForbidden,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrForbidden),
			}, nil
		}
		
		// Even with access, only owner can change permissions
		if node.UserId != userId {
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusForbidden,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrOnlyOwnerCanChangePermissions),
			}, nil
		}
	}
	
	// Update permissions
	err = permissionService.SetNodePermissions(ctx, nodeUuid, permissionReq, node.UserId, organizationId, userId)
	if err != nil {
		log.Printf("error updating permissions: %v", err)
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

// GetNodePermissionsHandler gets the permissions for a compute node
// GET /compute-nodes/{id}/permissions
func GetNodePermissionsHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetNodePermissionsHandler"
	
	// Get node UUID from path
	nodeUuid := request.PathParameters["id"]
	if nodeUuid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMissingNodeUuid),
		}, nil
	}
	
	// Get user claims
	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	userId := claims.UserClaim.NodeId
	organizationId := claims.OrgClaim.NodeId
	
	// Load AWS config
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}
	
	// Initialize stores
	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	nodeAccessTable := os.Getenv("NODE_ACCESS_TABLE")
	nodeAccessStore := store_dynamodb.NewNodeAccessDatabaseStore(dynamoDBClient, nodeAccessTable)
	
	// Initialize PostgreSQL if available
	var teamStore store_postgres.TeamStore
	if pgHost := os.Getenv("POSTGRES_HOST"); pgHost != "" {
		pgConnStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
			pgHost,
			os.Getenv("POSTGRES_PORT"),
			os.Getenv("POSTGRES_USER"),
			os.Getenv("POSTGRES_PASSWORD"),
			os.Getenv("POSTGRES_DB"),
		)
		db, err := sql.Open("postgres", pgConnStr)
		if err == nil {
			teamStore = store_postgres.NewPostgresTeamStore(db)
			defer db.Close()
		}
	}
	
	permissionService := service.NewPermissionService(nodeAccessStore, teamStore)
	
	// Check if user has access to the node
	hasAccess, err := permissionService.CheckNodeAccess(ctx, userId, nodeUuid, organizationId)
	if err != nil {
		log.Printf("error checking access: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrCheckingAccess),
		}, nil
	}
	
	if !hasAccess {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrForbidden),
		}, nil
	}
	
	// Get permissions
	permissions, err := permissionService.GetNodePermissions(ctx, nodeUuid)
	if err != nil {
		log.Printf("error getting permissions: %v", err)
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