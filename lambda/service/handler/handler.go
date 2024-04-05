package handler

import (
	"context"
	"log/slog"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambdacontext"
	"github.com/pennsieve/account-service/service/logging"
)

var logger = logging.Default

func init() {
	logger.Info("init()")
}

func AccountServiceHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	if lc, ok := lambdacontext.FromContext(ctx); ok {
		logger = logger.With(slog.String("requestID", lc.AwsRequestID))
	}

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
	router.GET("/accounts", GetAccountsHandler)
	router.GET("/accounts/{id}", GetAccountHandler)
	return router.Start(ctx, request)
}
