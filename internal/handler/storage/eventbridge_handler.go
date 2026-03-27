package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-lambda-go/events"
	awsconfig "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pennsieve/account-service/internal/service"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/utils"
)

// StorageNodeEvent represents the event from the storage provisioner
type StorageNodeEvent struct {
	Action        string `json:"action"`
	StorageNodeId string `json:"storageNodeId"`
	Identifier    string `json:"identifier"`
	BucketName    string `json:"bucketName,omitempty"`
	BucketArn     string `json:"bucketArn,omitempty"`
	Region        string `json:"region,omitempty"`
	CreatedAt     string `json:"createdAt,omitempty"`
}

// StorageNodeErrorEvent represents error events from the storage provisioner
type StorageNodeErrorEvent struct {
	Action        string `json:"action"`
	StorageNodeId string `json:"storageNodeId"`
	Identifier    string `json:"identifier"`
	ErrorMessage  string `json:"errorMessage"`
	ErrorType     string `json:"errorType"`
	Timestamp     string `json:"timestamp"`
}

// StorageNodeEventBridgeHandler handles EventBridge events from the storage node provisioner
func StorageNodeEventBridgeHandler(ctx context.Context, event events.CloudWatchEvent) error {
	log.Printf("Received storage EventBridge event: Source=%s, DetailType=%s", event.Source, event.DetailType)

	cfg, err := utils.LoadAWSConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	storageNodesTable := os.Getenv("STORAGE_NODES_TABLE")
	if storageNodesTable == "" {
		return fmt.Errorf("STORAGE_NODES_TABLE not configured")
	}

	storageNodeStore := store_dynamodb.NewStorageNodeDatabaseStore(dynamoDBClient, storageNodesTable)

	switch event.DetailType {
	case "StorageNodeCREATE":
		return handleStorageCreateComplete(ctx, cfg, storageNodeStore, event.Detail)
	case "StorageNodeCREATEError":
		return handleStorageError(ctx, storageNodeStore, event.Detail)
	case "StorageNodeUPDATE":
		return handleStorageUpdateComplete(ctx, storageNodeStore, event.Detail)
	case "StorageNodeUPDATEError":
		return handleStorageError(ctx, storageNodeStore, event.Detail)
	case "StorageNodeDELETE":
		return handleStorageDeleteComplete(ctx, cfg, storageNodeStore, event.Detail)
	case "StorageNodeDELETEError":
		return handleStorageError(ctx, storageNodeStore, event.Detail)
	default:
		log.Printf("Unknown storage event type: %s", event.DetailType)
		return nil
	}
}

func handleStorageCreateComplete(ctx context.Context, cfg awsconfig.Config, storageNodeStore store_dynamodb.StorageNodeStore, detail json.RawMessage) error {
	var event StorageNodeEvent
	if err := json.Unmarshal(detail, &event); err != nil {
		return fmt.Errorf("failed to unmarshal CREATE event: %w", err)
	}

	log.Printf("Processing CREATE completion for storage node %s", event.StorageNodeId)

	node, err := storageNodeStore.GetById(ctx, event.StorageNodeId)
	if err != nil {
		return fmt.Errorf("failed to fetch storage node: %w", err)
	}
	if node.Uuid == "" {
		return fmt.Errorf("storage node %s not found", event.StorageNodeId)
	}

	node.Status = "Enabled"
	if err := storageNodeStore.Put(ctx, node); err != nil {
		return fmt.Errorf("failed to update storage node: %w", err)
	}

	// Regenerate IAM policies now that the bucket is created
	if node.ProviderType == "s3" {
		storagePolicyService := service.NewStoragePolicyService(cfg, storageNodeStore)
		if err := storagePolicyService.RegenerateStoragePolicies(ctx); err != nil {
			log.Printf("Warning: Failed to regenerate storage policies: %v", err)
		}
	}

	log.Printf("Successfully updated storage node %s to Enabled", event.StorageNodeId)
	return nil
}

func handleStorageUpdateComplete(ctx context.Context, storageNodeStore store_dynamodb.StorageNodeStore, detail json.RawMessage) error {
	var event StorageNodeEvent
	if err := json.Unmarshal(detail, &event); err != nil {
		return fmt.Errorf("failed to unmarshal UPDATE event: %w", err)
	}

	log.Printf("Processing UPDATE completion for storage node %s", event.StorageNodeId)

	node, err := storageNodeStore.GetById(ctx, event.StorageNodeId)
	if err != nil {
		return fmt.Errorf("failed to fetch storage node: %w", err)
	}
	if node.Uuid == "" {
		return fmt.Errorf("storage node %s not found", event.StorageNodeId)
	}

	node.Status = "Enabled"
	if err := storageNodeStore.Put(ctx, node); err != nil {
		return fmt.Errorf("failed to update storage node: %w", err)
	}

	log.Printf("Successfully updated storage node %s config, status back to Enabled", event.StorageNodeId)
	return nil
}

func handleStorageError(ctx context.Context, storageNodeStore store_dynamodb.StorageNodeStore, detail json.RawMessage) error {
	var event StorageNodeErrorEvent
	if err := json.Unmarshal(detail, &event); err != nil {
		return fmt.Errorf("failed to unmarshal error event: %w", err)
	}

	log.Printf("Processing error for storage node %s: %s", event.StorageNodeId, event.ErrorMessage)

	node, err := storageNodeStore.GetById(ctx, event.StorageNodeId)
	if err != nil {
		return fmt.Errorf("failed to fetch storage node: %w", err)
	}
	if node.Uuid == "" {
		return fmt.Errorf("storage node %s not found", event.StorageNodeId)
	}

	node.Status = "Failed"
	if err := storageNodeStore.Put(ctx, node); err != nil {
		return fmt.Errorf("failed to update storage node status: %w", err)
	}

	log.Printf("Updated storage node %s to Failed due to: %s", event.StorageNodeId, event.ErrorMessage)
	return nil
}

func handleStorageDeleteComplete(ctx context.Context, cfg awsconfig.Config, storageNodeStore store_dynamodb.StorageNodeStore, detail json.RawMessage) error {
	var event StorageNodeEvent
	if err := json.Unmarshal(detail, &event); err != nil {
		return fmt.Errorf("failed to unmarshal DELETE event: %w", err)
	}

	log.Printf("Processing DELETE completion for storage node %s", event.StorageNodeId)

	// Clean up workspace associations
	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	storageNodeWorkspaceTable := os.Getenv("STORAGE_NODE_WORKSPACE_TABLE")
	if storageNodeWorkspaceTable != "" {
		wsStore := store_dynamodb.NewStorageNodeWorkspaceStore(dynamoDBClient, storageNodeWorkspaceTable)
		workspaces, err := wsStore.GetByStorageNode(ctx, event.StorageNodeId)
		if err != nil {
			log.Printf("Error getting workspace associations: %v", err)
		}
		for _, ws := range workspaces {
			if err := wsStore.Delete(ctx, ws.StorageNodeUuid, ws.WorkspaceId); err != nil {
				log.Printf("Error deleting workspace association: %v", err)
			}
		}
	}

	// Delete the storage node record
	if err := storageNodeStore.Delete(ctx, event.StorageNodeId); err != nil {
		return fmt.Errorf("failed to delete storage node: %w", err)
	}

	// Regenerate IAM policies
	storagePolicyService := service.NewStoragePolicyService(cfg, storageNodeStore)
	if err := storagePolicyService.RegenerateStoragePolicies(ctx); err != nil {
		log.Printf("Warning: Failed to regenerate storage policies: %v", err)
	}

	log.Printf("Successfully deleted storage node %s", event.StorageNodeId)
	return nil
}
