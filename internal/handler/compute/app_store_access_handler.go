package compute

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	appStoreEcr "github.com/pennsieve/account-service/internal/ecr"
	"github.com/pennsieve/account-service/internal/errors"
	"github.com/pennsieve/account-service/internal/utils"
)

type appStoreAccessRequest struct {
	AccountId   string `json:"accountId"`
	AccountType string `json:"accountType"`
}

// PostAppStoreAccessHandler grants the requesting AWS account cross-account
// pull access to the private App Store ECR repository. It reads the existing
// repository policy, adds the new principal if not already present, and writes
// the updated policy back — avoiding a full account table scan on every call.
//
// POST /app-store/access
func PostAppStoreAccessHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "PostAppStoreAccessHandler"

	var req appStoreAccessRequest
	if err := json.Unmarshal([]byte(request.Body), &req); err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrUnmarshaling),
		}, nil
	}

	if req.AccountId == "" || req.AccountType == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrBadRequest),
		}, nil
	}

	repoURL := os.Getenv("APP_STORE_ECR_REPOSITORY")
	if repoURL == "" {
		log.Println("APP_STORE_ECR_REPOSITORY environment variable not set")
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}

	repoName := utils.ExtractRepoName(repoURL)

	// Skip ECR calls in test environments
	envValue := os.Getenv("ENV")
	if envValue == "DOCKER" || envValue == "TEST" {
		log.Printf("Skipping ECR policy update in test environment for account %s", req.AccountId)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusOK,
		}, nil
	}

	cfg, err := utils.LoadAWSConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}

	policy := appStoreEcr.NewAppStorePolicy(ecr.NewFromConfig(cfg), repoName)
	if err := policy.EnsureAccess(ctx, req.AccountId); err != nil {
		log.Printf("Error ensuring App Store access: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, err),
		}, nil
	}

	log.Printf("Successfully ensured account %s has access to App Store repository %s", req.AccountId, repoName)
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
	}, nil
}
