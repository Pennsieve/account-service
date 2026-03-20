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
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	authclient "github.com/pennsieve/account-service/internal/authorizer"
	"github.com/pennsieve/account-service/internal/errors"
	"github.com/pennsieve/account-service/internal/runner"
	"github.com/pennsieve/account-service/internal/service"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/utils"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

// UpdateConfigRequest is the request body for POST /compute-nodes/{id}/update-config.
// Only infrastructure-affecting fields are accepted. Nil fields are left unchanged.
type UpdateConfigRequest struct {
	MaxGpuInstances *int    `json:"maxGpuInstances,omitempty"`
	GpuTier         *string `json:"gpuTier,omitempty"`
	EnableLLMAccess *bool   `json:"enableLLMAccess,omitempty"`
}

// PostUpdateConfigHandler updates infrastructure configuration for a compute node
// and triggers re-provisioning.
// POST /compute-nodes/{id}/update-config
//
// Required Permissions:
// - Must be the owner of the compute node OR the account owner
//
// Restrictions:
// - Cannot update nodes in "Pending" or "Destroying" state
func PostUpdateConfigHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "PostUpdateConfigHandler"

	nodeUuid := request.PathParameters["id"]
	if nodeUuid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMissingNodeUuid),
		}, nil
	}

	var req UpdateConfigRequest
	if err := json.Unmarshal([]byte(request.Body), &req); err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrUnmarshaling),
		}, nil
	}

	// Must provide at least one field
	if req.MaxGpuInstances == nil && req.GpuTier == nil && req.EnableLLMAccess == nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrUnmarshaling),
		}, nil
	}

	// Validate values
	if req.MaxGpuInstances != nil && *req.MaxGpuInstances < 0 {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrBadRequest),
		}, nil
	}
	validGpuTiers := map[string]bool{"small": true, "medium": true, "large": true, "xlarge": true}
	if req.GpuTier != nil && !validGpuTiers[*req.GpuTier] {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrBadRequest),
		}, nil
	}

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	userId := claims.UserClaim.NodeId

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

	computeNode, err := nodesStore.GetById(ctx, nodeUuid)
	if err != nil {
		log.Printf("error getting compute node: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}
	if computeNode.Uuid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrNotFound),
		}, nil
	}

	// Check node is in a state that allows config updates
	if computeNode.Status == "Pending" || computeNode.Status == "Destroying" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrNodePending),
		}, nil
	}

	// Permission check: node owner or account owner
	accountsTable := os.Getenv("ACCOUNTS_TABLE")
	accountStore := store_dynamodb.NewAccountDatabaseStore(dynamoDBClient, accountsTable)
	account, err := accountStore.GetById(ctx, computeNode.AccountUuid)
	if err != nil {
		log.Printf("Error fetching account %s: %v", computeNode.AccountUuid, err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	canUpdate := computeNode.UserId == userId ||
		((store_dynamodb.Account{}) != account && account.UserId == userId)

	if !canUpdate && computeNode.OrganizationId != "" && computeNode.OrganizationId != "INDEPENDENT" {
		lambdaClient := lambda.NewFromConfig(cfg)
		nodeAccessTable := os.Getenv("NODE_ACCESS_TABLE")
		accountWorkspaceTable := os.Getenv("ACCOUNT_WORKSPACE_TABLE")
		nodeAccessStore := store_dynamodb.NewNodeAccessDatabaseStore(dynamoDBClient, nodeAccessTable)
		permissionService := service.NewPermissionService(nodeAccessStore, nil)
		permissionService.SetAuthorizer(authclient.NewLambdaDirectAuthorizer(lambdaClient))
		permissionService.SetAccountWorkspaceStore(store_dynamodb.NewAccountWorkspaceStore(dynamoDBClient, accountWorkspaceTable))
		isAdmin, err := permissionService.IsAdminWithManageAccess(ctx, userId, computeNode.OrganizationId, computeNode.AccountUuid)
		if err == nil && isAdmin {
			canUpdate = true
		}
	}

	if !canUpdate {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrForbidden),
		}, nil
	}

	// Apply config changes
	if req.MaxGpuInstances != nil {
		computeNode.MaxGpuInstances = *req.MaxGpuInstances
	}
	if req.GpuTier != nil {
		computeNode.GpuTier = *req.GpuTier
	}
	if req.EnableLLMAccess != nil {
		computeNode.EnableLLMAccess = *req.EnableLLMAccess
	}

	// Save to DynamoDB
	if err := nodesStore.Put(ctx, computeNode); err != nil {
		log.Printf("error updating compute node config: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}
	log.Printf("Updated config for node %s: maxGpuInstances=%d, gpuTier=%s, enableLLMAccess=%v",
		nodeUuid, computeNode.MaxGpuInstances, computeNode.GpuTier, computeNode.EnableLLMAccess)

	// Trigger re-provisioning
	envValue := os.Getenv("ENV")
	if envValue == "DOCKER" || envValue == "TEST" {
		log.Println("Test environment detected, skipping re-provisioning")
		m, _ := json.Marshal(map[string]string{"message": "Config updated (re-provisioning skipped in test)"})
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusAccepted,
			Body:       string(m),
		}, nil
	}

	client := ecs.NewFromConfig(cfg)

	// Use the node's stored provisioner image for re-provisioning
	provisionerImage := computeNode.ProvisionerImage
	if provisionerImage == "" {
		provisionerImage = "pennsieve/compute-node-aws-provisioner-v2"
	}
	provisionerImageTag := computeNode.ProvisionerImageTag
	if provisionerImageTag == "" {
		provisionerImageTag = "latest"
	}

	dynamicTaskDef, err := createDynamicTaskDefinition(ctx, client, provisionerImage, provisionerImageTag, envValue)
	if err != nil {
		log.Printf("Error creating dynamic task definition: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrRunningFargateTask),
		}, nil
	}

	subIdStr := os.Getenv("SUBNET_IDS")
	subNetIds := strings.Split(subIdStr, ",")
	cluster := os.Getenv("CLUSTER_ARN")
	securityGroup := os.Getenv("SECURITY_GROUP")
	taskDefContainerName := os.Getenv("TASK_DEF_CONTAINER_NAME")

	nodeIdentifier := computeNode.Identifier
	if nodeIdentifier == "" {
		nodeIdentifier = nodeUuid[:8]
	}

	deploymentMode := computeNode.DeploymentMode
	if deploymentMode == "" {
		deploymentMode = "basic"
	}
	enableLLMAccess := "false"
	if computeNode.EnableLLMAccess {
		enableLLMAccess = "true"
	}

	env := buildEnvVars(map[string]string{
		"COMPUTE_NODE_ID":     nodeUuid,
		"ENV":                 envValue,
		"ACTION":              "UPDATE",
		"COMPUTE_NODES_TABLE": computeNodesTable,
		"ORG_ID":              computeNode.OrganizationId,
		"USER_ID":             userId,
		"ACCOUNT_ID":          computeNode.AccountId,
		"UUID":                computeNode.AccountUuid,
		"ACCOUNT_TYPE":        computeNode.AccountType,
		"WM_TAG":              computeNode.WorkflowManagerTag,
		"WM_CPU":              "2048",
		"WM_MEMORY":           "4096",
		"NODE_IDENTIFIER":     nodeIdentifier,
		"NODE_NAME":           computeNode.Name,
		"PROVISIONER_IMAGE":     provisionerImage,
		"PROVISIONER_IMAGE_TAG": provisionerImageTag,
		"DEPLOYMENT_MODE":     deploymentMode,
		"ENABLE_LLM_ACCESS":   enableLLMAccess,
		"MAX_GPU_INSTANCES":   strconv.Itoa(computeNode.MaxGpuInstances),
		"GPU_TIER":            computeNode.GpuTier,
		"ROLE_NAME":           account.RoleName,
	})

	runTaskIn := &ecs.RunTaskInput{
		TaskDefinition: dynamicTaskDef.TaskDefinitionArn,
		Cluster:        aws.String(cluster),
		NetworkConfiguration: &types.NetworkConfiguration{
			AwsvpcConfiguration: &types.AwsVpcConfiguration{
				Subnets:        subNetIds,
				SecurityGroups: []string{securityGroup},
				AssignPublicIp: types.AssignPublicIpEnabled,
			},
		},
		Overrides: &types.TaskOverride{
			ContainerOverrides: []types.ContainerOverride{
				{
					Name:        &taskDefContainerName,
					Environment: env,
				},
			},
		},
		LaunchType: types.LaunchTypeFargate,
	}

	taskRunner := runner.NewECSTaskRunner(client, runTaskIn)
	if err := taskRunner.Run(ctx); err != nil {
		log.Println(err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrRunningFargateTask),
		}, nil
	}

	m, _ := json.Marshal(map[string]string{"message": "Config updated, re-provisioning initiated"})
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusAccepted,
		Body:       string(m),
	}, nil
}

// buildEnvVars converts a string map to ECS KeyValuePair slice.
func buildEnvVars(vars map[string]string) []types.KeyValuePair {
	result := make([]types.KeyValuePair, 0, len(vars))
	for k, v := range vars {
		key := k
		val := v
		result = append(result, types.KeyValuePair{
			Name:  &key,
			Value: &val,
		})
	}
	return result
}