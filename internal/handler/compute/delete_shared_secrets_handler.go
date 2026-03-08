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

func DeleteSharedSecretsHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "DeleteSharedSecretsHandler"

	sctx, errResp := initSecretsContext(ctx, request, handlerName, true)
	if errResp != nil {
		return *errResp, nil
	}

	path := fmt.Sprintf("/secrets?computeNodeId=%s&scope=shared",
		url.QueryEscape(sctx.NodeUuid))

	if _, err := sctx.ProvisionerClient.Delete(ctx, path); err != nil {
		log.Printf("Error deleting shared secrets: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrProvisionerRequest),
		}, nil
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       `{"message":"shared secrets deleted"}`,
	}, nil
}