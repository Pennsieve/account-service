package main

import (
	"context"
	"log"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/pennsieve/account-service/internal/handler/compute"
	"github.com/pennsieve/account-service/internal/handler/storage"
)

// CombinedEventBridgeHandler routes EventBridge events to the appropriate handler
// based on the event source
func CombinedEventBridgeHandler(ctx context.Context, event events.CloudWatchEvent) error {
	log.Printf("Received EventBridge event: Source=%s, DetailType=%s", event.Source, event.DetailType)

	switch event.Source {
	case "storage-node-provisioner":
		return storage.StorageNodeEventBridgeHandler(ctx, event)
	default:
		// All other events go to the compute node handler (including "compute-node-provisioner")
		return compute.ComputeNodeEventBridgeHandler(ctx, event)
	}
}

func main() {
	lambda.Start(CombinedEventBridgeHandler)
}
