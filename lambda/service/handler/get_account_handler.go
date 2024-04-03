package handler

import (
	"net/http"

	"github.com/aws/aws-lambda-go/events"
)

func GetAccountHandler(request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetAccountHandler"

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       handlerName,
	}, nil
}
