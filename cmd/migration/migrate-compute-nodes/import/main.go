package main

import (
	"context"
	"encoding/json"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/aws"
)


func importComputeNodes() {
	ctx := context.Background()

	// Get target table name and input file from environment
	targetTable := os.Getenv("TARGET_COMPUTE_NODES_TABLE")
	inputFile := os.Getenv("INPUT_FILE")
	dryRun := os.Getenv("DRY_RUN") == "true"
	skipExisting := os.Getenv("SKIP_EXISTING") == "true"

	if targetTable == "" {
		log.Fatal("TARGET_COMPUTE_NODES_TABLE environment variable must be set")
	}

	if inputFile == "" {
		inputFile = "compute_nodes_export.json"
	}

	log.Printf("Importing compute nodes to table: %s", targetTable)
	log.Printf("Input file: %s", inputFile)
	if dryRun {
		log.Println("DRY RUN mode enabled - no actual changes will be made")
	}
	if skipExisting {
		log.Println("SKIP_EXISTING mode enabled - will skip nodes that already exist")
	}

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("Failed to load AWS config: %v", err)
	}

	dynamoClient := dynamodb.NewFromConfig(cfg)

	// Read the JSON file
	file, err := os.Open(inputFile)
	if err != nil {
		log.Fatalf("Failed to open input file: %v", err)
	}
	defer file.Close()

	var nodes []DynamoDBNode
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&nodes)
	if err != nil {
		log.Fatalf("Failed to decode JSON: %v", err)
	}

	log.Printf("Loaded %d compute nodes from %s", len(nodes), inputFile)

	var totalProcessed, totalImported, totalSkipped, totalErrors int

	// Process each node
	for i, node := range nodes {
		totalProcessed++

		if totalProcessed%50 == 0 {
			log.Printf("Processing node %d/%d...", totalProcessed, len(nodes))
		}

		// Skip nodes with empty UUID
		if node.Uuid == "" {
			log.Printf("Skipping node %d: empty UUID", i+1)
			totalSkipped++
			continue
		}

		// Ensure Status field is set to "Enabled" if not already present
		if node.Status == "" {
			node.Status = "Enabled"
			log.Printf("Setting status to 'Enabled' for node %s", node.Uuid)
		}

		log.Printf("Processing node %s (org: %s, user: %s, status: %s)",
			node.Uuid, node.OrganizationId, node.UserId, node.Status)

		if skipExisting {
			// Check if the node already exists
			getItemInput := &dynamodb.GetItemInput{
				TableName: aws.String(targetTable),
				Key: map[string]types.AttributeValue{
					"uuid": &types.AttributeValueMemberS{Value: node.Uuid},
				},
			}

			existingItem, err := dynamoClient.GetItem(ctx, getItemInput)
			if err != nil {
				log.Printf("Error checking existing node %s: %v", node.Uuid, err)
				totalErrors++
				continue
			}

			if existingItem.Item != nil {
				log.Printf("Node %s already exists, skipping", node.Uuid)
				totalSkipped++
				continue
			}
		}

		if dryRun {
			log.Printf("DRY RUN: Would import node %s", node.Uuid)
			totalImported++
			continue
		}

		// Marshal the node to DynamoDB format
		item, err := attributevalue.MarshalMap(node)
		if err != nil {
			log.Printf("Error marshaling node %s: %v", node.Uuid, err)
			totalErrors++
			continue
		}

		// Put the item in the target table
		putItemInput := &dynamodb.PutItemInput{
			TableName: aws.String(targetTable),
			Item:      item,
		}

		if skipExisting {
			// Use condition expression to avoid overwriting existing items
			putItemInput.ConditionExpression = aws.String("attribute_not_exists(uuid)")
		}

		_, err = dynamoClient.PutItem(ctx, putItemInput)
		if err != nil {
			log.Printf("Error importing node %s: %v", node.Uuid, err)
			totalErrors++
			continue
		}

		log.Printf("Successfully imported node %s", node.Uuid)
		totalImported++
	}

	log.Printf("\nImport complete!")
	log.Printf("Total processed: %d", totalProcessed)
	log.Printf("Total imported: %d", totalImported)
	log.Printf("Total skipped: %d", totalSkipped)
	log.Printf("Total errors: %d", totalErrors)

	if dryRun {
		log.Println("\nThis was a DRY RUN. No actual changes were made.")
		log.Println("Run without DRY_RUN=true to perform the actual import.")
	}

	if totalErrors > 0 {
		log.Printf("\nWARNING: %d errors occurred during import. Check logs above for details.", totalErrors)
		os.Exit(1)
	}
}

func main() {
	importComputeNodes()
}