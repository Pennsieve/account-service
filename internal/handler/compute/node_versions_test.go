package compute

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
)

func TestProvisionerImageRef_DockerHubWithoutRegistry(t *testing.T) {
	t.Setenv("PROVISIONER_IMAGE_REGISTRY", "")
	for _, tag := range []string{"v1.8.12", "latest"} {
		ref, resolved, err := provisionerImageRef(context.Background(), aws.Config{}, "pennsieve/compute-node-aws-provisioner-v2", tag)
		if err != nil {
			t.Fatalf("provisionerImageRef(%q): %v", tag, err)
		}
		if want := "pennsieve/compute-node-aws-provisioner-v2:" + tag; ref != want || resolved != tag {
			t.Errorf("provisionerImageRef(%q) = %q, %q; want %q, %q", tag, ref, resolved, want, tag)
		}
	}
}
