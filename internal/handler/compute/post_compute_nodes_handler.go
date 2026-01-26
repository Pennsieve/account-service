package compute

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/google/uuid"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/runner"
	"github.com/pennsieve/account-service/internal/service"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
	"github.com/pennsieve/account-service/internal/errors"
)

func PostComputeNodesHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "PostComputeNodesHandler"
	var node models.Node
	if err := json.Unmarshal([]byte(request.Body), &node); err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrUnmarshaling),
		}, nil
	}

	envValue := os.Getenv("ENV") // this is either DEV or PROD

	TaskDefinitionArn := os.Getenv("TASK_DEF_ARN")
	subIdStr := os.Getenv("SUBNET_IDS")
	SubNetIds := strings.Split(subIdStr, ",")
	cluster := os.Getenv("CLUSTER_ARN")
	SecurityGroup := os.Getenv("SECURITY_GROUP")

	TaskDefContainerName := os.Getenv("TASK_DEF_CONTAINER_NAME")

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	organizationId := claims.OrgClaim.NodeId
	userId := claims.UserClaim.NodeId

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}

	client := ecs.NewFromConfig(cfg)
	log.Println("Initiating new Provisioning Fargate Task.")
	envKey := "ENV"
	accountIdKey := "ACCOUNT_ID"
	accountIdValue := node.Account.AccountId
	accountTypeKey := "ACCOUNT_TYPE"
	accountTypeValue := node.Account.AccountType
	accountUuidKey := "ACCOUNT_UUID"
	accountUuidValue := node.Account.Uuid
	organizationIdKey := "ORG_ID"
	organizationIdValue := organizationId
	userIdKey := "USER_ID"
	userIdValue := userId
	actionKey := "ACTION"
	actionValue := "CREATE"
	tableKey := "COMPUTE_NODES_TABLE"
	tableValue := os.Getenv("COMPUTE_NODES_TABLE")
	nodeNameKey := "NODE_NAME"
	nodeDescriptionKey := "NODE_DESCRIPTION"
	nameValue := node.Name
	descriptionValue := node.Description
	wmTagKey := "WM_TAG"
	wmTagValue := node.WorkflowManagerTag
	statusKey := "STATUS"
	statusValue := "Enabled" // Default status for new compute nodes
	
	// Generate a UUID for the new node
	nodeUuid := uuid.New().String()
	nodeUuidKey := "NODE_UUID"
	nodeUuidValue := nodeUuid

	runTaskIn := &ecs.RunTaskInput{
		TaskDefinition: aws.String(TaskDefinitionArn),
		Cluster:        aws.String(cluster),
		NetworkConfiguration: &types.NetworkConfiguration{
			AwsvpcConfiguration: &types.AwsVpcConfiguration{
				Subnets:        SubNetIds,
				SecurityGroups: []string{SecurityGroup},
				AssignPublicIp: types.AssignPublicIpEnabled,
			},
		},
		Overrides: &types.TaskOverride{
			ContainerOverrides: []types.ContainerOverride{
				{
					Name: &TaskDefContainerName,
					Environment: []types.KeyValuePair{
						{
							Name:  &envKey,
							Value: &envValue,
						},
						{
							Name:  &nodeNameKey,
							Value: &nameValue,
						},
						{
							Name:  &nodeDescriptionKey,
							Value: &descriptionValue,
						},
						{
							Name:  &accountIdKey,
							Value: &accountIdValue,
						},
						{
							Name:  &accountUuidKey,
							Value: &accountUuidValue,
						},
						{
							Name:  &accountTypeKey,
							Value: &accountTypeValue,
						},
						{
							Name:  &actionKey,
							Value: &actionValue,
						},
						{
							Name:  &tableKey,
							Value: &tableValue,
						},
						{
							Name:  &organizationIdKey,
							Value: &organizationIdValue,
						},
						{
							Name:  &userIdKey,
							Value: &userIdValue,
						},
						{
							Name:  &wmTagKey,
							Value: &wmTagValue,
						},
						{
							Name:  &statusKey,
							Value: &statusValue,
						},
						{
							Name:  &nodeUuidKey,
							Value: &nodeUuidValue,
						},
					},
				},
			},
		},
		LaunchType: types.LaunchTypeFargate,
	}

	runner := runner.NewECSTaskRunner(client, runTaskIn)
	if err := runner.Run(ctx); err != nil {
		log.Println(err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: 500,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrRunningFargateTask),
		}, nil
	}
	
	// Set up initial permissions for the node
	// Default to private visibility (only owner can access)
	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	nodeAccessTable := os.Getenv("NODE_ACCESS_TABLE")
	if nodeAccessTable != "" {
		nodeAccessStore := store_dynamodb.NewNodeAccessDatabaseStore(dynamoDBClient, nodeAccessTable)
		permissionService := service.NewPermissionService(nodeAccessStore, nil) // No PostgreSQL in Lambda for now
		
		// Set initial permissions - private by default
		permissionReq := models.NodeAccessRequest{
			NodeUuid:    nodeUuid,
			AccessScope: models.AccessScopePrivate,
		}
		
		err = permissionService.SetNodePermissions(ctx, nodeUuid, permissionReq, userId, organizationId, userId)
		if err != nil {
			log.Printf("Warning: Failed to set initial permissions for node %s: %v", nodeUuid, err)
			// Don't fail the request, but log the error
		}
	}

	m, err := json.Marshal(models.NodeResponse{
		Message: "Compute node creation initiated",
	})
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: 500,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMarshaling),
		}, nil
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusAccepted,
		Body:       string(m),
	}, nil
}