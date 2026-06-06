package interactivedns

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/route53/types"
)

type fakeRoute53 struct {
	called   bool
	input    *route53.ChangeResourceRecordSetsInput
	err      error
	existing []types.ResourceRecordSet // returned by ListResourceRecordSets
}

func (f *fakeRoute53) ChangeResourceRecordSets(ctx context.Context, params *route53.ChangeResourceRecordSetsInput, optFns ...func(*route53.Options)) (*route53.ChangeResourceRecordSetsOutput, error) {
	f.called = true
	f.input = params
	return &route53.ChangeResourceRecordSetsOutput{}, f.err
}

func (f *fakeRoute53) ListResourceRecordSets(ctx context.Context, params *route53.ListResourceRecordSetsInput, optFns ...func(*route53.Options)) (*route53.ListResourceRecordSetsOutput, error) {
	return &route53.ListResourceRecordSetsOutput{ResourceRecordSets: f.existing}, nil
}

func nsRRS(name string) types.ResourceRecordSet {
	return types.ResourceRecordSet{Name: aws.String(name), Type: types.RRTypeNs}
}

func TestEnsureZoneDelegation_NoOpOnEmptyInputs(t *testing.T) {
	cases := []struct {
		name        string
		parentZone  string
		zoneName    string
		nameServers []string
	}{
		{"no parent zone", "", "acct.compute.pennsieve.net", []string{"ns-1.aws"}},
		{"no zone name", "Z123", "", []string{"ns-1.aws"}},
		{"no name servers", "Z123", "acct.compute.pennsieve.net", nil},
		{"only empty name servers", "Z123", "acct.compute.pennsieve.net", []string{""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRoute53{}
			created, err := EnsureZoneDelegation(context.Background(), f, tc.parentZone, tc.zoneName, tc.nameServers)
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			if created || f.called {
				t.Errorf("expected no-op for %q (created=%v called=%v)", tc.name, created, f.called)
			}
		})
	}
}

func TestEnsureZoneDelegation_CreatesWhenAbsent(t *testing.T) {
	f := &fakeRoute53{} // no existing records
	ns := []string{"ns-1.awsdns-00.com", "ns-2.awsdns-00.net"}
	created, err := EnsureZoneDelegation(context.Background(), f, "ZPARENT", "acct.compute.pennsieve.net", ns)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !created {
		t.Error("expected created=true when the NS record did not exist (loop-guard signal)")
	}
	change := f.input.ChangeBatch.Changes[0]
	if change.Action != types.ChangeActionUpsert || change.ResourceRecordSet.Type != types.RRTypeNs {
		t.Errorf("expected NS UPSERT, got %v %v", change.Action, change.ResourceRecordSet.Type)
	}
	if len(change.ResourceRecordSet.ResourceRecords) != 2 || *f.input.HostedZoneId != "ZPARENT" {
		t.Errorf("unexpected change: %+v", change.ResourceRecordSet)
	}
}

func TestEnsureZoneDelegation_NotCreatedWhenPresent(t *testing.T) {
	// Already delegated (trailing dot as Route53 returns it) → created=false so
	// the caller won't re-trigger phase-2 provisioning in a loop.
	f := &fakeRoute53{existing: []types.ResourceRecordSet{nsRRS("acct.compute.pennsieve.net.")}}
	created, err := EnsureZoneDelegation(context.Background(), f, "ZPARENT", "acct.compute.pennsieve.net", []string{"ns-1.aws"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created {
		t.Error("expected created=false when the NS record already exists")
	}
	if !f.called {
		t.Error("should still UPSERT (refresh) even when present")
	}
}

func TestEnsureZoneDelegation_PropagatesError(t *testing.T) {
	f := &fakeRoute53{err: errors.New("boom")}
	_, err := EnsureZoneDelegation(context.Background(), f, "ZPARENT", "acct.compute.pennsieve.net", []string{"ns-1.aws"})
	if err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestRemoveZoneDelegation(t *testing.T) {
	t.Run("deletes when present", func(t *testing.T) {
		f := &fakeRoute53{existing: []types.ResourceRecordSet{nsRRS("acct.compute.pennsieve.net.")}}
		if err := RemoveZoneDelegation(context.Background(), f, "ZPARENT", "acct.compute.pennsieve.net"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !f.called || f.input.ChangeBatch.Changes[0].Action != types.ChangeActionDelete {
			t.Errorf("expected a DELETE change, got called=%v", f.called)
		}
	})
	t.Run("no-op when absent", func(t *testing.T) {
		f := &fakeRoute53{} // nothing existing
		if err := RemoveZoneDelegation(context.Background(), f, "ZPARENT", "acct.compute.pennsieve.net"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if f.called {
			t.Error("expected no DELETE when the record is absent")
		}
	})
	t.Run("no-op on empty args", func(t *testing.T) {
		f := &fakeRoute53{}
		_ = RemoveZoneDelegation(context.Background(), f, "", "acct.compute.pennsieve.net")
		if f.called {
			t.Error("expected no-op on empty parent zone")
		}
	})
}
