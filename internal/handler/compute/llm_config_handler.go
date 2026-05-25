package compute

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/aws/aws-lambda-go/events"
	"github.com/pennsieve/account-service/internal/errors"
)

// llmConfigRequest is the body shape for PutLLMConfigHandler. Mirrors the
// gateway's wire shape so the proxy is a straight pass-through. Period is
// "daily" or "monthly".
type llmConfigRequest struct {
	BudgetUsd    float64 `json:"budgetUsd"`
	BudgetPeriod string  `json:"budgetPeriod"`
}

// PutLLMConfigHandler is the owner-only PUT for the node-wide LLM cost
// budget. Forwards to the compute-gateway, which writes the SSM parameter
// the governor reads on every invocation (chat + workflow apps).
func PutLLMConfigHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "PutLLMConfigHandler"

	sctx, errResp := initSecretsContext(ctx, request, handlerName, true)
	if errResp != nil {
		return *errResp, nil
	}

	var body llmConfigRequest
	if err := json.Unmarshal([]byte(request.Body), &body); err != nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrUnmarshaling),
		}, nil
	}
	if body.BudgetUsd < 0 {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       `{"message":"budgetUsd must be non-negative"}`,
		}, nil
	}
	if body.BudgetPeriod != "daily" && body.BudgetPeriod != "monthly" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       `{"message":"budgetPeriod must be 'daily' or 'monthly'"}`,
		}, nil
	}

	payload, err := json.Marshal(body)
	if err != nil {
		log.Printf("Error marshaling LLM config payload: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMarshaling),
		}, nil
	}

	respBody, err := sctx.ProvisionerClient.Put(ctx, "/llm-config", payload)
	if err != nil {
		log.Printf("Error putting LLM config: %v", err)
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

// GetLLMConfigHandler is owner-only. The current budget is policy data,
// not user-scoped, so we don't expose it more widely. Shared/team users
// who hit the cap see the rejection message from the governor itself.
func GetLLMConfigHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetLLMConfigHandler"

	sctx, errResp := initSecretsContext(ctx, request, handlerName, true)
	if errResp != nil {
		return *errResp, nil
	}

	respBody, err := sctx.ProvisionerClient.Get(ctx, "/llm-config")
	if err != nil {
		log.Printf("Error getting LLM config: %v", err)
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
