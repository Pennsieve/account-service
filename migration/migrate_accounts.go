package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go/aws"
)

type OldAccount struct {
	Uuid           string `dynamodbav:"uuid"`
	UserId         string `dynamodbav:"userId"`
	OrganizationId string `dynamodbav:"organizationId"`
	AccountId      string `dynamodbav:"accountId"`
	AccountType    string `dynamodbav:"accountType"`
	RoleName       string `dynamodbav:"roleName"`
	ExternalId     string `dynamodbav:"externalId"`
}

type NewAccount struct {
	Uuid        string `dynamodbav:"uuid"`
	UserId      string `dynamodbav:"userId"`
	AccountId   string `dynamodbav:"accountId"`
	AccountType string `dynamodbav:"accountType"`
	RoleName    string `dynamodbav:"roleName"`
	ExternalId  string `dynamodbav:"externalId"`
}

type AccountWorkspaceEnablement struct {
	AccountUuid string `dynamodbav:"accountUuid"`
	WorkspaceId string `dynamodbav:"workspaceId"` // Changed from organizationId to match new table structure
	IsPublic    bool   `dynamodbav:"isPublic"`
	EnabledBy   string `dynamodbav:"enabledBy"`
	EnabledAt   int64  `dynamodbav:"enabledAt"`
	MigratedAt  int64  `dynamodbav:"migratedAt"`
}

func main() {
	ctx := context.Background()

	// Get table names from environment or command line
	accountsTable := os.Getenv("ACCOUNTS_TABLE")
	enablementTable := os.Getenv("ACCOUNT_WORKSPACE_TABLE")
	dryRun := os.Getenv("DRY_RUN") == "true"

	if accountsTable == "" || enablementTable == "" {
		log.Fatal("ACCOUNTS_TABLE and ACCOUNT_WORKSPACE_TABLE environment variables must be set")
	}

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("Failed to load AWS config: %v", err)
	}

	dynamoClient := dynamodb.NewFromConfig(cfg)

	// Step 1: Scan all existing accounts
	log.Println("Starting migration scan...")
	scanInput := &dynamodb.ScanInput{
		TableName: aws.String(accountsTable),
	}

	var totalProcessed, totalMigrated, totalSkipped int

	paginator := dynamodb.NewScanPaginator(dynamoClient, scanInput)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			log.Fatalf("Failed to scan table: %v", err)
		}

		for _, item := range page.Items {
			totalProcessed++

			var oldAccount OldAccount
			err := attributevalue.UnmarshalMap(item, &oldAccount)
			if err != nil {
				log.Printf("Error unmarshaling account: %v", err)
				continue
			}

			// Check if this account has an organizationId (needs migration)
			if oldAccount.OrganizationId == "" {
				log.Printf("Account %s already migrated (no organizationId), skipping", oldAccount.Uuid)
				totalSkipped++
				continue
			}

			log.Printf("Processing account %s for user %s in org %s",
				oldAccount.Uuid, oldAccount.UserId, oldAccount.OrganizationId)

			if dryRun {
				log.Printf("DRY RUN: Would migrate account %s", oldAccount.Uuid)
				totalMigrated++
				continue
			}

			// Step 2: Create workspace enablement record
			enablement := AccountWorkspaceEnablement{
				AccountUuid: oldAccount.Uuid,
				WorkspaceId: oldAccount.OrganizationId, // Map organizationId to workspaceId
				IsPublic:    true,                      // Default existing accounts to public in their workspace
				EnabledBy:   oldAccount.UserId,         // Set the original user as the enabler
				EnabledAt:   time.Now().Unix(),
				MigratedAt:  time.Now().Unix(),
			}

			enablementItem, err := attributevalue.MarshalMap(enablement)
			if err != nil {
				log.Printf("Error marshaling enablement: %v", err)
				continue
			}

			// Check if enablement already exists
			getItemInput := &dynamodb.GetItemInput{
				TableName: aws.String(enablementTable),
				Key: map[string]types.AttributeValue{
					"accountUuid": &types.AttributeValueMemberS{Value: oldAccount.Uuid},
					"workspaceId": &types.AttributeValueMemberS{Value: oldAccount.OrganizationId},
				},
			}

			existingItem, err := dynamoClient.GetItem(ctx, getItemInput)
			if err != nil {
				log.Printf("Error checking existing enablement: %v", err)
				continue
			}

			if existingItem.Item != nil {
				log.Printf("Enablement already exists for account %s in org %s, skipping",
					oldAccount.Uuid, oldAccount.OrganizationId)
				totalSkipped++
				continue
			}

			// Create the enablement
			putItemInput := &dynamodb.PutItemInput{
				TableName: aws.String(enablementTable),
				Item:      enablementItem,
			}

			_, err = dynamoClient.PutItem(ctx, putItemInput)
			if err != nil {
				log.Printf("Error creating enablement: %v", err)
				continue
			}

			// Step 3: Update the account record to remove organizationId
			// We do this with UpdateItem to preserve any other fields
			updateInput := &dynamodb.UpdateItemInput{
				TableName: aws.String(accountsTable),
				Key: map[string]types.AttributeValue{
					"uuid": &types.AttributeValueMemberS{Value: oldAccount.Uuid},
				},
				UpdateExpression: aws.String("REMOVE organizationId"),
				// Add a condition to ensure we only update if organizationId exists
				ConditionExpression: aws.String("attribute_exists(organizationId)"),
			}

			_, err = dynamoClient.UpdateItem(ctx, updateInput)
			if err != nil {
				log.Printf("Error updating account %s: %v", oldAccount.Uuid, err)
				// Try to rollback the enablement
				deleteInput := &dynamodb.DeleteItemInput{
					TableName: aws.String(enablementTable),
					Key: map[string]types.AttributeValue{
						"accountUuid": &types.AttributeValueMemberS{Value: oldAccount.Uuid},
						"workspaceId": &types.AttributeValueMemberS{Value: oldAccount.OrganizationId},
					},
				}
				_, _ = dynamoClient.DeleteItem(ctx, deleteInput)
				continue
			}

			log.Printf("Successfully migrated account %s", oldAccount.Uuid)
			totalMigrated++
		}
	}

	log.Printf("\nMigration complete!")
	log.Printf("Total processed: %d", totalProcessed)
	log.Printf("Total migrated: %d", totalMigrated)
	log.Printf("Total skipped: %d", totalSkipped)

	if dryRun {
		log.Println("\nThis was a DRY RUN. No actual changes were made.")
		log.Println("Run without DRY_RUN=true to perform the actual migration.")
	}
}
