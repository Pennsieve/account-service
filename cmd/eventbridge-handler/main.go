package main

import (
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/pennsieve/account-service/internal/handler/compute"
)

func main() {
	// Start the Lambda handler for EventBridge events
	lambda.Start(compute.ComputeNodeEventBridgeHandler)
}