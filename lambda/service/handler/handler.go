package handler

import (
	"log/slog"

	"github.com/aws/aws-lambda-go/events"
	"github.com/pennsieve/account-service/service/logging"
)

var logger = logging.Default

func init() {
	logger.Info("init()")
}

func AccountServiceHandler(request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	logger = logger.With(slog.String("requestID", request.RequestContext.RequestID))

	logger.Info("request parameters",
		"routeKey", request.RouteKey,
		"pathParameters", request.PathParameters,
		"rawPath", request.RawPath,
		"requestContext.routeKey", request.RequestContext.RouteKey,
		"requestContext.http.path", request.RequestContext.HTTP.Path)

	router := NewLambdaRouter()
	// register routes based on their supported methods
	router.GET("/pennsieve-accounts/{accountType}", GetPennsieveAccountsHandler)
	router.POST("/accounts", PostAccountsHandler)
	return router.Start(request)
}
