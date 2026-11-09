package store_dynamodb

import (
	"context"
	"errors"
	"testing"

	"github.com/pennsieve/account-service/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAccounts struct {
	DynamoDBStore
	records []Account
	err     error
}

func (f fakeAccounts) GetByAccountId(_ context.Context, accountId string) ([]Account, error) {
	out := []Account{}
	for _, r := range f.records {
		if r.AccountId == accountId {
			out = append(out, r)
		}
	}
	return out, f.err
}

type fakeNodes struct {
	NodeStore
	nodes []models.DynamoDBNode
}

func (f fakeNodes) GetByAccount(_ context.Context, accountUuid string) ([]models.DynamoDBNode, error) {
	out := []models.DynamoDBNode{}
	for _, n := range f.nodes {
		if n.AccountUuid == accountUuid {
			out = append(out, n)
		}
	}
	return out, nil
}

func TestNodesInAWSAccount_SpansAccountRecords(t *testing.T) {
	accounts := fakeAccounts{records: []Account{
		{Uuid: "rec-a", AccountId: "111"},
		{Uuid: "rec-b", AccountId: "111"},
		{Uuid: "rec-c", AccountId: "222"},
	}}
	nodes := fakeNodes{nodes: []models.DynamoDBNode{
		{Uuid: "n1", AccountUuid: "rec-a", AccountId: "111"},
		{Uuid: "n2", AccountUuid: "rec-b", AccountId: "111"},
		{Uuid: "n3", AccountUuid: "rec-c", AccountId: "222"},
	}}

	got, err := NodesInAWSAccount(context.Background(), accounts, nodes, "111")
	require.NoError(t, err)
	uuids := []string{}
	for _, n := range got {
		uuids = append(uuids, n.Uuid)
	}
	assert.ElementsMatch(t, []string{"n1", "n2"}, uuids)
}

func TestNodesInAWSAccount_Errors(t *testing.T) {
	_, err := NodesInAWSAccount(context.Background(), fakeAccounts{}, fakeNodes{}, "")
	assert.Error(t, err, "an empty AWS account id must not read as 'no nodes'")

	_, err = NodesInAWSAccount(context.Background(), fakeAccounts{err: errors.New("boom")}, fakeNodes{}, "111")
	assert.Error(t, err)
}
