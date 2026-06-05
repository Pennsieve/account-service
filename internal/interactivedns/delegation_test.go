package interactivedns

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/route53/types"
)

type fakeRoute53 struct {
	called bool
	input  *route53.ChangeResourceRecordSetsInput
	err    error
}

func (f *fakeRoute53) ChangeResourceRecordSets(ctx context.Context, params *route53.ChangeResourceRecordSetsInput, optFns ...func(*route53.Options)) (*route53.ChangeResourceRecordSetsOutput, error) {
	f.called = true
	f.input = params
	return &route53.ChangeResourceRecordSetsOutput{}, f.err
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
			if err := EnsureZoneDelegation(context.Background(), f, tc.parentZone, tc.zoneName, tc.nameServers); err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			if f.called {
				t.Errorf("expected no Route53 call for %q", tc.name)
			}
		})
	}
}

func TestEnsureZoneDelegation_UpsertsNSRecord(t *testing.T) {
	f := &fakeRoute53{}
	ns := []string{"ns-1.awsdns-00.com", "ns-2.awsdns-00.net"}
	err := EnsureZoneDelegation(context.Background(), f, "ZPARENT", "acct.compute.pennsieve.net", ns)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !f.called {
		t.Fatal("expected Route53 ChangeResourceRecordSets to be called")
	}
	change := f.input.ChangeBatch.Changes[0]
	if change.Action != types.ChangeActionUpsert {
		t.Errorf("expected UPSERT, got %v", change.Action)
	}
	rrs := change.ResourceRecordSet
	if rrs.Type != types.RRTypeNs {
		t.Errorf("expected NS record, got %v", rrs.Type)
	}
	if *rrs.Name != "acct.compute.pennsieve.net" {
		t.Errorf("unexpected record name %q", *rrs.Name)
	}
	if len(rrs.ResourceRecords) != 2 {
		t.Fatalf("expected 2 name servers, got %d", len(rrs.ResourceRecords))
	}
	if *f.input.HostedZoneId != "ZPARENT" {
		t.Errorf("unexpected parent zone %q", *f.input.HostedZoneId)
	}
}

func TestEnsureZoneDelegation_PropagatesError(t *testing.T) {
	f := &fakeRoute53{err: errors.New("boom")}
	err := EnsureZoneDelegation(context.Background(), f, "ZPARENT", "acct.compute.pennsieve.net", []string{"ns-1.aws"})
	if err == nil {
		t.Fatal("expected error to propagate")
	}
}
