// backfill-node-tenant sets the tenant attribute on compute node records
// written before nodes had one.
//
// It derives each tenant from the record's organizationId and userId, as the
// service does when it reads such a record, and changes nothing else. Run it
// first without -apply: it lists every record and any whose ids can't name a
// tenant, which the service would refuse to store again.
//
//	go run ./scripts/backfill-node-tenant -table <env>-compute-resource-nodes-table-use1
//	go run ./scripts/backfill-node-tenant -table <env>-compute-resource-nodes-table-use1 -apply
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/pennsieve/account-service/internal/models"
)

func main() {
	table := flag.String("table", "", "compute nodes table name")
	apply := flag.Bool("apply", false, "write the tenants (default: report only)")
	flag.Parse()
	if *table == "" {
		flag.Usage()
		os.Exit(2)
	}

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("loading AWS config: %v", err)
	}
	db := dynamodb.NewFromConfig(cfg)

	var total, hasTenant, filled, failed int
	paginator := dynamodb.NewScanPaginator(db, &dynamodb.ScanInput{TableName: table})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			log.Fatalf("scanning %s: %v", *table, err)
		}
		for _, item := range page.Items {
			total++
			var node models.DynamoDBNode
			if err := attributevalue.UnmarshalMap(item, &node); err != nil {
				failed++
				fmt.Printf("UNREADABLE %v: %v\n", item["uuid"], err)
				continue
			}
			if !node.Tenant.IsZero() {
				hasTenant++
				continue
			}
			withTenant, err := node.WithTenant()
			if err != nil {
				failed++
				fmt.Printf("NO TENANT  %s (organizationId %q, userId %q): %v\n", node.Uuid, node.OrganizationId, node.UserId, err)
				continue
			}
			fmt.Printf("%-10s %s -> %s\n", map[bool]string{true: "SET", false: "WOULD SET"}[*apply], node.Uuid, withTenant.Tenant)
			if *apply {
				if err := setTenant(ctx, db, *table, withTenant); err != nil {
					failed++
					fmt.Printf("FAILED     %s: %v\n", node.Uuid, err)
					continue
				}
			}
			filled++
		}
	}

	fmt.Printf("\n%d records: %d already had a tenant, %d %s, %d failed\n",
		total, hasTenant, filled, map[bool]string{true: "set", false: "to set"}[*apply], failed)
	if failed > 0 {
		os.Exit(1)
	}
}

// setTenant writes only the tenant attribute, and only if the record still
// has none, so a concurrent write by the service is never overwritten.
func setTenant(ctx context.Context, db *dynamodb.Client, table string, node models.DynamoDBNode) error {
	value, err := node.Tenant.MarshalDynamoDBAttributeValue()
	if err != nil {
		return err
	}
	_, err = db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 aws.String(table),
		Key:                       node.GetKey(),
		UpdateExpression:          aws.String("SET #tenant = :tenant"),
		ConditionExpression:       aws.String("attribute_exists(#uuid) AND attribute_not_exists(#tenant)"),
		ExpressionAttributeNames:  map[string]string{"#tenant": "tenant", "#uuid": "uuid"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":tenant": value},
	})
	var conditionFailed *types.ConditionalCheckFailedException
	if errors.As(err, &conditionFailed) {
		return nil // the service set it, or the node was deleted, meanwhile
	}
	return err
}
