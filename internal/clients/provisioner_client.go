package clients

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

// ProvisionerClient makes IAM SigV4-signed HTTP requests to a compute-gateway
// Lambda function URL in the customer's AWS account.
type ProvisionerClient struct {
	GatewayURL string
	Region     string
	Config     aws.Config
}

func NewProvisionerClient(gatewayURL, region string, cfg aws.Config) *ProvisionerClient {
	return &ProvisionerClient{
		GatewayURL: gatewayURL,
		Region:     region,
		Config:     cfg,
	}
}

func (c *ProvisionerClient) Patch(ctx context.Context, path string, body []byte) ([]byte, error) {
	return c.doRequest(ctx, http.MethodPatch, path, body)
}

func (c *ProvisionerClient) Get(ctx context.Context, path string) ([]byte, error) {
	return c.doRequest(ctx, http.MethodGet, path, nil)
}

func (c *ProvisionerClient) Delete(ctx context.Context, path string) ([]byte, error) {
	return c.doRequest(ctx, http.MethodDelete, path, nil)
}

func (c *ProvisionerClient) doRequest(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	url := strings.TrimRight(c.GatewayURL, "/") + path

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// SigV4 sign the request
	creds, err := c.Config.Credentials.Retrieve(ctx)
	if err != nil {
		return nil, fmt.Errorf("retrieving credentials: %w", err)
	}

	var payloadHash string
	if body != nil {
		sum := sha256.Sum256(body)
		payloadHash = hex.EncodeToString(sum[:])
	} else {
		payloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" // empty string SHA256
	}

	signer := v4.NewSigner()
	if err := signer.SignHTTP(ctx, creds, req, payloadHash, "lambda", c.Region, time.Now()); err != nil {
		return nil, fmt.Errorf("signing request: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req = req.WithContext(reqCtx)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		log.Printf("Provisioner %s %s returned %d: %s", method, path, resp.StatusCode, string(respBody))
		return respBody, fmt.Errorf("provisioner returned status %d", resp.StatusCode)
	}

	return respBody, nil
}
