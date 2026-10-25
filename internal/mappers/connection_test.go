package mappers

import (
	"testing"

	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
)

func TestWithConnection(t *testing.T) {
	t.Setenv("CFN_TEMPLATE_VERSION", "4")
	cf := store_dynamodb.Account{UserId: "owner", Connection: "cloudformation", TemplateVersion: "3"}

	got := WithConnection(models.Account{}, cf, "owner")
	if got.Connection != "cloudformation" || got.TemplateVersion != "3" || got.LatestTemplateVersion != "4" {
		t.Fatalf("owner sees %+v", got)
	}
	// An account from before the template reads as legacy.
	got = WithConnection(models.Account{}, store_dynamodb.Account{UserId: "owner"}, "owner")
	if got.Connection != "legacy" || got.TemplateVersion != "" || got.LatestTemplateVersion != "4" {
		t.Fatalf("legacy reads %+v", got)
	}
	// Anyone else (an admin of an Admin Managed account) sees none of it.
	got = WithConnection(models.Account{}, cf, "admin")
	if got.Connection != "" || got.TemplateVersion != "" || got.LatestTemplateVersion != "" {
		t.Fatalf("non-owner sees %+v", got)
	}
}
