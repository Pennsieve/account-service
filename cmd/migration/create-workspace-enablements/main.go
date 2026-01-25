package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/aws"
)

// CreateWorkspaceEnablements reads the original export and creates workspace enablement records
// based on the organizationId field that was in the old data
func main() {
	ctx := context.Background()
	
	if len(os.Args) < 2 {
		log.Fatal("Usage: go run create_workspace_enablements.go <export_directory>")
	}
	
	exportDir := os.Args[1]
	env := os.Getenv("ENV")
	if env == "" {
		env = "dev"
	}
	
	defaultUserId := "N:user:e9c03fb8-b28b-4383-a5c0-7bf6e072f3a2"
	
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("Failed to load AWS config: %v", err)
	}
	
	dynamoClient := dynamodb.NewFromConfig(cfg)
	workspaceTable := fmt.Sprintf("%s-compute-resource-account-workspace-table-use1", env)
	
	// Read the original export file
	accountsFile := fmt.Sprintf("%s/accounts.json", exportDir)
	data, err := ioutil.ReadFile(accountsFile)
	if err != nil {
		log.Fatalf("Failed to read file: %v", err)
	}
	
	// Parse the JSON
	var rawData map[string]interface{}
	if err := json.Unmarshal(data, &rawData); err != nil {
		log.Fatalf("Failed to parse JSON: %v", err)
	}
	
	items, ok := rawData["Items"].([]interface{})
	if !ok {
		log.Fatal("No Items found in export")
	}
	
	log.Printf("Processing %d accounts for workspace enablements...", len(items))
	successCount := 0
	skipCount := 0
	errorCount := 0
	
	for i, rawItem := range items {
		itemMap := rawItem.(map[string]interface{})
		
		// Extract necessary fields
		var uuid, organizationId, userId string
		
		if uuidVal, ok := itemMap["uuid"].(map[string]interface{}); ok {
			if s, ok := uuidVal["S"].(string); ok {
				uuid = s
			}
		}
		
		if orgVal, ok := itemMap["organizationId"].(map[string]interface{}); ok {
			if s, ok := orgVal["S"].(string); ok && s != "" {
				organizationId = s
			}
		}
		
		if userVal, ok := itemMap["userId"].(map[string]interface{}); ok {
			if s, ok := userVal["S"].(string); ok && s != "" {
				userId = s
			}
		}
		
		// Skip if no organizationId (nothing to migrate)
		if organizationId == "" {
			log.Printf("Account %s has no organizationId, skipping", uuid)
			skipCount++
			continue
		}
		
		// Use default userId if empty
		if userId == "" {
			userId = defaultUserId
		}
		
		// Create workspace enablement record
		enablement := map[string]types.AttributeValue{
			"accountUuid": &types.AttributeValueMemberS{Value: uuid},
			"workspaceId": &types.AttributeValueMemberS{Value: organizationId}, // Map org to workspace
			"isPublic":    &types.AttributeValueMemberBOOL{Value: true}, // Default to public
			"enabledBy":   &types.AttributeValueMemberS{Value: userId},
			"enabledAt":   &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", time.Now().Unix())},
		}
		
		log.Printf("Creating workspace enablement: account=%s, workspace=%s, enabledBy=%s", 
			uuid, organizationId, userId)
		
		_, err := dynamoClient.PutItem(ctx, &dynamodb.PutItemInput{
			TableName: aws.String(workspaceTable),
			Item:      enablement,
		})
		
		if err != nil {
			log.Printf("Error creating enablement for item %d: %v", i, err)
			errorCount++
		} else {
			successCount++
		}
	}
	
	log.Printf("\nWorkspace enablement creation complete!")
	log.Printf("Successful: %d", successCount)
	log.Printf("Skipped (no organizationId): %d", skipCount)
	log.Printf("Errors: %d", errorCount)
}