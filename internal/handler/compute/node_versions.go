package compute

import (
	"context"
	"log"
	"os"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
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
		sm := secretsmanager.NewFromConfig(cfg)
		ssmClient := ssm.NewFromConfig(cfg)
		resolver = dockerhub.NewResolver(sm, ssmClient, os.Getenv("DOCKER_HUB_CREDENTIALS_SECRET_ARN"), os.Getenv("ENV"))
	})
	return resolver
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
// looking up the newest released provisioner tag on Docker Hub (cached per
// image). It degrades gracefully: if a lookup fails, LatestVersion stays empty
// and UpdateAvailable stays false — the list call never fails over this.
func annotateLatestVersions(ctx context.Context, cfg aws.Config, nodes []models.Node) {
	if len(nodes) == 0 {
		return
	}
	r := getResolver(cfg)

	latestByImage := make(map[string]string)
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
		nodes[i].UpdateAvailable = dockerhub.IsOutdated(nodes[i].ProvisionerImageTag, latest)
	}
}
