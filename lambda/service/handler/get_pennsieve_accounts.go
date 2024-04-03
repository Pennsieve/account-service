package handler

import (
	"net/http"

	"github.com/aws/aws-lambda-go/events"
)

func GetAccountsHandler(request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetAccountsHandler"

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       handlerName,
	}, nil
}
