package account

import (
	"context"
	"encoding/json"
	errs "errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/pennsieve/account-service/internal/utils"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/google/uuid"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/errors"
)

// PostAccountsHandler creates a new account
// POST /accounts
//
// Required Permissions:
// - Must be an authenticated user
// - Account will be associated with the requesting user (userId)
// - Prevents duplicate account registration per user
// - Pennsieve must be able to assume the role before it's registered; with an
//   external ID (agents that asked for one), the ID must be one Pennsieve
//   issued to this user for this role (unexpired and unused, used up here), the
//   role must also require it, and the account's role can't already be
//   registered by anyone. Those refusals carry {"message": ...}, which the
//   agent shows the user.
// - Without an external ID (older agents), only until LEGACY_REGISTRATION_CUTOFF:
//   before it, accepted with Deprecation/Sunset headers; from it, refused.
func PostAccountsHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "PostAccountsHandler"
	var account models.Account
	if err := json.Unmarshal([]byte(request.Body), &account); err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.HandlerError(handlerName, errors.ErrUnmarshaling),
		}, nil
	}

	if !validTemplateVersion(account.TemplateVersion) {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, ErrInvalidTemplateVersion),
		}, nil
	}

	if !validExternalId(account.ExternalId) {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, ErrInvalidExternalId),
		}, nil
	}

	userId, err := utils.GetUserIdFromRequest(request)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.HandlerError(handlerName, errors.ErrConfig),
		}, nil
	}

	// Registering without an external ID is being retired: refused from the
	// cutoff, flagged as deprecated before it. Registered legacy accounts are
	// untouched.
	var deprecationHeaders map[string]string
	if account.ExternalId == "" {
		refused, headers := legacyRegistration(time.Now())
		if refused {
			log.Printf("%s: refused a registration without an external ID (user %s, account %s): past LEGACY_REGISTRATION_CUTOFF", handlerName, userId, account.AccountId)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusBadRequest,
				Body:       errors.ComputeHandlerError(handlerName, ErrLegacyRegistrationRetired),
			}, nil
		}
		if headers != nil {
			log.Printf("%s: WARNING: registration without an external ID (user %s, account %s); these stop being accepted at %s", handlerName, userId, account.AccountId, headers["Sunset"])
		}
		deprecationHeaders = headers
	}

	cfg, err := utils.LoadAWSConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.HandlerError(handlerName, errors.ErrConfig),
		}, nil
	}
	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	accountsTable := os.Getenv("ACCOUNTS_TABLE")
	accountsStore := &store_dynamodb.AccountDatabaseStore{
		DB:        dynamoDBClient,
		TableName: accountsTable,
	}

	// Check if user already has this account registered
	userAccounts, err := accountsStore.GetByUserId(ctx, userId)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.HandlerError(handlerName, errors.ErrConfig),
		}, nil
	}
	
	// Check for duplicate account for this user
	for _, existingAccount := range userAccounts {
		if existingAccount.AccountId == account.AccountId {
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusUnprocessableEntity,
				Body:       errors.HandlerError(handlerName, errors.ErrRecordAlreadyExists),
			}, nil
		}
	}

	// An external ID registers only the role it was issued for, and only for
	// the user it was issued to: seeing it in someone's trust policy isn't
	// enough. Checked before Pennsieve tries the role at all.
	roleCodes := store_dynamodb.NewRoleCodeStore(dynamoDBClient, os.Getenv("ROLE_CODES_TABLE"))
	if account.ExternalId != "" {
		if err := checkIssuedCode(ctx, roleCodes, account.ExternalId, userId, account.RoleName, time.Now()); err != nil {
			if errs.Is(err, ErrRoleCodeNotIssued) {
				return events.APIGatewayV2HTTPResponse{
					StatusCode: http.StatusForbidden,
					Body:       errors.ComputeHandlerError(handlerName, err),
				}, nil
			}
			log.Println(err.Error())
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.HandlerError(handlerName, errors.ErrDynamoDB),
			}, nil
		}
	}

	// One registration per role once roles carry an external ID: another
	// user registering the same account and role is refused, rather than
	// handed a second record for it.
	if account.ExternalId != "" {
		sameAccount, err := accountsStore.GetByAccountId(ctx, account.AccountId)
		if err != nil {
			log.Println(err.Error())
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.HandlerError(handlerName, errors.ErrDynamoDB),
			}, nil
		}
		for _, existing := range sameAccount {
			if existing.RoleName == account.RoleName {
				return events.APIGatewayV2HTTPResponse{
					StatusCode: http.StatusConflict,
					Body:       errors.ComputeHandlerError(handlerName, ErrRoleAlreadyRegistered),
				}, nil
			}
		}
	}

	// Both roles a stack from template v2 makes must work; a v1 stack or a
	// legacy role has one role for everything.
	runtimeRoleName, err := verifyRoles(ctx, NewRoleAssumer(cfg), account.AccountId, account.RoleName, account.ExternalId, account.TemplateVersion, time.Sleep)
	if err != nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, err),
		}, nil
	}

	// The role works: use the code up, so it registers this role once.
	if account.ExternalId != "" {
		if err := useIssuedCode(ctx, roleCodes, account.ExternalId, userId, account.RoleName, time.Now()); err != nil {
			if errs.Is(err, ErrRoleCodeNotIssued) {
				return events.APIGatewayV2HTTPResponse{
					StatusCode: http.StatusConflict,
					Body:       errors.ComputeHandlerError(handlerName, err),
				}, nil
			}
			log.Println(err.Error())
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.HandlerError(handlerName, errors.ErrDynamoDB),
			}, nil
		}
	}

	id := uuid.New()
	registeredAccountId := id.String()

	// persist to dynamodb - now only associated with user
	store_account := store_dynamodb.Account{
		Uuid:        registeredAccountId,
		UserId:      userId,
		AccountId:   account.AccountId,
		AccountType: account.AccountType,
		RoleName:    account.RoleName,
		ExternalId:  account.ExternalId,
		Name:        account.Name,
		Description: account.Description,
		Status:      "Enabled", // Default status is Enabled
	}
	// A role from the published template (the app, or an agent that uses it)
	// says which version it was made from; any other is a legacy connection.
	if account.ExternalId != "" && account.TemplateVersion != "" {
		store_account.Connection = models.ConnectionCloudFormation
		store_account.TemplateVersion = account.TemplateVersion
		store_account.RuntimeRoleName = runtimeRoleName
	}
	err = accountsStore.Insert(ctx, store_account)
	if err != nil {
		log.Println(err.Error())
		// Not registered after all: the code can register the role again.
		if account.ExternalId != "" {
			if rerr := roleCodes.Release(ctx, account.ExternalId); rerr != nil {
				log.Println(rerr.Error())
			}
		}
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.HandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	m, err := json.Marshal(models.AccountResponse{
		Uuid: registeredAccountId,
	})
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.HandlerError(handlerName, errors.ErrMarshaling),
		}, nil
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusCreated,
		Headers:    deprecationHeaders,
		Body:       string(m),
	}, nil
}
