package handler

import (
	"context"
	"log/slog"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambdacontext"
	"github.com/pennsieve/account-service/internal/logging"
)

var logger = logging.Default

func init() {
	logger.Info("init()")
}

func AccountServiceHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	if lc, ok := lambdacontext.FromContext(ctx); ok {
		logger = logger.With(slog.String("requestID", lc.AwsRequestID))
	}

	router := NewLambdaRouter()
	// register routes based on their supported methods
	router.GET("/pennsieve-accounts/{accountType}", GetPennsieveAccountsHandler)
	router.POST("/accounts", PostAccountsHandler)
	router.GET("/accounts", GetAccountsHandler)
	router.GET("/accounts/{id}", GetAccountHandler)
	router.PATCH("/accounts/{id}", PatchAccountHandler)
	router.POST("/accounts/{uuid}/workspaces", PostAccountWorkspaceEnablementHandler)
	router.DELETE("/accounts/{uuid}/workspaces/{workspaceId}", DeleteAccountWorkspaceEnablementHandler)
	return router.Start(ctx, request)
}
