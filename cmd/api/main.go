package main

import (
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/pennsieve/account-service/internal/handler"
)

func main() {
	lambda.Start(handler.AccountServiceHandler)
}