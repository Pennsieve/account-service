package compute

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"

	"github.com/aws/aws-lambda-go/events"
	"github.com/pennsieve/account-service/internal/errors"
)

func DeleteSecretsHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "DeleteSecretsHandler"

	sctx, errResp := initSecretsContext(ctx, request, handlerName, false)
	if errResp != nil {
		return *errResp, nil
	}

	path := fmt.Sprintf("/secrets?computeNodeId=%s&userId=%s&scope=user",
		url.QueryEscape(sctx.NodeUuid), url.QueryEscape(sctx.UserID))

	if _, err := sctx.ProvisionerClient.Delete(ctx, path); err != nil {
		log.Printf("Error deleting user secrets: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrProvisionerRequest),
		}, nil
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       `{"message":"secrets deleted"}`,
	}, nil
}