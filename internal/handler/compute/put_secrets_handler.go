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

const (
	maxSecrets        = 50
	maxSecretKeyLen   = 256
	maxSecretValueLen = 10000
)

type putSecretsRequest struct {
	Secrets map[string]string `json:"secrets"`
}

func validateSecrets(secrets map[string]string) error {
	if len(secrets) > maxSecrets {
		return errors.ErrTooManySecrets
	}
	for k, v := range secrets {
		if len(k) > maxSecretKeyLen {
			return errors.ErrSecretKeyTooLong
		}
		if len(v) > maxSecretValueLen {
			return errors.ErrSecretValueTooLong
		}
	}
	return nil
}

func PutSecretsHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "PutSecretsHandler"

	sctx, errResp := initSecretsContext(ctx, request, handlerName, false)
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
		"userId":        sctx.UserID,
		"scope":         "user",
		"secrets":       body.Secrets,
	})
	if err != nil {
		log.Printf("Error marshaling secrets payload: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMarshaling),
		}, nil
	}

	path := fmt.Sprintf("/secrets?computeNodeId=%s&userId=%s&scope=user",
		url.QueryEscape(sctx.NodeUuid), url.QueryEscape(sctx.UserID))

	if _, err := sctx.ProvisionerClient.Put(ctx, path, payload); err != nil {
		log.Printf("Error putting user secrets: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrProvisionerRequest),
		}, nil
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       `{"message":"secrets updated"}`,
	}, nil
}