package handler

import (
	"github.com/aws/aws-lambda-go/events"
	"github.com/pennsieve/template-serverless-service/service/logging"
	"log/slog"
)

var logger = logging.Default

func init() {
	logger.Info("init()")
}

// TODO update Handler function name
func TemplateServiceHandler(request events.APIGatewayV2HTTPRequest) (*events.APIGatewayV2HTTPResponse, error) {
	logger = logger.With(slog.String("requestID", request.RequestContext.RequestID))

	apiResponse, err := handleRequest()

	return apiResponse, err
}

func handleRequest() (*events.APIGatewayV2HTTPResponse, error) {
	logger.Info("handleRequest()")
	apiResponse := events.APIGatewayV2HTTPResponse{Body: "{'response':'hello'}", StatusCode: 200}

	return &apiResponse, nil
}
