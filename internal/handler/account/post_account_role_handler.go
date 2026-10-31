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
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pennsieve/account-service/internal/errors"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/utils"
)

// AccountRoleRequest switches an account to a new role (POST
// /accounts/{uuid}/role): one the owner created from the published template,
// with the role setup Pennsieve issued for it.
type AccountRoleRequest struct {
	RoleName   string `json:"roleName"`
	ExternalId string `json:"externalId"`
	// TemplateVersion is the template the role was made from; the published
	// one (CFN_TEMPLATE_VERSION) when left out.
	TemplateVersion string `json:"templateVersion,omitempty"`
}

// AccountRoleResponse names the role the account no longer uses, so the
// owner can delete it: Pennsieve can't, as the role's own policy denies
// touching Pennsieve-Compute-* roles.
type AccountRoleResponse struct {
	OldRoleName     string `json:"oldRoleName"`
	RoleName        string `json:"roleName"`
	RuntimeRoleName string `json:"runtimeRoleName,omitempty"`
	Connection      string `json:"connection"`
	TemplateVersion string `json:"templateVersion,omitempty"`
}

// PostAccountRoleHandler upgrades an account's connection: the account
// switches to a new role, typically one made from the CloudFormation
// template, replacing an older ("legacy") role.
// POST /accounts/{uuid}/role
//
// Only the account's owner may, and the new role is checked as a
// registration's is: its external ID was issued to the owner for that role
// (unexpired, unused, and used up here), Pennsieve can use the role with it
// and not without it, the role is in the account's AWS account, and no other
// account has registered it.
//
// The account record is the only place the role is kept: compute and storage
// nodes refer to the account, and everything that uses the role (this
// service, workflow-service, app-deploy-service, the provisioner tasks it
// starts, the infra dashboard) reads it from the record when it does. So
// switching the record is the whole upgrade, and nodes keep running.
func PostAccountRoleHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "PostAccountRoleHandler"
	refuse := func(status int, err error) (events.APIGatewayV2HTTPResponse, error) {
		return events.APIGatewayV2HTTPResponse{StatusCode: status, Body: errors.ComputeHandlerError(handlerName, err)}, nil
	}
	failed := func(err error, code error) (events.APIGatewayV2HTTPResponse, error) {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusInternalServerError, Body: errors.HandlerError(handlerName, code)}, nil
	}

	accountUuid := request.PathParameters["uuid"]
	var body AccountRoleRequest
	if err := json.Unmarshal([]byte(request.Body), &body); err != nil {
		return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusBadRequest, Body: errors.HandlerError(handlerName, errors.ErrUnmarshaling)}, nil
	}
	if body.RoleName == "" || body.ExternalId == "" || !validExternalId(body.ExternalId) {
		return refuse(http.StatusBadRequest, ErrInvalidExternalId)
	}
	if !validTemplateVersion(body.TemplateVersion) {
		return refuse(http.StatusBadRequest, ErrInvalidTemplateVersion)
	}
	templateVersion := body.TemplateVersion
	if templateVersion == "" {
		templateVersion = os.Getenv("CFN_TEMPLATE_VERSION")
	}

	userId, err := utils.GetUserIdFromRequest(request)
	if err != nil {
		return failed(err, errors.ErrConfig)
	}
	cfg, err := utils.LoadAWSConfig(ctx)
	if err != nil {
		return failed(err, errors.ErrConfig)
	}
	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	accountsStore := store_dynamodb.NewAccountDatabaseStore(dynamoDBClient, os.Getenv("ACCOUNTS_TABLE"))
	roleCodes := store_dynamodb.NewRoleCodeStore(dynamoDBClient, os.Getenv("ROLE_CODES_TABLE"))

	account, err := accountsStore.GetById(ctx, accountUuid)
	if err != nil {
		return failed(err, errors.ErrDynamoDB)
	}
	if account.Uuid == "" {
		return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusNotFound, Body: errors.HandlerError(handlerName, errors.ErrNoRecordsFound)}, nil
	}
	if account.UserId != userId {
		return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusForbidden, Body: errors.HandlerError(handlerName, errors.ErrAccountDoesNotBelongToUser)}, nil
	}
	if body.RoleName == account.RoleName {
		return refuse(http.StatusBadRequest, ErrSameRole)
	}

	if err := checkIssuedCode(ctx, roleCodes, body.ExternalId, userId, body.RoleName, time.Now()); err != nil {
		if errs.Is(err, ErrRoleCodeNotIssued) {
			return refuse(http.StatusForbidden, err)
		}
		return failed(err, errors.ErrDynamoDB)
	}

	// The new role is no other account's.
	sameAccount, err := accountsStore.GetByAccountId(ctx, account.AccountId)
	if err != nil {
		return failed(err, errors.ErrDynamoDB)
	}
	for _, other := range sameAccount {
		if other.Uuid != account.Uuid && other.RoleName == body.RoleName {
			return refuse(http.StatusConflict, ErrRoleAlreadyRegistered)
		}
	}

	runtimeRoleName, err := verifyRoles(ctx, NewRoleAssumer(cfg), account.AccountId, body.RoleName, body.ExternalId, templateVersion, time.Sleep)
	if err != nil {
		return refuse(http.StatusBadRequest, err)
	}

	if err := useIssuedCode(ctx, roleCodes, body.ExternalId, userId, body.RoleName, time.Now()); err != nil {
		if errs.Is(err, ErrRoleCodeNotIssued) {
			return refuse(http.StatusConflict, err)
		}
		return failed(err, errors.ErrDynamoDB)
	}

	oldRoleName := account.RoleName
	account.RoleName = body.RoleName
	account.RuntimeRoleName = runtimeRoleName
	account.ExternalId = body.ExternalId
	account.Connection = models.ConnectionCloudFormation
	account.TemplateVersion = templateVersion
	if err := accountsStore.Update(ctx, account); err != nil {
		// Still on the old role: the code can switch it again.
		if rerr := roleCodes.Release(ctx, body.ExternalId); rerr != nil {
			log.Println(rerr.Error())
		}
		return failed(err, errors.ErrDynamoDB)
	}
	log.Printf("%s: account %s switched from role %s to %s (template v%s)", handlerName, account.Uuid, oldRoleName, account.RoleName, templateVersion)

	m, err := json.Marshal(AccountRoleResponse{
		OldRoleName:     oldRoleName,
		RoleName:        account.RoleName,
		RuntimeRoleName: account.RuntimeRoleName,
		Connection:      account.Connection,
		TemplateVersion: account.TemplateVersion,
	})
	if err != nil {
		return failed(err, errors.ErrMarshaling)
	}
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(m),
	}, nil
}
