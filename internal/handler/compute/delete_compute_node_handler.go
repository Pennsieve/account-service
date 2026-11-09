package compute

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"

	"fmt"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/pennsieve/account-service/internal/errors"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/noderecords"
	"github.com/pennsieve/account-service/internal/roleaccess"
	"github.com/pennsieve/account-service/internal/runner"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/utils"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

// DeleteComputeNodeHandler deletes a compute node
// DELETE /compute-nodes/{id}
//
// Required Permissions:
// - Must be the owner of the compute node OR
// - Must be the owner of the account associated with the compute node
func DeleteComputeNodeHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "DeleteComputeNodeHandler"
	uuid := request.PathParameters["id"]

	cfg, err := utils.LoadAWSConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}

	subIdStr := os.Getenv("SUBNET_IDS")
	SubNetIds := strings.Split(subIdStr, ",")
	cluster := os.Getenv("CLUSTER_ARN")
	SecurityGroup := os.Getenv("SECURITY_GROUP")
	envValue := os.Getenv("ENV")
	TaskDefContainerName := os.Getenv("TASK_DEF_CONTAINER_NAME")

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	userId := claims.UserClaim.NodeId

	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	computeNodesTable := os.Getenv("COMPUTE_NODES_TABLE")

	dynamo_store := store_dynamodb.NewNodeDatabaseStore(dynamoDBClient, computeNodesTable)
	computeNode, err := dynamo_store.GetById(ctx, uuid)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}
	if (models.DynamoDBNode{}) == computeNode {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrNoRecordsFound),
		}, nil
	}

	// Load account (needed for permission check and RoleName for Fargate task)
	accountsTable := os.Getenv("ACCOUNTS_TABLE")
	if accountsTable == "" {
		log.Println("ACCOUNTS_TABLE environment variable not set")
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}

	accountStore := store_dynamodb.NewAccountDatabaseStore(dynamoDBClient, accountsTable)
	account, err := accountStore.GetById(ctx, computeNode.AccountUuid)
	if err != nil {
		log.Printf("Error fetching account %s: %v", computeNode.AccountUuid, err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	// Check delete permissions: node owner, account owner, or org admin (if IsPublic) can delete
	canDelete := computeNode.UserId == userId ||
		((store_dynamodb.Account{}) != account && account.UserId == userId)

	if !canDelete {
		canDelete = checkAdminManageAccess(ctx, cfg, dynamoDBClient, userId, computeNode.OrganizationNodeId(), computeNode.AccountUuid)
	}

	if !canDelete {
		log.Printf("User %s does not have permission to delete node %s (node owner: %s)", userId, uuid, computeNode.UserId)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrForbidden),
		}, nil
	}

	// Validate provisioner image against whitelist from SSM (empty passes through)
	if err := validateProvisionerImage(ctx, cfg, computeNode.ProvisionerImage); err != nil {
		log.Printf("Invalid provisioner image: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrBadRequest),
		}, nil
	}

	// Teardown uses the LATEST provisioner image, not the node's pinned
	// provision-time tag. Pinning exists for reproducible PROVISIONING; DELETE
	// wants the newest teardown logic so fixes (shared-VPC resolution, ENI
	// draining, interactive-task reaping) apply to EVERY existing node regardless
	// of when it was created — otherwise an old-pinned node carries its
	// provisioning-era delete bugs forever and can't be torn down without a manual
	// tag bump. Overridable via PROVISIONER_TEARDOWN_IMAGE_TAG to pin a known-good
	// teardown build. The image repo still comes from the node (custom repos).
	provisionerImage := computeNode.ProvisionerImage
	if provisionerImage == "" {
		provisionerImage = "pennsieve/compute-node-aws-provisioner-v2"
	}
	provisionerImageTag := os.Getenv("PROVISIONER_TEARDOWN_IMAGE_TAG")
	if provisionerImageTag == "" {
		provisionerImageTag = "latest"
	}

	// When the account's role was removed (or no longer trusts Pennsieve), the
	// provisioner can't assume it, so a teardown would only fail and leave the
	// node Failed. Remove Pennsieve's records instead and say what may remain
	// in the AWS account. The active-work gate below doesn't apply: without the
	// role nothing can be started on the node, and what may still run there is
	// the customer's to stop. A check that fails for another reason
	// (throttling, network) carries on as before.
	//
	// Only for connections made in the web app (CloudFormation): their
	// registration or upgrade proved this service can assume the role, so a
	// refusal now means the role was removed or changed. An older role may
	// trust only the provisioner, not this service; for it a refusal proves
	// nothing, and the teardown runs as before.
	if (store_dynamodb.Account{}) != account && account.Connection == models.ConnectionCloudFormation {
		usability, cerr := roleaccess.Check(ctx, roleaccess.NewAssumer(cfg), account.AccountId, account.RoleName, account.ExternalId)
		if usability == roleaccess.Unusable {
			log.Printf("Role %s of account %s is unusable (%v): removing node %s without a teardown", account.RoleName, account.Uuid, cerr, uuid)
			if err := noderecords.Remove(ctx, cfg, dynamoDBClient, dynamo_store, uuid); err != nil {
				log.Println(err.Error())
				return events.APIGatewayV2HTTPResponse{
					StatusCode: http.StatusInternalServerError,
					Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
				}, nil
			}
			body, _ := json.Marshal(roleaccess.RemovedNote{
				Message:            "Pennsieve can no longer use this account's role, so it removed the compute node without tearing it down. Its resources may remain in the AWS account.",
				ResourcesMayRemain: true,
				RunbookUrl:         roleaccess.RunbookURL,
			})
			return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusOK, Body: string(body)}, nil
		}
		if usability == roleaccess.Unknown && cerr != nil {
			log.Printf("Warning: could not check role %s of account %s (%v); deleting as usual", account.RoleName, account.Uuid, cerr)
		}
	}

	// Gate: refuse to delete a node with live work — running workflows OR
	// interactive sessions (both are Step Functions executions). The authoritative
	// count comes from the node's gateway /health. If it's unknown (old or
	// unreachable gateway) we proceed and rely on the provisioner-side delete()
	// active-execution check as the backstop.
	if active, known := activeExecutionsViaGateway(ctx, computeNode.ComputeNodeGatewayUrl, cfg.Region, cfg); known && active > 0 {
		log.Printf("Refusing delete of node %s: %d active execution(s) reported by gateway", uuid, active)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusConflict,
			Body:       errors.ComputeHandlerError(handlerName, fmt.Errorf("compute node has %d active workflow execution(s) or interactive session(s) — cancel or wait for them to finish before deleting", active)),
		}, nil
	}

	// Set node status to "Destroying" before launching the delete task
	computeNode.Status = "Destroying"
	err = dynamo_store.Put(ctx, computeNode)
	if err != nil {
		log.Printf("Error updating node status to Destroying: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}
	log.Printf("Set node %s status to Destroying", uuid)

	// In test environment, skip ECS task execution and return mock response
	if envValue == "DOCKER" || envValue == "TEST" {
		log.Println("Test environment detected, skipping ECS task execution")

		m, err := json.Marshal(models.NodeResponse{
			Message: "Compute node deletion initiated",
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

	client := ecs.NewFromConfig(cfg)
	log.Printf("Initiating delete Fargate Task with image: %s:%s", provisionerImage, provisionerImageTag)

	// Create a dynamic task definition with the provisioner image
	dynamicTaskDef, err := createDynamicTaskDefinition(ctx, cfg, client, provisionerImage, provisionerImageTag, envValue)
	if err != nil {
		log.Printf("Error creating dynamic task definition: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrRunningFargateTask),
		}, nil
	}
	TaskDefinitionArn := *dynamicTaskDef.TaskDefinitionArn

	computeNodeIdKey := "COMPUTE_NODE_ID"
	envKey := "ENV"
	organizationIdKey := "ORG_ID"
	organizationIdValue := computeNode.OrganizationId
	userIdKey := "USER_ID"
	userIdValue := userId
	actionKey := "ACTION"
	actionValue := "DELETE"
	tableKey := "COMPUTE_NODES_TABLE"
	tableValue := computeNodesTable
	accountIdKey := "ACCOUNT_ID"
	accountIdValue := computeNode.AccountId
	accountTypeKey := "ACCOUNT_TYPE"
	accountTypeValue := computeNode.AccountType
	accountUuidKey := "UUID"
	accountUuidValue := computeNode.AccountUuid
	wmTagKey := "WM_TAG"
	wmTagValue := computeNode.WorkflowManagerTag
	nodeIdentifierKey := "NODE_IDENTIFIER"
	nodeIdentifierValue := computeNode.Identifier
	// For nodes that don't have a stored Identifier, derive from UUID
	if nodeIdentifierValue == "" {
		nodeIdentifierValue = uuid[:8]
	}
	nodeNameKey := "NODE_NAME"
	nodeNameValue := computeNode.Name
	roleNameKey := "ROLE_NAME"
	roleNameValue := account.RoleName
	externalIdKey := "EXTERNAL_ID"
	externalIdValue := account.ExternalId
	// DEPLOYMENT_MODE must be forwarded on DELETE too: the provisioner gates its
	// shared-infrastructure teardown (last-node VPC/NAT destroy AND the
	// DESTROY_INTERACTIVE interactive teardown below) on deployment_mode=="secure".
	// Without this the provisioner defaults to "basic" and skips that teardown
	// entirely, leaking the shared ALB/zone/cert (and VPC/NAT on the last node).
	deploymentModeKey := "DEPLOYMENT_MODE"
	deploymentModeValue := computeNode.DeploymentMode
	if deploymentModeValue == "" {
		deploymentModeValue = "basic"
	}

	// If this node is interactive and it's the LAST interactive node in the
	// account, tell the provisioner to tear down the shared interactive infra
	// (ALB + zone + cert + listener) on this same delete run. When other nodes
	// remain the provisioner keeps the shared VPC/NAT; when this is the last node
	// overall, its full shared destroy removes everything regardless of this flag.
	// Fail safe: on any uncertainty, leave it false (never wrongly destroy infra
	// another node depends on).
	destroyInteractiveKey := "DESTROY_INTERACTIVE"
	destroyInteractiveValue := "false"
	if computeNode.MaxInteractiveSessions > 0 {
		otherInteractive, err := accountHasInteractiveExcept(ctx, accountStore, dynamo_store, account.AccountId, computeNode.Uuid)
		if err != nil {
			log.Printf("Warning: could not determine remaining interactive nodes for AWS account %s; not tearing down interactive infra: %v", account.AccountId, err)
		} else if !otherInteractive {
			destroyInteractiveValue = "true"
		}
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
							Name:  &nodeIdentifierKey,
							Value: &nodeIdentifierValue,
						},
						{
							Name:  &nodeNameKey,
							Value: &nodeNameValue,
						},
						{
							Name:  &roleNameKey,
							Value: &roleNameValue,
						},
						{
							Name:  &externalIdKey,
							Value: &externalIdValue,
						},
						{
							Name:  &deploymentModeKey,
							Value: &deploymentModeValue,
						},
						{
							Name:  &destroyInteractiveKey,
							Value: &destroyInteractiveValue,
						},
					},
				},
			},
		},
		LaunchType: types.LaunchTypeFargate,
	}

	ecsRunner := runner.NewECSTaskRunner(client, runTaskIn)
	if err := ecsRunner.Run(ctx); err != nil {
		log.Println(err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: 500,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrRunningFargateTask),
		}, nil
	}

	m, err := json.Marshal(models.NodeResponse{
		Message: "Compute node deletion initiated",
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
