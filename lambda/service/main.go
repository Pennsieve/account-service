package main

import (
	"github.com/aws/aws-lambda-go/lambda"
	//TODO update import
	"github.com/pennsieve/template-serverless-service/service/handler"
)

func main() {
	lambda.Start(handler.TemplateServiceHandler)
}
