package store_dynamodb

import (
	"context"
	"errors"
	"fmt"

	"github.com/pennsieve/account-service/internal/models"
)

// NodesInAWSAccount returns the nodes of every account record registered for
// the AWS account awsAccountId. Several records can share one AWS account (one
// per registering user), while the shared infrastructure in it (the
// interactive ALB, zone and its NS delegation) serves the nodes of all of
// them, so whether that infrastructure is still needed is decided here, not by
// one record's nodes.
func NodesInAWSAccount(ctx context.Context, accounts DynamoDBStore, nodes NodeStore, awsAccountId string) ([]models.DynamoDBNode, error) {
	if awsAccountId == "" {
		return nil, errors.New("no AWS account id")
	}
	records, err := accounts.GetByAccountId(ctx, awsAccountId)
	if err != nil {
		return nil, fmt.Errorf("listing account records for %s: %w", awsAccountId, err)
	}
	all := []models.DynamoDBNode{}
	for _, record := range records {
		recordNodes, err := nodes.GetByAccount(ctx, record.Uuid)
		if err != nil {
			return nil, fmt.Errorf("listing nodes of account %s: %w", record.Uuid, err)
		}
		all = append(all, recordNodes...)
	}
	return all, nil
}
