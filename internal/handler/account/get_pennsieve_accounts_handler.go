package account

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/pennsieve/account-service/internal/utils"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/errors"
)

const (
	AWS = "aws"
)

func GetPennsieveAccountsHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetPennsieveAccountsHandler"
	accountType := request.PathParameters["accountType"]
	log.Println("request.RequestContext.AccountID", request.RequestContext.AccountID)

	switch strings.ToLower(accountType) {
	case AWS:
		cfg, err := utils.LoadAWSConfig(ctx)
		if err != nil {
			log.Println(err.Error())
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.HandlerError(handlerName, errors.ErrConfig),
			}, nil
		}

		client := sts.NewFromConfig(cfg)
		input := &sts.GetCallerIdentityInput{}

		req, err := client.GetCallerIdentity(ctx, input)
		if err != nil {
			log.Println(err.Error())
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.HandlerError(handlerName, errors.ErrSTS),
			}, nil
		}
		accountId := *req.Account
		m, err := json.Marshal(models.PennsieveAccount{
			AccountID: accountId,
			Type:      AWS,
		})
		if err != nil {
			log.Println(err.Error())
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.HandlerError(handlerName, errors.ErrMarshaling),
			}, nil
		}
		response := events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusOK,
			Body:       string(m),
		}
		return response, nil
	default:
		log.Println(errors.ErrUnsupportedAccountType.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusUnprocessableEntity,
			Body:       errors.HandlerError(handlerName, errors.ErrUnsupportedAccountType),
		}, nil
	}
}
