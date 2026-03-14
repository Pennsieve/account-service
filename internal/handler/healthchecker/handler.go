package healthchecker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/pennsieve/account-service/internal/clients"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
)

const (
	maxConcurrency    = 10
	healthCheckPath   = "/health"
	logTTLDays        = 30
)

type Config struct {
	Region string
	AWSCfg aws.Config
}

type Handler struct {
	NodeStore      store_dynamodb.NodeStore
	HealthLogStore store_dynamodb.HealthCheckLogStore
	DDBClient      *dynamodb.Client
	LayersTable    string
	Config         Config
}

type result struct {
	NodeUuid string
	Status   string
	Issues   string
	Body     string
}

func (h *Handler) Handle(ctx context.Context) error {
	nodes, err := h.NodeStore.GetAllEnabled(ctx)
	if err != nil {
		return fmt.Errorf("failed to get enabled nodes: %w", err)
	}

	log.Printf("Health check: found %d enabled nodes", len(nodes))

	results := make(chan result, len(nodes))
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup

	for _, node := range nodes {
		wg.Add(1)
		go func(n models.DynamoDBNode) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results <- h.checkNode(ctx, n)
		}(node)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	ttl := now.Add(logTTLDays * 24 * time.Hour).Unix()

	var errs []error
	for r := range results {
		// Update node health status
		if err := h.NodeStore.UpdateHealthStatus(ctx, r.NodeUuid, r.Status, nowStr); err != nil {
			log.Printf("Failed to update health status for node %s: %v", r.NodeUuid, err)
			errs = append(errs, err)
		}

		// Write health check log entry
		logEntry := models.DynamoDBHealthCheckLog{
			NodeId:       r.NodeUuid,
			Timestamp:    nowStr,
			Status:       r.Status,
			Issues:       r.Issues,
			ResponseBody: r.Body,
			TTL:          ttl,
		}
		if err := h.HealthLogStore.PutLog(ctx, logEntry); err != nil {
			log.Printf("Failed to write health check log for node %s: %v", r.NodeUuid, err)
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		log.Printf("Health check completed with %d errors out of %d nodes", len(errs), len(nodes))
	} else {
		log.Printf("Health check completed successfully for %d nodes", len(nodes))
	}

	return nil
}

func (h *Handler) checkNode(ctx context.Context, node models.DynamoDBNode) result {
	r := result{NodeUuid: node.Uuid}

	if node.ComputeNodeGatewayUrl == "" {
		log.Printf("Node %s (%s): no gateway URL, marking as UNKNOWN", node.Uuid, node.Name)
		r.Status = models.HealthStatusUnknown
		r.Issues = "no gateway URL configured"
		return r
	}

	client := clients.NewProvisionerClient(node.ComputeNodeGatewayUrl, h.Config.Region, h.Config.AWSCfg)
	body, err := client.Get(ctx, healthCheckPath)
	if err != nil {
		var httpErr *clients.ProvisionerHTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == 404 {
			log.Printf("Node %s (%s): health endpoint returned 404, old provisioner", node.Uuid, node.Name)
			r.Status = models.HealthStatusUnknown
			r.Issues = "health endpoint not available (404)"
			return r
		}

		log.Printf("Node %s (%s): health check failed: %v", node.Uuid, node.Name, err)
		r.Status = models.HealthStatusUnreachable
		r.Issues = fmt.Sprintf("health check failed: %v", err)
		return r
	}

	r.Body = string(body)

	// A null or empty response indicates the gateway doesn't implement /health
	// (e.g. v1 Python gateways return null for unhandled routes)
	trimmed := string(body)
	if trimmed == "null" || trimmed == "" {
		log.Printf("Node %s (%s): gateway returned null/empty response, old gateway without health endpoint", node.Uuid, node.Name)
		r.Status = models.HealthStatusUnknown
		r.Issues = "health endpoint not available (gateway returned null)"
		return r
	}

	var healthResp models.HealthCheckResponse
	if err := json.Unmarshal(body, &healthResp); err != nil {
		log.Printf("Node %s (%s): failed to parse health response: %v", node.Uuid, node.Name, err)
		r.Status = models.HealthStatusWarning
		r.Issues = fmt.Sprintf("failed to parse health response: %v", err)
		return r
	}

	r.Status = healthResp.Status
	if r.Status == "" {
		r.Status = models.HealthStatusUnknown
	}

	// Reconcile EFS layers against DynamoDB metadata
	if len(healthResp.Resources.EFSLayers) > 0 || h.LayersTable != "" {
		layerIssues := h.reconcileLayers(ctx, node.Uuid, healthResp.Resources.EFSLayers)
		healthResp.Issues = append(healthResp.Issues, layerIssues...)
	}

	if len(healthResp.Issues) > 0 {
		issuesJSON, _ := json.Marshal(healthResp.Issues)
		r.Issues = string(issuesJSON)
		if r.Status == models.HealthStatusHealthy {
			r.Status = models.HealthStatusWarning
		}
	}

	log.Printf("Node %s (%s): %s", node.Uuid, node.Name, r.Status)
	return r
}

// reconcileLayers compares EFS layer data against DynamoDB metadata.
// Auto-corrects size/fileCount mismatches and returns issues for structural problems.
func (h *Handler) reconcileLayers(ctx context.Context, computeNodeId string, efsLayers []models.EFSLayerInfo) []models.HealthCheckIssue {
	if h.LayersTable == "" || h.DDBClient == nil {
		return nil
	}

	// Query DynamoDB for all layers belonging to this compute node
	dbLayers, err := h.queryLayers(ctx, computeNodeId)
	if err != nil {
		log.Printf("Layer reconcile: failed to query layers for node %s: %v", computeNodeId, err)
		return nil
	}

	// Build lookup maps
	efsMap := make(map[string]models.EFSLayerInfo)
	for _, l := range efsLayers {
		efsMap[l.Name] = l
	}
	dbMap := make(map[string]models.LayerRecord)
	for _, l := range dbLayers {
		dbMap[l.LayerName] = l
	}

	var issues []models.HealthCheckIssue

	// Check DynamoDB layers against EFS
	for _, dbLayer := range dbLayers {
		efsLayer, onDisk := efsMap[dbLayer.LayerName]
		if !onDisk {
			issues = append(issues, models.HealthCheckIssue{
				Component: "layer:" + dbLayer.LayerName,
				Status:    "WARNING",
				Message:   "layer exists in metadata but not on disk (ORPHANED_METADATA)",
			})
			continue
		}

		// Auto-correct size/fileCount mismatches
		if dbLayer.SizeBytes != efsLayer.SizeBytes || dbLayer.FileCount != efsLayer.FileCount {
			log.Printf("Layer reconcile: correcting %s/%s: sizeBytes %d->%d, fileCount %d->%d",
				computeNodeId, dbLayer.LayerName,
				dbLayer.SizeBytes, efsLayer.SizeBytes,
				dbLayer.FileCount, efsLayer.FileCount)

			dbLayer.SizeBytes = efsLayer.SizeBytes
			dbLayer.FileCount = efsLayer.FileCount
			if err := h.updateLayer(ctx, dbLayer); err != nil {
				log.Printf("Layer reconcile: failed to update %s/%s: %v", computeNodeId, dbLayer.LayerName, err)
			}
		}
	}

	// Check for EFS layers not in DynamoDB
	for name := range efsMap {
		if _, inDB := dbMap[name]; !inDB {
			issues = append(issues, models.HealthCheckIssue{
				Component: "layer:" + name,
				Status:    "WARNING",
				Message:   "layer exists on disk but not in metadata (ORPHANED_DATA)",
			})
		}
	}

	if len(issues) > 0 {
		log.Printf("Layer reconcile: node %s has %d layer issues", computeNodeId, len(issues))
	}
	return issues
}

func (h *Handler) queryLayers(ctx context.Context, computeNodeId string) ([]models.LayerRecord, error) {
	resp, err := h.DDBClient.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(h.LayersTable),
		KeyConditionExpression: aws.String("computeNodeId = :cni"),
		ExpressionAttributeValues: map[string]dynamodbtypes.AttributeValue{
			":cni": &dynamodbtypes.AttributeValueMemberS{Value: computeNodeId},
		},
	})
	if err != nil {
		return nil, err
	}

	var layers []models.LayerRecord
	err = attributevalue.UnmarshalListOfMaps(resp.Items, &layers)
	return layers, err
}

func (h *Handler) updateLayer(ctx context.Context, layer models.LayerRecord) error {
	item, err := attributevalue.MarshalMap(layer)
	if err != nil {
		return err
	}

	_, err = h.DDBClient.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(h.LayersTable),
		Item:      item,
	})
	return err
}