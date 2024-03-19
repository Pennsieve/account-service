package handler

import (
	"encoding/json"
	"log"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/pennsieve/account-service/service/models"
	"github.com/pennsieve/account-service/service/utils"
)

const (
	AWS = "aws"
)

func GetPennsieveAccountsHandler(request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetPennsieveAccountsHandler"
	accountType := utils.ExtractParam(request.RawPath)
	accountId := request.RequestContext.AccountID

	switch strings.ToLower(accountType) {
	case AWS:
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
