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

	router := NewLambdaRouter()
	// register routes based on their supported methods
	router.GET("/pennsieve-accounts", GetPennsieveAccountsHandler)
	return router.Start(request)
}
