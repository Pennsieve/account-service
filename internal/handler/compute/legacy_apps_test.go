package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// fakeLegacyApps answers each Scan with its next page.
type fakeLegacyApps struct {
	pages []*dynamodb.ScanOutput
	calls []*dynamodb.ScanInput
}

func (f *fakeLegacyApps) Scan(_ context.Context, in *dynamodb.ScanInput, _ ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	f.calls = append(f.calls, in)
	page := f.pages[0]
	f.pages = f.pages[1:]
	return page, nil
}

func appItem(uuid, name string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"uuid": &types.AttributeValueMemberS{Value: uuid},
		"name": &types.AttributeValueMemberS{Value: name},
	}
}

// Every page is read, filtered to the node, and the refusal names each app.
func TestLegacyAppsOnNode(t *testing.T) {
	more := map[string]types.AttributeValue{"uuid": &types.AttributeValueMemberS{Value: "a1"}}
	f := &fakeLegacyApps{pages: []*dynamodb.ScanOutput{
		{Items: []map[string]types.AttributeValue{appItem("a1", "dandi-cli")}, LastEvaluatedKey: more},
		{Items: []map[string]types.AttributeValue{appItem("a2", "sleap-nn")}},
	}}
	apps, err := legacyAppsOnNode(context.Background(), f, "apps", "node-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 2 || apps[0].Name != "dandi-cli" || apps[1].Uuid != "a2" {
		t.Fatalf("apps %+v", apps)
	}
	if len(f.calls) != 2 || f.calls[1].ExclusiveStartKey == nil {
		t.Fatal("want the second page read from where the first ended")
	}
	if v := f.calls[0].ExpressionAttributeValues[":node"].(*types.AttributeValueMemberS).Value; v != "node-1" {
		t.Fatalf("filtered on %q", v)
	}

	msg := legacyAppsError(apps).Error()
	for _, want := range []string{"2 app(s)", "dandi-cli (a1)", "sleap-nn (a2)", "DELETE /applications/{id}"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
}

func TestLegacyAppsOnNode_None(t *testing.T) {
	f := &fakeLegacyApps{pages: []*dynamodb.ScanOutput{{}}}
	apps, err := legacyAppsOnNode(context.Background(), f, "apps", "node-1")
	if err != nil || len(apps) != 0 {
		t.Fatalf("got %v, %v", apps, err)
	}
}
