package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/pennsieve/account-service/internal/errors"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/runner"
	"github.com/pennsieve/account-service/internal/service"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/utils"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

// DeleteStorageNodeHandler deletes a storage node
// DELETE /storage-nodes/{id}
//
// For provisioned nodes (status was Pending→Enabled via provisioner), launches a Fargate destroy task.
// For registered nodes (skipProvisioning=true), deletes the DynamoDB record directly.
func DeleteStorageNodeHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "DeleteStorageNodeHandler"
	nodeId := request.PathParameters["id"]

	if nodeId == "" {
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
	storageNodesTable := os.Getenv("STORAGE_NODES_TABLE")
	storageNodeStore := store_dynamodb.NewStorageNodeDatabaseStore(dynamoDBClient, storageNodesTable)

	node, err := storageNodeStore.GetById(ctx, nodeId)
	if err != nil {
		log.Printf("Error getting storage node: %v", err)
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

	// Only account owner can delete storage nodes
	accountsTable := os.Getenv("ACCOUNTS_TABLE")
	accountStore := store_dynamodb.NewAccountDatabaseStore(dynamoDBClient, accountsTable)
	account, err := accountStore.GetById(ctx, node.AccountUuid)
	if err != nil {
		log.Printf("Error getting account: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	if account.UserId != userId {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrForbidden),
		}, nil
	}

	// Safety check: block delete if workspaces are still attached unless force=true.
	// The caller should detach workspaces explicitly (or confirm the blast radius) before destroying the bucket.
	force := request.QueryStringParameters["force"] == "true"
	storageNodeWorkspaceTable := os.Getenv("STORAGE_NODE_WORKSPACE_TABLE")
	var attachedWorkspaces []models.DynamoDBStorageNodeWorkspace
	if storageNodeWorkspaceTable != "" {
		wsStore := store_dynamodb.NewStorageNodeWorkspaceStore(dynamoDBClient, storageNodeWorkspaceTable)
		attachedWorkspaces, err = wsStore.GetByStorageNode(ctx, nodeId)
		if err != nil {
			log.Printf("Error getting workspace associations: %v", err)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
			}, nil
		}
		if len(attachedWorkspaces) > 0 && !force {
			body, _ := json.Marshal(map[string]interface{}{
				"error":           errors.ErrStorageNodeHasAttachments.Error(),
				"attachmentCount": len(attachedWorkspaces),
			})
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusConflict,
				Body:       string(body),
			}, nil
		}
	}

	// Safety check: for S3 nodes, require the bucket to have the
	// "pennsieve:allow-delete" = "true" tag before proceeding.
	// This forces the account owner to take a manual action in AWS
	// as confirmation before the bucket can be deregistered/destroyed.
	envValue := os.Getenv("ENV")
	if node.ProviderType == "s3" && envValue != "DOCKER" && envValue != "TEST" {
		hasDeleteTag, err := checkDeleteTag(ctx, cfg, account, node)
		if err != nil {
			log.Printf("Error checking delete tag on bucket %s: %v", node.StorageLocation, err)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrCheckingAccess),
			}, nil
		}
		if !hasDeleteTag {
			log.Printf("Bucket %s missing pennsieve:allow-delete tag", node.StorageLocation)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusConflict,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrDeleteTagRequired),
			}, nil
		}
	}

	storageTaskDefArn := os.Getenv("STORAGE_TASK_DEF_ARN")
	needsProvisioning := node.ProviderType == "s3" && storageTaskDefArn != "" && envValue != "DOCKER" && envValue != "TEST"

	if needsProvisioning {
		// Set status to Destroying and launch Fargate destroy task
		node.Status = "Destroying"
		err = storageNodeStore.Put(ctx, node)
		if err != nil {
			log.Printf("Error updating storage node status: %v", err)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
			}, nil
		}
		log.Printf("Set storage node %s status to Destroying", nodeId)

		if err := launchStorageDestroyTask(ctx, cfg, account, node); err != nil {
			log.Printf("Error launching storage destroy task: %v", err)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrRunningFargateTask),
			}, nil
		}

		m, _ := json.Marshal(models.StorageNodeResponse{
			Message: "Storage node deletion initiated",
		})
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusAccepted,
			Body:       string(m),
		}, nil
	}

	// Registration-only: delete workspace associations (if force=true retained any) + DynamoDB record directly
	if storageNodeWorkspaceTable != "" {
		wsStore := store_dynamodb.NewStorageNodeWorkspaceStore(dynamoDBClient, storageNodeWorkspaceTable)
		for _, ws := range attachedWorkspaces {
			if err := wsStore.Delete(ctx, ws.StorageNodeUuid, ws.WorkspaceId); err != nil {
				log.Printf("Error deleting workspace association: %v", err)
			}
		}
	}

	providerType := node.ProviderType
	err = storageNodeStore.Delete(ctx, nodeId)
	if err != nil {
		log.Printf("Error deleting storage node: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	// Regenerate IAM policies
	if providerType == "s3" {
		storagePolicyService := service.NewStoragePolicyService(cfg, storageNodeStore)
		if err := storagePolicyService.RegenerateStoragePolicies(ctx); err != nil {
			log.Printf("Warning: Failed to regenerate storage policies: %v", err)
		}
	}

	m, _ := json.Marshal(models.StorageNodeResponse{
		Message: "Storage node deleted",
	})
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       string(m),
	}, nil
}

// checkDeleteTag verifies the bucket has the "pennsieve:allow-delete" = "true" tag.
// Uses the cross-account role to read tags from the customer's bucket.
func checkDeleteTag(ctx context.Context, cfg aws.Config, account store_dynamodb.Account, node models.DynamoDBStorageNode) (bool, error) {
	// Assume the cross-account role to access the bucket
	stsClient := sts.NewFromConfig(cfg)
	roleArn := fmt.Sprintf("arn:aws:iam::%s:role/%s", account.AccountId, account.RoleName)
	crossAccountCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(node.Region),
		config.WithCredentialsProvider(
			stscreds.NewAssumeRoleProvider(stsClient, roleArn),
		),
	)
	if err != nil {
		return false, fmt.Errorf("failed to assume role for tag check: %w", err)
	}

	s3Client := s3.NewFromConfig(crossAccountCfg)
	output, err := s3Client.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{
		Bucket: aws.String(node.StorageLocation),
	})
	if err != nil {
		// NoSuchTagSet means bucket has no tags — treat as missing tag
		if strings.Contains(err.Error(), "NoSuchTagSet") {
			return false, nil
		}
		return false, fmt.Errorf("failed to get bucket tags: %w", err)
	}

	for _, tag := range output.TagSet {
		if aws.ToString(tag.Key) == "pennsieve:allow-delete" && aws.ToString(tag.Value) == "true" {
			return true, nil
		}
	}

	return false, nil
}

func launchStorageDestroyTask(ctx context.Context, cfg aws.Config, account store_dynamodb.Account, node models.DynamoDBStorageNode) error {
	storageTaskDefArn := os.Getenv("STORAGE_TASK_DEF_ARN")
	subIdStr := os.Getenv("SUBNET_IDS")
	subNetIds := strings.Split(subIdStr, ",")
	cluster := os.Getenv("CLUSTER_ARN")
	securityGroup := os.Getenv("SECURITY_GROUP")
	containerName := os.Getenv("STORAGE_TASK_DEF_CONTAINER_NAME")
	if containerName == "" {
		containerName = "storage-provisioner"
	}

	client := ecs.NewFromConfig(cfg)

	runTaskIn := &ecs.RunTaskInput{
		TaskDefinition: aws.String(storageTaskDefArn),
		Cluster:        aws.String(cluster),
		NetworkConfiguration: &ecstypes.NetworkConfiguration{
			AwsvpcConfiguration: &ecstypes.AwsVpcConfiguration{
				Subnets:        subNetIds,
				SecurityGroups: []string{securityGroup},
				AssignPublicIp: ecstypes.AssignPublicIpEnabled,
			},
		},
		Overrides: &ecstypes.TaskOverride{
			ContainerOverrides: []ecstypes.ContainerOverride{
				{
					Name: aws.String(containerName),
					Environment: []ecstypes.KeyValuePair{
						{Name: aws.String("ACTION"), Value: aws.String("DELETE")},
						{Name: aws.String("STORAGE_NODE_ID"), Value: aws.String(node.Uuid)},
						{Name: aws.String("BUCKET_NAME"), Value: aws.String(node.StorageLocation)},
						{Name: aws.String("ACCOUNT_ID"), Value: aws.String(account.AccountId)},
						{Name: aws.String("ENV"), Value: aws.String(os.Getenv("ENV"))},
						{Name: aws.String("REGION"), Value: aws.String(node.Region)},
						{Name: aws.String("ROLE_NAME"), Value: aws.String(account.RoleName)},
					},
				},
			},
		},
		LaunchType: ecstypes.LaunchTypeFargate,
	}

	taskRunner := runner.NewECSTaskRunner(client, runTaskIn)
	return taskRunner.Run(ctx)
}
