package compute

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Apps installed on a node the older way, before the App Store, were built
// into the node's own AWS account by app-deploy-service (its applications
// table, one row per app). Their IAM roles carry the node's permissions
// boundary, so the node's teardown can delete everything but that boundary and
// fails at the end, leaving the node Failed. The delete is refused until those
// apps are gone; app-deploy-service deletes them (DELETE /applications/{id}).

// legacyAppScanner is the DynamoDB surface legacyAppsOnNode needs.
type legacyAppScanner interface {
	Scan(context.Context, *dynamodb.ScanInput, ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error)
}

// legacyApp is an app installed on a node the older way.
type legacyApp struct {
	Uuid string
	Name string
}

// legacyAppsOnNode lists the apps installed on nodeUuid the older way. The
// table has no index by node, but it holds only those older installs.
func legacyAppsOnNode(ctx context.Context, client legacyAppScanner, table, nodeUuid string) ([]legacyApp, error) {
	var apps []legacyApp
	var start map[string]types.AttributeValue
	for {
		out, err := client.Scan(ctx, &dynamodb.ScanInput{
			TableName:                aws.String(table),
			FilterExpression:         aws.String("computeNodeUuid = :node"),
			ProjectionExpression:     aws.String("#uuid, #name"),
			ExpressionAttributeNames: map[string]string{"#uuid": "uuid", "#name": "name"},
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":node": &types.AttributeValueMemberS{Value: nodeUuid},
			},
			ExclusiveStartKey: start,
		})
		if err != nil {
			return nil, fmt.Errorf("listing apps installed on node %s: %w", nodeUuid, err)
		}
		for _, item := range out.Items {
			apps = append(apps, legacyApp{Uuid: stringAttr(item, "uuid"), Name: stringAttr(item, "name")})
		}
		if len(out.LastEvaluatedKey) == 0 {
			return apps, nil
		}
		start = out.LastEvaluatedKey
	}
}

func stringAttr(item map[string]types.AttributeValue, key string) string {
	if v, ok := item[key].(*types.AttributeValueMemberS); ok {
		return v.Value
	}
	return ""
}

// legacyAppsError is the refusal naming the apps to delete first.
func legacyAppsError(apps []legacyApp) error {
	names := make([]string, 0, len(apps))
	for _, a := range apps {
		names = append(names, fmt.Sprintf("%s (%s)", a.Name, a.Uuid))
	}
	return fmt.Errorf("compute node still has %d app(s) installed on it from before the App Store: %s. Their roles use the node's permissions boundary, so its teardown can't finish: delete them first (DELETE /applications/{id}), then delete the node",
		len(apps), strings.Join(names, ", "))
}
