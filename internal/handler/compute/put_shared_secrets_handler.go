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

func PutSharedSecretsHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "PutSharedSecretsHandler"

	sctx, errResp := initSecretsContext(ctx, request, handlerName, true)
	if errResp != nil {
		return *errResp, nil
	}

	var body putSecretsRequest
	if err := json.Unmarshal([]byte(request.Body), &body); err != nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrUnmarshaling),
		}, nil
	}

	if len(body.Secrets) == 0 {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrBadRequest),
		}, nil
	}

	if err := validateSecrets(body.Secrets); err != nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, err),
		}, nil
	}
	if err := validateSecretKeys(body.Secrets); err != nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, err),
		}, nil
	}

	payload, err := json.Marshal(map[string]interface{}{
		"computeNodeId": sctx.NodeUuid,
		"scope":         "shared",
		"secrets":       body.Secrets,
	})
	if err != nil {
		log.Printf("Error marshaling shared secrets payload: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMarshaling),
		}, nil
	}

	path := fmt.Sprintf("/secrets?computeNodeId=%s&scope=shared",
		url.QueryEscape(sctx.NodeUuid))

	if _, err := sctx.ProvisionerClient.Put(ctx, path, payload); err != nil {
		log.Printf("Error putting shared secrets: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrProvisionerRequest),
		}, nil
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       `{"message":"shared secrets updated"}`,
	}, nil
}