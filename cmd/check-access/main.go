package main

import (
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/pennsieve/account-service/internal/handler/internal"
)

func main() {
	// Start the Lambda handler
	// This uses the LambdaHandler which accepts raw JSON events
	lambda.Start(internal.LambdaHandler)
}