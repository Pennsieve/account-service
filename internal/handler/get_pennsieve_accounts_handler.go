package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/pennsieve/account-service/internal/models"
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
		cfg, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			log.Println(err.Error())
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       handlerError(handlerName, ErrConfig),
			}, nil
		}

		client := sts.NewFromConfig(cfg)
		input := &sts.GetCallerIdentityInput{}

		req, err := client.GetCallerIdentity(ctx, input)
		if err != nil {
			log.Println(err.Error())
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       handlerError(handlerName, ErrSTS),
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
				Body:       handlerError(handlerName, ErrMarshaling),
			}, nil
		}
		response := events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusOK,
			Body:       string(m),
		}
		return response, nil
	default:
		log.Println(ErrUnsupportedAccountType.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusUnprocessableEntity,
			Body:       handlerError(handlerName, ErrUnsupportedAccountType),
		}, nil
	}
}
