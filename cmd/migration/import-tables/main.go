package main

import (
    "context"
    "encoding/json"
    "fmt"
    "io/ioutil"
    "log"
    "os"

    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
    "github.com/aws/aws-sdk-go-v2/aws"
)

type ExportData struct {
    Items []map[string]types.AttributeValue `json:"Items"`
    Count int                               `json:"Count"`
}

func main() {
    ctx := context.Background()

    if len(os.Args) < 2 {
        log.Fatal("Usage: go run import_tables.go <export_directory>")
    }

    exportDir := os.Args[1]
    env := os.Getenv("ENV")
    if env == "" {
        env = "dev"
    }

    cfg, err := config.LoadDefaultConfig(ctx)
    if err != nil {
        log.Fatalf("Failed to load AWS config: %v", err)
    }

    dynamoClient := dynamodb.NewFromConfig(cfg)

    // Import accounts table
    accountsFile := fmt.Sprintf("%s/accounts.json", exportDir)
    newAccountsTable := fmt.Sprintf("%s-compute-resource-accounts-table-use1", env)

    log.Printf("Importing accounts from %s to %s...", accountsFile, newAccountsTable)
    if err := importTable(ctx, dynamoClient, accountsFile, newAccountsTable); err != nil {
        log.Printf("Error importing accounts: %v", err)
    }

    log.Println("Import completed!")
}

func importTable(ctx context.Context, client *dynamodb.Client, fileName string, tableName string) error {
    // Read the export file
    data, err := ioutil.ReadFile(fileName)
    if err != nil {
        return fmt.Errorf("failed to read file: %v", err)
    }

    // Parse the JSON - handling DynamoDB JSON format
    var rawData map[string]interface{}
    if err := json.Unmarshal(data, &rawData); err != nil {
        return fmt.Errorf("failed to parse JSON: %v", err)
    }

    items, ok := rawData["Items"].([]interface{})
    if !ok {
        return fmt.Errorf("no Items found in export")
    }

    successCount := 0
    errorCount := 0

    for i, rawItem := range items {
        // Convert the raw item to DynamoDB AttributeValue format
        itemMap := rawItem.(map[string]interface{})
        dynamoItem := make(map[string]types.AttributeValue)

        hasUserId := false
        for key, value := range itemMap {
            // Skip organizationId field - it shouldn't exist in the new model
            if key == "organizationId" {
                log.Printf("Skipping organizationId field for item %d", i)
                continue
            }

            // Check if userId exists and is not empty
            if key == "userId" {
                if userIdMap, ok := value.(map[string]interface{}); ok {
                    if s, ok := userIdMap["S"].(string); ok && s != "" {
                        hasUserId = true
                    }
                }
            }

            // Convert the value to AttributeValue
            attr := convertToDynamoAttribute(value)
            if attr != nil {
                dynamoItem[key] = attr
            }
        }

        // Set default userId if it doesn't exist or is empty
        if !hasUserId {
            log.Printf("Setting default userId for item %d (uuid: %v)", i, dynamoItem["uuid"])
            dynamoItem["userId"] = &types.AttributeValueMemberS{
                Value: "N:user:9e8ecf93-62cf-41bf-9f32-99542acda06c",
            }
        }

        // Put item into new table
        _, err := client.PutItem(ctx, &dynamodb.PutItemInput{
            TableName: aws.String(tableName),
            Item:      dynamoItem,
        })

        if err != nil {
            log.Printf("Error importing item %d: %v", i, err)
            errorCount++
        } else {
            successCount++
        }
    }

    log.Printf("Import stats for %s: %d successful, %d errors", tableName, successCount, errorCount)
    return nil
}

func convertToDynamoAttribute(value interface{}) types.AttributeValue {
    switch v := value.(type) {
    case map[string]interface{}:
        // Handle DynamoDB JSON format
        if s, ok := v["S"].(string); ok {
            return &types.AttributeValueMemberS{Value: s}
        }
        if n, ok := v["N"].(string); ok {
            return &types.AttributeValueMemberN{Value: n}
        }
        if b, ok := v["BOOL"].(bool); ok {
            return &types.AttributeValueMemberBOOL{Value: b}
        }
        // Add more type handlers as needed (L, M, etc.)
    }
    return nil
}
