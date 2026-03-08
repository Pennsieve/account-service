package compute

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"

	"github.com/aws/aws-lambda-go/events"
	"github.com/pennsieve/account-service/internal/errors"
)

func GetSecretsHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetSecretsHandler"

	sctx, errResp := initSecretsContext(ctx, request, handlerName, false)
	if errResp != nil {
		return *errResp, nil
	}

	path := fmt.Sprintf("/secrets?computeNodeId=%s&userId=%s&scope=user",
		url.QueryEscape(sctx.NodeUuid), url.QueryEscape(sctx.UserID))

	respBody, err := sctx.ProvisionerClient.Get(ctx, path)
	if err != nil {
		log.Printf("Error getting user secrets: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrProvisionerRequest),
		}, nil
	}

	if !json.Valid(respBody) {
		log.Printf("Provisioner returned invalid JSON for user secrets")
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrProvisionerRequest),
		}, nil
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       string(respBody),
	}, nil
}