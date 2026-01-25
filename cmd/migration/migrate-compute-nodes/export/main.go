package main

import (
    "context"
    "encoding/json"
    "log"
    "os"

    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

func exportComputeNodes() {
    ctx := context.Background()

    // Get source table name from environment or command line
    sourceTable := os.Getenv("SOURCE_COMPUTE_NODES_TABLE")
    outputFile := os.Getenv("OUTPUT_FILE")

    if sourceTable == "" {
        log.Fatal("SOURCE_COMPUTE_NODES_TABLE environment variable must be set")
    }

    if outputFile == "" {
        outputFile = "compute_nodes_export.json"
    }

    log.Printf("Exporting compute nodes from table: %s", sourceTable)
    log.Printf("Output file: %s", outputFile)

    cfg, err := config.LoadDefaultConfig(ctx)
    if err != nil {
        log.Fatalf("Failed to load AWS config: %v", err)
    }

    dynamoClient := dynamodb.NewFromConfig(cfg)

    // Scan all items from the source table
    scanInput := &dynamodb.ScanInput{
        TableName: aws.String(sourceTable),
    }

    var allNodes []DynamoDBNode
    var totalProcessed int

    log.Println("Starting export scan...")

    paginator := dynamodb.NewScanPaginator(dynamoClient, scanInput)
    for paginator.HasMorePages() {
        page, err := paginator.NextPage(ctx)
        if err != nil {
            log.Fatalf("Failed to scan table: %v", err)
        }

        for _, item := range page.Items {
            totalProcessed++

            var node DynamoDBNode
            err := attributevalue.UnmarshalMap(item, &node)
            if err != nil {
                log.Printf("Error unmarshaling node %d: %v", totalProcessed, err)
                continue
            }

            allNodes = append(allNodes, node)

            if totalProcessed%100 == 0 {
                log.Printf("Processed %d items...", totalProcessed)
            }
        }
    }

    log.Printf("Total items scanned: %d", totalProcessed)
    log.Printf("Total nodes to export: %d", len(allNodes))

    // Write to JSON file
    file, err := os.Create(outputFile)
    if err != nil {
        log.Fatalf("Failed to create output file: %v", err)
    }
    defer file.Close()

    encoder := json.NewEncoder(file)
    encoder.SetIndent("", "  ")
    err = encoder.Encode(allNodes)
    if err != nil {
        log.Fatalf("Failed to write JSON: %v", err)
    }

    log.Printf("Export complete! Exported %d compute nodes to %s", len(allNodes), outputFile)
}

func main() {
    exportComputeNodes()
}
