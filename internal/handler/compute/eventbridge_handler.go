package compute

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
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
	CreatedAt             string `json:"createdAt,omitempty"`
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
	return nil
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

	// Update the workflow manager tag if provided
	if event.WorkflowManagerTag != "" {
		node.WorkflowManagerTag = event.WorkflowManagerTag
	}

	// Ensure status is Enabled after successful update
	node.Status = "Enabled"

	// Save the updated node
	if err := nodeStore.Put(ctx, node); err != nil {
		log.Printf("Error updating node %s: %v", event.ComputeNodeId, err)
		return fmt.Errorf("failed to update node: %w", err)
	}

	log.Printf("Successfully processed UPDATE for node %s", event.ComputeNodeId)
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
	return nil
}
