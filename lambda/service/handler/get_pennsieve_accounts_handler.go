package handler

import (
	"context"
	"encoding/json"
	"log"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/pennsieve/account-service/service/models"
	"github.com/pennsieve/account-service/service/utils"
)

const (
	AWS = "aws"
)

func GetPennsieveAccountsHandler(request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetPennsieveAccountsHandler"
	accountType := utils.ExtractParam(request.RawPath)
	log.Println("request.RequestContext.AccountID", request.RequestContext.AccountID)

	switch strings.ToLower(accountType) {
	case AWS:
		cfg, err := config.LoadDefaultConfig(context.Background())
		if err != nil {
			log.Println(err.Error())
			return events.APIGatewayV2HTTPResponse{
				StatusCode: 500,
				Body:       handlerName,
			}, ErrConfig
		}

		client := sts.NewFromConfig(cfg)
		input := &sts.GetCallerIdentityInput{}

		req, err := client.GetCallerIdentity(context.TODO(), input)
		if err != nil {
			log.Println(err.Error())
			return events.APIGatewayV2HTTPResponse{
				StatusCode: 500,
				Body:       handlerName,
			}, ErrSTS
		}
		accountId := *req.Account
		m, err := json.Marshal(models.PennsieveAccount{
			AccountID: accountId,
			Type:      AWS,
		})
		if err != nil {
			log.Println(err.Error())
			return events.APIGatewayV2HTTPResponse{
				StatusCode: 500,
				Body:       handlerName,
			}, ErrMarshaling
		}
		response := events.APIGatewayV2HTTPResponse{
			StatusCode: 200,
			Body:       string(m),
		}
		return response, nil
	default:
		return events.APIGatewayV2HTTPResponse{
			StatusCode: 422,
			Body:       handlerName,
		}, ErrUnsupportedAccountType
	}
}
