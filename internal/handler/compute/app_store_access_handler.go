package compute

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrTypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
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

	ecrClient := ecr.NewFromConfig(cfg)
	newPrincipal := fmt.Sprintf("arn:aws:iam::%s:root", req.AccountId)

	// Fetch the existing repository policy
	existingPolicy, err := ecrClient.GetRepositoryPolicy(ctx, &ecr.GetRepositoryPolicyInput{
		RepositoryName: aws.String(repoName),
	})

	var policy ecrPolicyDocument
	if err != nil {
		// If no policy exists yet, start with an empty one
		var rnfe *ecrTypes.RepositoryPolicyNotFoundException
		if !stderrors.As(err, &rnfe) {
			log.Printf("Error getting ECR repository policy: %v", err)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.ComputeHandlerError(handlerName, fmt.Errorf("error getting ECR repository policy")),
			}, nil
		}
		policy = buildEcrPolicy(nil)
	} else {
		if err := json.Unmarshal([]byte(*existingPolicy.PolicyText), &policy); err != nil {
			log.Printf("Error parsing existing ECR policy: %v", err)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrUnmarshaling),
			}, nil
		}
	}

	// Check if the principal already exists in the policy
	if policyContainsPrincipal(policy, newPrincipal) {
		log.Printf("Account %s already has access to %s; no update needed", req.AccountId, repoName)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusOK,
		}, nil
	}

	// Add the new principal to the policy
	policy = addPrincipalToPolicy(policy, newPrincipal)

	policyJSON, err := json.Marshal(policy)
	if err != nil {
		log.Printf("Error marshaling ECR policy: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMarshaling),
		}, nil
	}

	_, err = ecrClient.SetRepositoryPolicy(ctx, &ecr.SetRepositoryPolicyInput{
		RepositoryName: aws.String(repoName),
		PolicyText:     aws.String(string(policyJSON)),
	})
	if err != nil {
		log.Printf("Error setting ECR repository policy: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, fmt.Errorf("error setting ECR repository policy")),
		}, nil
	}

	log.Printf("Successfully added account %s to ECR repository policy for %s", req.AccountId, repoName)
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
	}, nil
}

const appStorePolicySid = "AllowCrossAccountPull"

type ecrPolicyDocument struct {
	Version   string               `json:"Version"`
	Statement []ecrPolicyStatement `json:"Statement"`
}

type ecrPolicyStatement struct {
	Sid       string             `json:"Sid"`
	Effect    string             `json:"Effect"`
	Principal ecrPolicyPrincipal `json:"Principal"`
	Action    []string           `json:"Action"`
}

type ecrPolicyPrincipal struct {
	AWS []string `json:"AWS"`
}

func buildEcrPolicy(principals []string) ecrPolicyDocument {
	if principals == nil {
		principals = []string{}
	}
	return ecrPolicyDocument{
		Version: "2012-10-17",
		Statement: []ecrPolicyStatement{
			{
				Sid:    appStorePolicySid,
				Effect: "Allow",
				Principal: ecrPolicyPrincipal{
					AWS: principals,
				},
				Action: []string{
					"ecr:GetDownloadUrlForLayer",
					"ecr:BatchGetImage",
					"ecr:BatchCheckLayerAvailability",
				},
			},
		},
	}
}

func policyContainsPrincipal(policy ecrPolicyDocument, principal string) bool {
	for _, stmt := range policy.Statement {
		if stmt.Sid == appStorePolicySid {
			for _, p := range stmt.Principal.AWS {
				if p == principal {
					return true
				}
			}
		}
	}
	return false
}

func addPrincipalToPolicy(policy ecrPolicyDocument, principal string) ecrPolicyDocument {
	for i, stmt := range policy.Statement {
		if stmt.Sid == appStorePolicySid {
			policy.Statement[i].Principal.AWS = append(stmt.Principal.AWS, principal)
			return policy
		}
	}
	// Statement not found — add a new one
	policy.Statement = append(policy.Statement, ecrPolicyStatement{
		Sid:    appStorePolicySid,
		Effect: "Allow",
		Principal: ecrPolicyPrincipal{
			AWS: []string{principal},
		},
		Action: []string{
			"ecr:GetDownloadUrlForLayer",
			"ecr:BatchGetImage",
			"ecr:BatchCheckLayerAvailability",
		},
	})
	return policy
}
