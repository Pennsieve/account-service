package compute

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/pennsieve/account-service/internal/dockerhub"
	"github.com/pennsieve/account-service/internal/models"
)

// defaultProvisionerImage mirrors the default in post_compute_nodes_handler.go;
// legacy nodes stored before ProvisionerImage was persisted have an empty value.
const defaultProvisionerImage = "pennsieve/compute-node-aws-provisioner-v2"

// resolver is a process-wide singleton so its tag cache survives across warm
// Lambda invocations (built once on first use).
var (
	resolverOnce sync.Once
	resolver     *dockerhub.Resolver
)

func getResolver(cfg aws.Config) *dockerhub.Resolver {
	resolverOnce.Do(func() {
		resolver = dockerhub.NewResolverFromEnv(cfg)
	})
	return resolver
}

// provisionerImageRef returns the container image to run for image:tag, and the
// tag it resolved to. With PROVISIONER_IMAGE_REGISTRY set (clin), that is the
// tag's digest in this account's ECR. ECR tags there are immutable, so there is
// no floating "latest": it resolves to the newest release.
func provisionerImageRef(ctx context.Context, cfg aws.Config, image, tag string) (string, string, error) {
	registry := os.Getenv("PROVISIONER_IMAGE_REGISTRY")
	if registry == "" {
		return fmt.Sprintf("%s:%s", image, tag), tag, nil
	}

	if tag == "" || tag == "latest" {
		latest := getResolver(cfg).LatestVersion(ctx, image)
		if latest == "" {
			return "", "", fmt.Errorf("no released tag of %s in ECR", image)
		}
		tag = latest
	}

	out, err := ecr.NewFromConfig(cfg).DescribeImages(ctx, &ecr.DescribeImagesInput{
		RepositoryName: aws.String(image),
		ImageIds:       []ecrtypes.ImageIdentifier{{ImageTag: aws.String(tag)}},
	})
	if err != nil {
		return "", "", fmt.Errorf("look up %s:%s in ECR: %w", image, tag, err)
	}
	if len(out.ImageDetails) == 0 || out.ImageDetails[0].ImageDigest == nil {
		return "", "", fmt.Errorf("%s:%s not found in ECR", image, tag)
	}
	return fmt.Sprintf("%s/%s@%s", registry, image, *out.ImageDetails[0].ImageDigest), tag, nil
}

// resolveDefaultProvisionerTag returns the newest released vX.Y.Z tag for image,
// used when a create/update request omits the provisioner tag so new nodes are
// pinned to a real release. The floating "latest" tag is only a last resort when
// the release can't be determined.
func resolveDefaultProvisionerTag(ctx context.Context, cfg aws.Config, image string) string {
	if image == "" {
		image = defaultProvisionerImage
	}
	if latest := getResolver(cfg).LatestVersion(ctx, image); latest != "" {
		return latest
	}
	log.Printf("could not resolve latest provisioner release for %s; falling back to 'latest'", image)
	return "latest"
}

// annotateLatestVersions fills LatestVersion/UpdateAvailable on each node by
// looking up the newest released provisioner tag (cached per
// image), and returns the latest tag for the default provisioner image so the
// list response can report it even when there are no nodes. It degrades
// gracefully: if a lookup fails, LatestVersion stays empty and UpdateAvailable
// stays false — the list call never fails over this.
func annotateLatestVersions(ctx context.Context, cfg aws.Config, nodes []models.Node) string {
	r := getResolver(cfg)

	latestByImage := map[string]string{
		defaultProvisionerImage: r.LatestVersion(ctx, defaultProvisionerImage),
	}
	for i := range nodes {
		image := nodes[i].ProvisionerImage
		if image == "" {
			image = defaultProvisionerImage
		}
		latest, ok := latestByImage[image]
		if !ok {
			latest = r.LatestVersion(ctx, image)
			latestByImage[image] = latest
		}
		nodes[i].LatestVersion = latest
		// A node already being updated isn't offered another update.
		nodes[i].UpdateAvailable = nodes[i].PendingProvisionerImageTag == "" && dockerhub.IsOutdated(nodes[i].ProvisionerImageTag, latest)
	}
	return latestByImage[defaultProvisionerImage]
}
