package compute

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/pennsieve/account-service/internal/interactivedns"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/utils"
)

// ComputeNodeEvent represents the event from the provisioner
type ComputeNodeEvent struct {
	Action                string `json:"action"`
	ComputeNodeId         string `json:"computeNodeId"`
	Identifier            string `json:"identifier"`
	Name                  string `json:"name,omitempty"`
	ComputeNodeGatewayUrl string `json:"computeNodeGatewayUrl,omitempty"`
	EfsId                 string `json:"efsId,omitempty"`
	QueueUrl              string `json:"queueUrl,omitempty"`
	WorkflowManagerTag    string `json:"workflowManagerTag,omitempty"`
	ProvisionerImage      string `json:"provisionerImage,omitempty"`
	ProvisionerImageTag   string `json:"provisionerImageTag,omitempty"`
	CreatedAt             string `json:"createdAt,omitempty"`

	// Interactive (Jupyter) session subdomain delegation. Present when the node
	// enabled interactive sessions; account-service upserts the NS record in the
	// Pennsieve parent zone so {accountKey}.compute.pennsieve.net resolves.
	InteractiveZoneName        string   `json:"interactiveZoneName,omitempty"`
	InteractiveZoneNameServers []string `json:"interactiveZoneNameServers,omitempty"`
}

// ComputeNodeErrorEvent represents error events from the provisioner
type ComputeNodeErrorEvent struct {
	Action        string `json:"action"`
	ComputeNodeId string `json:"computeNodeId"`
	Identifier    string `json:"identifier"`
	ErrorMessage  string `json:"errorMessage"`
	ErrorType     string `json:"errorType"`
	Timestamp     string `json:"timestamp"`
}

// ComputeNodeEventBridgeHandler handles EventBridge events from the compute node provisioner
func ComputeNodeEventBridgeHandler(ctx context.Context, event events.CloudWatchEvent) error {
	log.Printf("Received EventBridge event: Source=%s, DetailType=%s", event.Source, event.DetailType)

	// Load AWS configuration
	cfg, err := utils.LoadAWSConfig(ctx)
	if err != nil {
		log.Printf("Error loading AWS config: %v", err)
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Initialize DynamoDB client and store
	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	computeNodesTable := os.Getenv("COMPUTE_NODES_TABLE")
	if computeNodesTable == "" {
		log.Printf("COMPUTE_NODES_TABLE environment variable not set")
		return fmt.Errorf("COMPUTE_NODES_TABLE not configured")
	}

	nodeStore := store_dynamodb.NewNodeDatabaseStore(dynamoDBClient, computeNodesTable)

	// Handle different event types based on DetailType
	switch event.DetailType {
	case "ComputeNodeCREATE":
		return handleProvisioningComplete(ctx, nodeStore, event.Detail)
	case "ComputeNodeCREATEError":
		return handleProvisioningError(ctx, nodeStore, event.Detail)
	case "ComputeNodeUPDATE":
		return handleUpdateComplete(ctx, nodeStore, event.Detail)
	case "ComputeNodeUPDATEError":
		return handleProvisioningError(ctx, nodeStore, event.Detail)
	case "ComputeNodeDELETE":
		return handleDeleteComplete(ctx, nodeStore, event.Detail)
	case "ComputeNodeDELETEError":
		return handleProvisioningError(ctx, nodeStore, event.Detail)
	default:
		log.Printf("Unknown event type: %s", event.DetailType)
		return nil // Don't error on unknown events
	}
}

func handleProvisioningComplete(ctx context.Context, nodeStore store_dynamodb.NodeStore, detail json.RawMessage) error {
	var event ComputeNodeEvent
	if err := json.Unmarshal(detail, &event); err != nil {
		log.Printf("Error unmarshaling CREATE event: %v", err)
		return fmt.Errorf("failed to unmarshal CREATE event: %w", err)
	}

	log.Printf("Processing CREATE completion for node %s", event.ComputeNodeId)

	// Get the existing node from DynamoDB
	node, err := nodeStore.GetById(ctx, event.ComputeNodeId)
	if err != nil {
		log.Printf("Error fetching node %s: %v", event.ComputeNodeId, err)
		return fmt.Errorf("failed to fetch node: %w", err)
	}

	// Check if node exists
	if node.Uuid == "" {
		log.Printf("Node %s not found in database", event.ComputeNodeId)
		return fmt.Errorf("node %s not found", event.ComputeNodeId)
	}

	// Update node with provisioning results
	node.ComputeNodeGatewayUrl = event.ComputeNodeGatewayUrl
	node.EfsId = event.EfsId
	node.QueueUrl = event.QueueUrl
	node.Status = "Enabled" // Change from Pending to Enabled

	// Update the node in DynamoDB
	if err := nodeStore.Put(ctx, node); err != nil {
		log.Printf("Error updating node %s: %v", event.ComputeNodeId, err)
		return fmt.Errorf("failed to update node: %w", err)
	}

	log.Printf("Successfully updated node %s to Enabled status with infrastructure details", event.ComputeNodeId)

	// Best-effort: create/refresh the interactive subdomain NS delegation, and —
	// if this is the FIRST time the zone is delegated — kick off phase 2 (cert
	// validation + HTTPS listener), which the provisioner deliberately skipped on
	// the first pass to avoid deadlocking on an undelegated zone.
	ensureInteractiveDelegation(ctx, nodeStore, event)
	return nil
}

// ensureInteractiveDelegation upserts the NS delegation for the node's
// interactive-session subdomain in the Pennsieve parent zone, then — only when
// the delegation was NEWLY created — auto-triggers a phase-2 re-provision so the
// HTTPS listener comes up without any operator action. Best-effort: a failure
// here must not fail provisioning (the node is already Enabled). The delegation
// logic lives in interactivedns and is transport-agnostic so a future non-AWS
// (e.g. Azure) provisioner can reuse it via a different channel.
func ensureInteractiveDelegation(ctx context.Context, nodeStore store_dynamodb.NodeStore, event ComputeNodeEvent) {
	if event.InteractiveZoneName == "" || len(event.InteractiveZoneNameServers) == 0 {
		return
	}
	parentZoneID := os.Getenv("INTERACTIVE_PARENT_ZONE_ID")
	if parentZoneID == "" {
		log.Printf("INTERACTIVE_PARENT_ZONE_ID not set; skipping NS delegation for %s", event.InteractiveZoneName)
		return
	}
	cfg, err := utils.LoadAWSConfig(ctx)
	if err != nil {
		log.Printf("Warning: failed to load AWS config for NS delegation: %v", err)
		return
	}
	r53 := route53.NewFromConfig(cfg)
	created, err := interactivedns.EnsureZoneDelegation(ctx, r53, parentZoneID, event.InteractiveZoneName, event.InteractiveZoneNameServers)
	if err != nil {
		log.Printf("Warning: failed to ensure NS delegation for %s: %v", event.InteractiveZoneName, err)
		return
	}
	log.Printf("Ensured interactive NS delegation for %s (%d name servers, newlyCreated=%v)", event.InteractiveZoneName, len(event.InteractiveZoneNameServers), created)

	// Loop-safe: only on first delegation. Phase 2 is itself an UPDATE → its
	// event re-runs this, but by then the NS record exists → created=false → no
	// re-trigger.
	if created {
		triggerInteractivePhase2(ctx, cfg, nodeStore, event.ComputeNodeId)
	}
}

func handleProvisioningError(ctx context.Context, nodeStore store_dynamodb.NodeStore, detail json.RawMessage) error {
	var event ComputeNodeErrorEvent
	if err := json.Unmarshal(detail, &event); err != nil {
		log.Printf("Error unmarshaling error event: %v", err)
		return fmt.Errorf("failed to unmarshal error event: %w", err)
	}

	log.Printf("Processing provisioning error for node %s: %s", event.ComputeNodeId, event.ErrorMessage)

	// Update node status to Failed
	if err := nodeStore.UpdateStatus(ctx, event.ComputeNodeId, "Failed"); err != nil {
		log.Printf("Error updating node %s status to Failed: %v", event.ComputeNodeId, err)
		return fmt.Errorf("failed to update node status: %w", err)
	}

	// Optionally, store error details in a separate field or log
	// You might want to extend the DynamoDB schema to include error information

	log.Printf("Updated node %s to Failed status due to: %s", event.ComputeNodeId, event.ErrorMessage)
	return nil
}

func handleUpdateComplete(ctx context.Context, nodeStore store_dynamodb.NodeStore, detail json.RawMessage) error {
	var event ComputeNodeEvent
	if err := json.Unmarshal(detail, &event); err != nil {
		log.Printf("Error unmarshaling UPDATE event: %v", err)
		return fmt.Errorf("failed to unmarshal UPDATE event: %w", err)
	}

	log.Printf("Processing UPDATE completion for node %s", event.ComputeNodeId)

	// Get the existing node
	node, err := nodeStore.GetById(ctx, event.ComputeNodeId)
	if err != nil {
		log.Printf("Error fetching node %s: %v", event.ComputeNodeId, err)
		return fmt.Errorf("failed to fetch node: %w", err)
	}

	if node.Uuid == "" {
		log.Printf("Node %s not found in database", event.ComputeNodeId)
		return fmt.Errorf("node %s not found", event.ComputeNodeId)
	}

	// Update infrastructure details if provided
	if event.ComputeNodeGatewayUrl != "" {
		node.ComputeNodeGatewayUrl = event.ComputeNodeGatewayUrl
	}
	if event.EfsId != "" {
		node.EfsId = event.EfsId
	}
	if event.QueueUrl != "" {
		node.QueueUrl = event.QueueUrl
	}

	// Update the workflow manager tag if provided
	if event.WorkflowManagerTag != "" {
		node.WorkflowManagerTag = event.WorkflowManagerTag
	}

	// Update provisioner image/tag if provided
	if event.ProvisionerImage != "" {
		node.ProvisionerImage = event.ProvisionerImage
	}
	if event.ProvisionerImageTag != "" {
		node.ProvisionerImageTag = event.ProvisionerImageTag
	}

	// Ensure status is Enabled after successful update
	node.Status = "Enabled"

	// Save the updated node
	if err := nodeStore.Put(ctx, node); err != nil {
		log.Printf("Error updating node %s: %v", event.ComputeNodeId, err)
		return fmt.Errorf("failed to update node: %w", err)
	}

	log.Printf("Successfully processed UPDATE for node %s", event.ComputeNodeId)

	// Best-effort: refresh the interactive subdomain NS delegation if present.
	// An UPDATE that just enabled interactive (or re-created the shared zone)
	// carries fresh name servers, so re-upserting keeps the delegation current.
	ensureInteractiveDelegation(ctx, nodeStore, event)
	return nil
}

func handleDeleteComplete(ctx context.Context, nodeStore store_dynamodb.NodeStore, detail json.RawMessage) error {
	var event ComputeNodeEvent
	if err := json.Unmarshal(detail, &event); err != nil {
		log.Printf("Error unmarshaling DELETE event: %v", err)
		return fmt.Errorf("failed to unmarshal DELETE event: %w", err)
	}

	log.Printf("Processing DELETE completion for node %s", event.ComputeNodeId)

	// Load AWS configuration for additional cleanup
	cfg, err := utils.LoadAWSConfig(ctx)
	if err != nil {
		log.Printf("Error loading AWS config for cleanup: %v", err)
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	dynamoDBClient := dynamodb.NewFromConfig(cfg)

	// Capture the node's account before we delete it, so we can decide whether
	// the per-account interactive DNS delegation should be torn down.
	deletedNode, _ := nodeStore.GetById(ctx, event.ComputeNodeId)

	// Clean up all node access records (permissions) for this node
	nodeAccessTable := os.Getenv("NODE_ACCESS_TABLE")
	if nodeAccessTable != "" {
		nodeAccessStore := store_dynamodb.NewNodeAccessDatabaseStore(dynamoDBClient, nodeAccessTable)
		if err := nodeAccessStore.RemoveAllNodeAccess(ctx, event.ComputeNodeId); err != nil {
			log.Printf("Error removing access records for node %s: %v", event.ComputeNodeId, err)
			// Continue with deletion even if access cleanup fails
		} else {
			log.Printf("Cleaned up all access records for node %s", event.ComputeNodeId)
		}
	} else {
		log.Printf("NODE_ACCESS_TABLE not configured, skipping access cleanup")
	}

	// Delete the node from the main nodes table
	if err := nodeStore.Delete(ctx, event.ComputeNodeId); err != nil {
		log.Printf("Error deleting node %s from nodes table: %v", event.ComputeNodeId, err)
		return fmt.Errorf("failed to delete node: %w", err)
	}

	log.Printf("Successfully deleted node %s and all associated access records from database", event.ComputeNodeId)

	// If this account no longer has any interactive node, the per-account
	// interactive zone is (being) torn down — remove its NS delegation from the
	// Pennsieve parent zone so it doesn't dangle. Best-effort.
	removeInteractiveDelegationIfUnused(ctx, cfg, nodeStore, deletedNode)
	return nil
}

// removeInteractiveDelegationIfUnused deletes the parent-zone NS delegation for
// the account's interactive subdomain when no interactive nodes remain for the
// account (i.e. the per-account zone is gone or about to be). Safe: if a node
// later re-enables interactive, the provisioner re-creates the zone and
// account-service re-delegates. No-op if interactive was never in play.
func removeInteractiveDelegationIfUnused(ctx context.Context, cfg aws.Config, nodeStore store_dynamodb.NodeStore, deletedNode models.DynamoDBNode) {
	parentZoneID := os.Getenv("INTERACTIVE_PARENT_ZONE_ID")
	parentDomain := os.Getenv("INTERACTIVE_PARENT_DOMAIN")
	if parentZoneID == "" || parentDomain == "" || deletedNode.AccountUuid == "" || deletedNode.AccountId == "" {
		return
	}
	remaining, err := nodeStore.GetByAccount(ctx, deletedNode.AccountUuid)
	if err != nil {
		log.Printf("Warning: could not list account %s nodes for delegation cleanup: %v", deletedNode.AccountUuid, err)
		return
	}
	for _, n := range remaining {
		if n.MaxInteractiveSessions > 0 {
			return // another interactive node still needs the zone
		}
	}
	zoneName := fmt.Sprintf("%s.%s", deletedNode.AccountId, parentDomain)
	r53 := route53.NewFromConfig(cfg)
	if err := interactivedns.RemoveZoneDelegation(ctx, r53, parentZoneID, zoneName); err != nil {
		log.Printf("Warning: failed to remove NS delegation for %s: %v", zoneName, err)
		return
	}
	log.Printf("Removed interactive NS delegation for %s (no interactive nodes remain)", zoneName)
}
