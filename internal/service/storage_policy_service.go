package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
)

type StoragePolicyService struct {
	AWSConfig        aws.Config
	StorageNodeStore store_dynamodb.StorageNodeStore
}

func NewStoragePolicyService(cfg aws.Config, storageNodeStore store_dynamodb.StorageNodeStore) *StoragePolicyService {
	return &StoragePolicyService{
		AWSConfig:        cfg,
		StorageNodeStore: storageNodeStore,
	}
}

type iamPolicyDocument struct {
	Version   string              `json:"Version"`
	Statement []iamPolicyStatement `json:"Statement"`
}

type iamPolicyStatement struct {
	Sid      string   `json:"Sid"`
	Effect   string   `json:"Effect"`
	Action   []string `json:"Action"`
	Resource []string `json:"Resource"`
}

// RegenerateStoragePolicies scans all enabled S3 storage nodes and rebuilds the managed IAM policies
func (s *StoragePolicyService) RegenerateStoragePolicies(ctx context.Context) error {
	envValue := os.Getenv("ENV")
	if envValue == "DOCKER" || envValue == "TEST" {
		log.Println("Skipping IAM policy regeneration in test environment")
		return nil
	}

	nodes, err := s.StorageNodeStore.GetAllEnabled(ctx)
	if err != nil {
		return fmt.Errorf("error getting enabled storage nodes: %w", err)
	}

	// Collect unique S3 bucket ARNs
	bucketSet := make(map[string]bool)
	for _, node := range nodes {
		if node.ProviderType == "s3" && node.StorageLocation != "" {
			bucketSet[node.StorageLocation] = true
		}
	}

	var resources []string
	if len(bucketSet) == 0 {
		// Use a placeholder so the policy is valid but grants no access
		resources = []string{"arn:aws:s3:::placeholder"}
	} else {
		// Sort for deterministic output
		var buckets []string
		for bucket := range bucketSet {
			buckets = append(buckets, bucket)
		}
		sort.Strings(buckets)

		for _, bucket := range buckets {
			resources = append(resources, fmt.Sprintf("arn:aws:s3:::%s", bucket))
			resources = append(resources, fmt.Sprintf("arn:aws:s3:::%s/*", bucket))
		}
	}

	readPolicyArn := os.Getenv("STORAGE_READ_POLICY_ARN")
	writePolicyArn := os.Getenv("STORAGE_WRITE_POLICY_ARN")

	if readPolicyArn == "" || writePolicyArn == "" {
		return fmt.Errorf("STORAGE_READ_POLICY_ARN or STORAGE_WRITE_POLICY_ARN not set")
	}

	readPolicy := iamPolicyDocument{
		Version: "2012-10-17",
		Statement: []iamPolicyStatement{
			{
				Sid:    "StorageBucketRead",
				Effect: "Allow",
				Action: []string{
					"s3:GetObject",
					"s3:GetObjectVersion",
					"s3:GetObjectAttributes",
					"s3:ListBucket",
					"s3:ListBucketVersions",
					"s3:GetObjectTagging",
				},
				Resource: resources,
			},
		},
	}

	writePolicy := iamPolicyDocument{
		Version: "2012-10-17",
		Statement: []iamPolicyStatement{
			{
				Sid:    "StorageBucketWrite",
				Effect: "Allow",
				Action: []string{
					"s3:PutObject",
					"s3:DeleteObject",
					"s3:DeleteObjectVersion",
					"s3:PutObjectTagging",
					"s3:AbortMultipartUpload",
				},
				Resource: resources,
			},
		},
	}

	iamClient := iam.NewFromConfig(s.AWSConfig)

	if err := s.updateManagedPolicy(ctx, iamClient, readPolicyArn, readPolicy); err != nil {
		return fmt.Errorf("error updating read policy: %w", err)
	}

	if err := s.updateManagedPolicy(ctx, iamClient, writePolicyArn, writePolicy); err != nil {
		return fmt.Errorf("error updating write policy: %w", err)
	}

	log.Printf("Successfully regenerated storage policies with %d buckets", len(bucketSet))
	return nil
}

func (s *StoragePolicyService) updateManagedPolicy(ctx context.Context, client *iam.Client, policyArn string, policyDoc iamPolicyDocument) error {
	policyJSON, err := json.Marshal(policyDoc)
	if err != nil {
		return fmt.Errorf("error marshaling policy document: %w", err)
	}

	// List existing versions to check if we need to delete one (max 5 versions)
	versions, err := client.ListPolicyVersions(ctx, &iam.ListPolicyVersionsInput{
		PolicyArn: aws.String(policyArn),
	})
	if err != nil {
		return fmt.Errorf("error listing policy versions: %w", err)
	}

	if len(versions.Versions) >= 5 {
		// Find and delete the oldest non-default version
		var oldestNonDefault *string
		for _, v := range versions.Versions {
			if !v.IsDefaultVersion {
				if oldestNonDefault == nil {
					oldestNonDefault = v.VersionId
				}
			}
		}
		if oldestNonDefault != nil {
			_, err := client.DeletePolicyVersion(ctx, &iam.DeletePolicyVersionInput{
				PolicyArn: aws.String(policyArn),
				VersionId: oldestNonDefault,
			})
			if err != nil {
				return fmt.Errorf("error deleting old policy version: %w", err)
			}
		}
	}

	// Create new version and set as default
	_, err = client.CreatePolicyVersion(ctx, &iam.CreatePolicyVersionInput{
		PolicyArn:      aws.String(policyArn),
		PolicyDocument: aws.String(string(policyJSON)),
		SetAsDefault:   true,
	})
	if err != nil {
		return fmt.Errorf("error creating policy version: %w", err)
	}

	return nil
}
