package account

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pennsieve/account-service/internal/errors"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/noderecords"
	"github.com/pennsieve/account-service/internal/roleaccess"
	"github.com/pennsieve/account-service/internal/service"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/utils"
)

// DeleteAccountHandler deletes an account and its workspace enablements
// DELETE /accounts/{id}
//
// Query Parameters:
// - force: if "true", delete even if account has active compute nodes
//
// Required Permissions:
// - Must be the owner of the account (account.UserId == requestingUserId)
func DeleteAccountHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "DeleteAccountHandler"

	uuid := request.PathParameters["id"]
	if uuid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.HandlerError(handlerName, errors.ErrMissingAccountUuid),
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
	nodesTable := os.Getenv("COMPUTE_NODES_TABLE")
	enablementTable := os.Getenv("ACCOUNT_WORKSPACE_TABLE")
	storageNodesTable := os.Getenv("STORAGE_NODES_TABLE")
	storageNodeWorkspaceTable := os.Getenv("STORAGE_NODE_WORKSPACE_TABLE")

	accountsStore := &store_dynamodb.AccountDatabaseStore{
		DB:        dynamoDBClient,
		TableName: accountsTable,
	}

	// Get the account
	account, err := accountsStore.GetById(ctx, uuid)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.HandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	if (store_dynamodb.Account{}) == account {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       errors.HandlerError(handlerName, errors.ErrNoRecordsFound),
		}, nil
	}

	// Check ownership
	if account.UserId != userId {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       errors.HandlerError(handlerName, errors.ErrAccountDoesNotBelongToUser),
		}, nil
	}

	// Check for active compute nodes
	nodeStore := store_dynamodb.NewNodeDatabaseStore(dynamoDBClient, nodesTable)
	nodes, err := nodeStore.GetByAccount(ctx, uuid)
	if err != nil {
		log.Printf("error getting nodes for account %s: %v", uuid, err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.HandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	force := request.QueryStringParameters["force"]

	// Check for active storage nodes on this account
	var storageNodes []models.DynamoDBStorageNode
	if storageNodesTable != "" {
		storageNodeStore := store_dynamodb.NewStorageNodeDatabaseStore(dynamoDBClient, storageNodesTable)
		storageNodes, err = storageNodeStore.GetByAccount(ctx, uuid)
		if err != nil {
			log.Printf("error getting storage nodes for account %s: %v", uuid, err)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.HandlerError(handlerName, errors.ErrDynamoDB),
			}, nil
		}
	}

	// With nodes left, the account is disconnected only once they're torn down
	// (or with force=true) — unless its role was removed outside Pennsieve, or
	// no longer trusts it. Then nothing can tear the nodes down from here, and
	// refusing would leave the account stuck: Pennsieve removes its records and
	// says what may remain in the AWS account. A role check that fails for
	// another reason (throttling, network) keeps the refusal.
	//
	// Only for connections made in the web app (CloudFormation): their
	// registration or upgrade proved this service can assume the role. An older
	// role may trust only the provisioner, so a refusal proves nothing; those
	// keep the refusal, with a hint to upgrade the connection first.
	cloudFormation := account.Connection == models.ConnectionCloudFormation
	roleGone := false
	if (len(nodes) > 0 || len(storageNodes) > 0) && force != "true" && cloudFormation {
		usability, cerr := roleaccess.Check(ctx, roleaccess.NewAssumer(cfg), account.AccountId, account.RoleName, account.ExternalId)
		if usability == roleaccess.Unusable {
			log.Printf("Role %s of account %s is unusable (%v): disconnecting with %d compute and %d storage nodes", account.RoleName, uuid, cerr, len(nodes), len(storageNodes))
			roleGone = true
		} else if cerr != nil && usability == roleaccess.Unknown {
			log.Printf("Warning: could not check role %s of account %s: %v", account.RoleName, uuid, cerr)
		}
	}
	if (len(nodes) > 0 || len(storageNodes) > 0) && force != "true" && !roleGone {
		msg := fmt.Sprintf("account has %d active compute nodes and %d active storage nodes", len(nodes), len(storageNodes))
		if !cloudFormation {
			msg += ". If its role was removed in AWS, upgrade the connection in the Pennsieve web app first, or clean up with " + roleaccess.RunbookURL
		}
		body, _ := json.Marshal(map[string]interface{}{
			"error":            msg,
			"nodeCount":        len(nodes),
			"storageNodeCount": len(storageNodes),
		})
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusConflict,
			Body:       string(body),
		}, nil
	}

	// Delete workspace enablements
	enablementStore := store_dynamodb.NewAccountWorkspaceStore(dynamoDBClient, enablementTable)
	enablements, err := enablementStore.GetByAccount(ctx, uuid)
	if err != nil {
		log.Printf("warning: failed to get workspace enablements for account %s: %v", uuid, err)
	} else {
		for _, e := range enablements {
			if err := enablementStore.Delete(ctx, e.AccountUuid, e.WorkspaceId); err != nil {
				log.Printf("warning: failed to delete workspace enablement %s/%s: %v", e.AccountUuid, e.WorkspaceId, err)
			}
		}
	}

	// Delete storage nodes (and their workspace attachments) so they don't linger in IAM policies.
	// Provisioned buckets are NOT torn down here — the owner is expected to have cleaned those up first,
	// or to pass force=true to accept that the records go away while the buckets remain.
	if len(storageNodes) > 0 {
		storageNodeStore := store_dynamodb.NewStorageNodeDatabaseStore(dynamoDBClient, storageNodesTable)
		var wsStore store_dynamodb.StorageNodeWorkspaceStore
		if storageNodeWorkspaceTable != "" {
			wsStore = store_dynamodb.NewStorageNodeWorkspaceStore(dynamoDBClient, storageNodeWorkspaceTable)
		}
		for _, sn := range storageNodes {
			if wsStore != nil {
				attached, err := wsStore.GetByStorageNode(ctx, sn.Uuid)
				if err != nil {
					log.Printf("warning: failed to get storage node workspaces %s: %v", sn.Uuid, err)
				}
				for _, a := range attached {
					if err := wsStore.Delete(ctx, a.StorageNodeUuid, a.WorkspaceId); err != nil {
						log.Printf("warning: failed to delete storage node workspace %s/%s: %v", a.StorageNodeUuid, a.WorkspaceId, err)
					}
				}
			}
			if err := storageNodeStore.Delete(ctx, sn.Uuid); err != nil {
				log.Printf("warning: failed to delete storage node %s: %v", sn.Uuid, err)
			}
		}
		storagePolicyService := service.NewStoragePolicyService(cfg, storageNodeStore)
		if err := storagePolicyService.RegenerateStoragePolicies(ctx); err != nil {
			log.Printf("warning: failed to regenerate storage policies after account delete: %v", err)
		}
	}

	// The compute nodes Pennsieve can no longer reach: their records go (as a
	// teardown's completion would remove them); what they built may remain.
	if roleGone {
		for _, n := range nodes {
			if err := noderecords.Remove(ctx, cfg, dynamoDBClient, nodeStore, n.Uuid); err != nil {
				log.Printf("warning: failed to remove compute node %s: %v", n.Uuid, err)
			}
		}
	}

	// Delete the account record
	err = accountsStore.Delete(ctx, uuid)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.HandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	// The deleted account, so the owner can delete its role (or the stack
	// that made it); with the role already gone, also what may remain.
	deleted := disconnectedAccount{Account: models.Account{
		Uuid:        account.Uuid,
		AccountId:   account.AccountId,
		AccountType: account.AccountType,
		RoleName:    account.RoleName,
		ExternalId:  account.ExternalId,
		UserId:      account.UserId,
	}}
	if roleGone {
		deleted.RemovedNote = &roleaccess.RemovedNote{
			Message:             "Pennsieve can no longer use this account's role, so it disconnected the account without tearing its nodes down. Their resources may remain in the AWS account.",
			ResourcesMayRemain:  true,
			RunbookUrl:          roleaccess.RunbookURL,
			ComputeNodesRemoved: len(nodes),
			StorageNodesRemoved: len(storageNodes),
		}
	}
	m, err := json.Marshal(deleted)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.HandlerError(handlerName, errors.ErrMarshaling),
		}, nil
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       string(m),
	}, nil
}

// disconnectedAccount is DELETE /accounts/{id}'s answer: the account, and,
// when its role was already gone, what may remain in the AWS account.
type disconnectedAccount struct {
	models.Account
	*roleaccess.RemovedNote
}
