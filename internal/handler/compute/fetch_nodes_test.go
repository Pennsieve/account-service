package compute

import (
	"context"
	"errors"
	"testing"

	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
)

type fakeNodeStore struct {
	store_dynamodb.NodeStore
	nodes map[string]models.DynamoDBNode
}

func (f fakeNodeStore) GetById(_ context.Context, uuid string) (models.DynamoDBNode, error) {
	if uuid == "broken" {
		return models.DynamoDBNode{}, errors.New("read failed")
	}
	return f.nodes[uuid], nil
}

func TestFetchNodesKeepsOrderAndSkipsMissing(t *testing.T) {
	store := fakeNodeStore{nodes: map[string]models.DynamoDBNode{
		"a": {Uuid: "a", Name: "keep"},
		"b": {Uuid: "b", Name: "drop"},
		"c": {Uuid: "c", Name: "keep"},
	}}
	uuids := []string{"c", "gone", "b", "broken", "a"}
	got := fetchNodes(context.Background(), store, uuids, func(n models.DynamoDBNode) bool { return n.Name == "keep" })
	if len(got) != 2 || got[0].Uuid != "c" || got[1].Uuid != "a" {
		t.Fatalf("got %+v, want c then a", got)
	}
}
