package compute

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/pennsieve/account-service/internal/runner"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
)

// triggerInteractivePhase2 launches an UPDATE re-provision of the node so the
// provisioner can complete interactive phase 2 (ACM cert validation + the shared
// ALB's HTTPS listener) now that the per-account zone has just been delegated.
//
// Phase 1 (the original create/update) deliberately skipped validation/listener
// to avoid the apply deadlocking on an undelegated zone. This is the auto-trigger
// that makes "create a node in the app" fully deploy interactive with no operator
// action — it's invoked only when the NS delegation was newly created, so it
// can't loop (phase 2 is itself an UPDATE whose event finds the delegation
// already present). Best-effort: a failure just means the operator (or the next
// node update) completes phase 2 on a later pass.
func triggerInteractivePhase2(ctx context.Context, cfg aws.Config, nodeStore store_dynamodb.NodeStore, computeNodeID string) {
	envValue := os.Getenv("ENV")
	if envValue == "DOCKER" || envValue == "TEST" {
		return
	}

	node, err := nodeStore.GetById(ctx, computeNodeID)
	if err != nil || node.Uuid == "" {
		log.Printf("phase2: could not load node %s: %v", computeNodeID, err)
		return
	}
	if node.MaxInteractiveSessions <= 0 {
		return // interactive not (or no longer) enabled
	}

	cluster := os.Getenv("CLUSTER_ARN")
	taskDefContainerName := os.Getenv("TASK_DEF_CONTAINER_NAME")
	securityGroup := os.Getenv("SECURITY_GROUP")
	if cluster == "" || taskDefContainerName == "" || securityGroup == "" || os.Getenv("SUBNET_IDS") == "" || os.Getenv("TASK_DEF_ARN") == "" {
		log.Printf("phase2: launch env not configured on this lambda (CLUSTER_ARN/SUBNET_IDS/SECURITY_GROUP/TASK_DEF_CONTAINER_NAME/TASK_DEF_ARN); skipping auto-trigger for node %s", computeNodeID)
		return
	}
	subnetIDs := strings.Split(os.Getenv("SUBNET_IDS"), ",")

	// Resolve the per-account compute role for the provisioner to assume.
	account, err := store_dynamodb.NewAccountDatabaseStore(dynamodb.NewFromConfig(cfg), os.Getenv("ACCOUNTS_TABLE")).GetById(ctx, node.AccountUuid)
	if err != nil {
		log.Printf("phase2: could not load account %s: %v", node.AccountUuid, err)
		return
	}

	provisionerImage := node.ProvisionerImage
	if provisionerImage == "" {
		provisionerImage = "pennsieve/compute-node-aws-provisioner-v2"
	}
	provisionerImageTag := node.ProvisionerImageTag
	if provisionerImageTag == "" {
		provisionerImageTag = "latest"
	}

	client := ecs.NewFromConfig(cfg)
	taskDef, err := createDynamicTaskDefinition(ctx, client, provisionerImage, provisionerImageTag, envValue)
	if err != nil {
		log.Printf("phase2: task def for node %s: %v", computeNodeID, err)
		return
	}

	nodeIdentifier := node.Identifier
	if nodeIdentifier == "" {
		nodeIdentifier = node.Uuid[:8]
	}
	deploymentMode := node.DeploymentMode
	if deploymentMode == "" {
		deploymentMode = "basic"
	}
	enableLLM := "false"
	if node.EnableLLMAccess {
		enableLLM = "true"
	}

	env := buildEnvVars(map[string]string{
		"COMPUTE_NODE_ID":          node.Uuid,
		"ENV":                      envValue,
		"ACTION":                   "UPDATE",
		"COMPUTE_NODES_TABLE":      os.Getenv("COMPUTE_NODES_TABLE"),
		"ORG_ID":                   node.OrganizationId,
		"USER_ID":                  node.UserId,
		"ACCOUNT_ID":               node.AccountId,
		"UUID":                     node.AccountUuid,
		"ACCOUNT_TYPE":             node.AccountType,
		"WM_TAG":                   node.WorkflowManagerTag,
		"WM_CPU":                   "2048",
		"WM_MEMORY":                "4096",
		"NODE_IDENTIFIER":          nodeIdentifier,
		"NODE_NAME":                node.Name,
		"PROVISIONER_IMAGE":        provisionerImage,
		"PROVISIONER_IMAGE_TAG":    provisionerImageTag,
		"DEPLOYMENT_MODE":          deploymentMode,
		"ENABLE_LLM_ACCESS":        enableLLM,
		"MAX_GPU_INSTANCES":        strconv.Itoa(node.MaxGpuInstances),
		"GPU_TIER":                 node.GpuTier,
		"MAX_INTERACTIVE_SESSIONS": strconv.Itoa(node.MaxInteractiveSessions),
		"ENABLE_INTERACTIVE":       "true",
		"ROLE_NAME":                account.RoleName,
	})

	runTaskIn := &ecs.RunTaskInput{
		TaskDefinition: taskDef.TaskDefinitionArn,
		Cluster:        aws.String(cluster),
		NetworkConfiguration: &types.NetworkConfiguration{
			AwsvpcConfiguration: &types.AwsVpcConfiguration{
				Subnets:        subnetIDs,
				SecurityGroups: []string{securityGroup},
				AssignPublicIp: types.AssignPublicIpEnabled,
			},
		},
		Overrides: &types.TaskOverride{
			ContainerOverrides: []types.ContainerOverride{
				{Name: &taskDefContainerName, Environment: env},
			},
		},
		LaunchType: types.LaunchTypeFargate,
	}
	if err := runner.NewECSTaskRunner(client, runTaskIn).Run(ctx); err != nil {
		log.Printf("phase2: re-provision RunTask for node %s failed: %v", computeNodeID, err)
		return
	}
	log.Printf("phase2: auto-triggered re-provision (UPDATE) for node %s to complete interactive HTTPS", computeNodeID)
}
