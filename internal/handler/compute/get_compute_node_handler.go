package compute

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/pennsieve/account-service/internal/utils"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/errors"
)

func GetComputeNodeHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetComputeNodeHandler"
	uuid := request.PathParameters["id"]

	cfg, err := utils.LoadAWSConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}
	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	computeNodesTable := os.Getenv("COMPUTE_NODES_TABLE")

	dynamo_store := store_dynamodb.NewNodeDatabaseStore(dynamoDBClient, computeNodesTable)
	computeNode, err := dynamo_store.GetById(ctx, uuid)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}
	if (models.DynamoDBNode{}) == computeNode {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrNoRecordsFound),
		}, nil
	}

	m, err := json.Marshal(models.Node{
		Uuid:                  computeNode.Uuid,
		Name:                  computeNode.Name,
		Description:           computeNode.Description,
		ComputeNodeGatewayUrl: computeNode.ComputeNodeGatewayUrl,
		EfsId:                 computeNode.EfsId,
		QueueUrl:              computeNode.QueueUrl,
		Account: models.NodeAccount{
			Uuid:        computeNode.AccountUuid,
			AccountId:   computeNode.AccountId,
			AccountType: computeNode.AccountType,
		},
		CreatedAt:          computeNode.CreatedAt,
		OrganizationId:     computeNode.OrganizationId,
		UserId:             computeNode.UserId,
		Identifier:         computeNode.Identifier,
		WorkflowManagerTag: computeNode.WorkflowManagerTag,
		Status:             computeNode.Status,
	})
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMarshaling),
		}, nil
	}
	response := events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       string(m),
	}
	return response, nil
}