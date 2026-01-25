package compute

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/runner"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
	"github.com/pennsieve/account-service/internal/errors"
)

func PutComputeNodeHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "PutComputeNodeHandler"
	uuid := request.PathParameters["id"]

	var updateRequest models.NodeUpdateRequest
	if err := json.Unmarshal([]byte(request.Body), &updateRequest); err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrUnmarshaling),
		}, nil
	}

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}

	TaskDefinitionArn := os.Getenv("TASK_DEF_ARN")
	subIdStr := os.Getenv("SUBNET_IDS")
	SubNetIds := strings.Split(subIdStr, ",")
	cluster := os.Getenv("CLUSTER_ARN")
	SecurityGroup := os.Getenv("SECURITY_GROUP")
	envValue := os.Getenv("ENV")
	TaskDefContainerName := os.Getenv("TASK_DEF_CONTAINER_NAME")

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	organizationId := claims.OrgClaim.NodeId
	userId := claims.UserClaim.NodeId

	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	computeNodesTable := os.Getenv("COMPUTE_NODES_TABLE")

	dynamo_store := store_dynamodb.NewNodeDatabaseStore(dynamoDBClient, computeNodesTable)
	computeNode, err := dynamo_store.GetById(ctx, uuid)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrNoRecordsFound),
		}, nil
	}

	client := ecs.NewFromConfig(cfg)
	log.Println("Initiating new Provisioning Fargate Task for UPDATE.")

	computeNodeIdKey := "COMPUTE_NODE_ID"
	envKey := "ENV"
	organizationIdKey := "ORG_ID"
	organizationIdValue := organizationId
	userIdKey := "USER_ID"
	userIdValue := userId
	actionKey := "ACTION"
	actionValue := "UPDATE"
	tableKey := "COMPUTE_NODES_TABLE"
	tableValue := computeNodesTable
	accountIdKey := "ACCOUNT_ID"
	accountIdValue := computeNode.AccountId
	accountTypeKey := "ACCOUNT_TYPE"
	accountTypeValue := computeNode.AccountType
	accountUuidKey := "UUID"
	accountUuidValue := computeNode.AccountUuid

	// Update parameters from request
	wmTagKey := "WM_TAG"
	wmTagValue := updateRequest.WorkflowManagerTag
	if wmTagValue == "" {
		wmTagValue = computeNode.WorkflowManagerTag
	}

	wmCpuKey := "WM_CPU"
	wmCpuValue := strconv.Itoa(updateRequest.WorkflowManagerCpu)
	if updateRequest.WorkflowManagerCpu == 0 {
		wmCpuValue = "2048" // default value
	}

	wmMemoryKey := "WM_MEMORY"
	wmMemoryValue := strconv.Itoa(updateRequest.WorkflowManagerMemory)
	if updateRequest.WorkflowManagerMemory == 0 {
		wmMemoryValue = "4096" // default value
	}

	authTypeKey := "AUTH_TYPE"
	authTypeValue := updateRequest.AuthorizationType
	if authTypeValue == "" {
		authTypeValue = "NONE" // default value
	}

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
							Name:  &computeNodeIdKey,
							Value: &uuid,
						},
						{
							Name:  &envKey,
							Value: &envValue,
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
							Name:  &wmTagKey,
							Value: &wmTagValue,
						},
						{
							Name:  &wmCpuKey,
							Value: &wmCpuValue,
						},
						{
							Name:  &wmMemoryKey,
							Value: &wmMemoryValue,
						},
						{
							Name:  &authTypeKey,
							Value: &authTypeValue,
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

	m, err := json.Marshal(models.NodeResponse{
		Message: "Compute node update initiated",
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