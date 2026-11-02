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
	"github.com/pennsieve/account-service/internal/clients"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
)

const (
	maxConcurrency  = 10
	healthCheckPath = "/health"
	logTTLDays      = 30
)

type Config struct {
	Region string
	AWSCfg aws.Config
}

// TagRefresher refreshes the shared cache of latest provisioner image tags
// (implemented by *dockerhub.Resolver). Optional — nil disables the refresh.
type TagRefresher interface {
	RefreshAll(ctx context.Context) error
}

type Handler struct {
	NodeStore      store_dynamodb.NodeStore
	HealthLogStore store_dynamodb.HealthCheckLogStore
	TagRefresher   TagRefresher
	Config         Config
}

type result struct {
	NodeUuid string
	Status   string
	Issues   string
	Body     string
}

func (h *Handler) Handle(ctx context.Context) error {
	// Refresh the shared latest-provisioner-tag cache so GET /compute-nodes can
	// flag outdated nodes without hitting Docker Hub on the request path.
	// Best-effort: a failure here must not fail the health check.
	if h.TagRefresher != nil {
		if err := h.TagRefresher.RefreshAll(ctx); err != nil {
			log.Printf("Provisioner tag refresh failed (continuing): %v", err)
		} else {
			log.Println("Provisioner tag cache refreshed")
		}
	}

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
