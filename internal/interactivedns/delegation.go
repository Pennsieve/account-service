// Package interactivedns manages the DNS delegation for interactive (Jupyter)
// session subdomains.
//
// Each compute node account that enables interactive sessions gets its own
// hosted zone {accountKey}.compute.pennsieve.net created in the node's cloud
// account (AWS Route53 today; Azure DNS later). For that subdomain to resolve
// publicly, Pennsieve's parent zone (compute.pennsieve.net, always in
// Pennsieve's AWS Route53) must hold an NS record delegating it to that zone's
// name servers.
//
// This package is deliberately TRANSPORT-AGNOSTIC: it takes the zone name +
// name servers and upserts the delegation, regardless of how those values
// arrived (EventBridge from an AWS provisioner today, an HTTP callback from an
// Azure provisioner tomorrow). The parent zone is always Pennsieve-owned
// Route53, so only the name-server values differ per cloud.
package interactivedns

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/route53/types"
)

// Route53API is the subset of the Route53 client this package uses (kept small
// for testability).
type Route53API interface {
	ChangeResourceRecordSets(ctx context.Context, params *route53.ChangeResourceRecordSetsInput, optFns ...func(*route53.Options)) (*route53.ChangeResourceRecordSetsOutput, error)
	ListResourceRecordSets(ctx context.Context, params *route53.ListResourceRecordSetsInput, optFns ...func(*route53.Options)) (*route53.ListResourceRecordSetsOutput, error)
}

// EnsureZoneDelegation upserts an NS record for zoneName in the Pennsieve parent
// hosted zone (parentZoneID), pointing at nameServers. Idempotent: an UPSERT
// refreshes the record if the node's zone was recreated with new name servers.
//
// Returns created=true only when the NS record did not previously exist — the
// caller uses this as a loop guard (e.g. trigger phase-2 re-provisioning only on
// the first delegation, not on the UPDATE event that phase 2 itself emits).
//
// It is a no-op (created=false, nil) when zoneName, nameServers, or parentZoneID
// is empty, so callers can invoke it unconditionally for any provisioning event.
func EnsureZoneDelegation(ctx context.Context, r53 Route53API, parentZoneID, zoneName string, nameServers []string) (created bool, err error) {
	if parentZoneID == "" || zoneName == "" || len(nameServers) == 0 {
		return false, nil
	}

	records := make([]types.ResourceRecord, 0, len(nameServers))
	for _, ns := range nameServers {
		if ns == "" {
			continue
		}
		records = append(records, types.ResourceRecord{Value: aws.String(ns)})
	}
	if len(records) == 0 {
		return false, nil
	}

	existed, err := nsRecordExists(ctx, r53, parentZoneID, zoneName)
	if err != nil {
		return false, err
	}

	_, err = r53.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(parentZoneID),
		ChangeBatch: &types.ChangeBatch{
			Comment: aws.String("interactive-session subdomain delegation (managed by account-service)"),
			Changes: []types.Change{
				{
					Action: types.ChangeActionUpsert,
					ResourceRecordSet: &types.ResourceRecordSet{
						Name:            aws.String(zoneName),
						Type:            types.RRTypeNs,
						TTL:             aws.Int64(300),
						ResourceRecords: records,
					},
				},
			},
		},
	})
	if err != nil {
		return false, fmt.Errorf("upserting NS delegation for %s in parent zone %s: %w", zoneName, parentZoneID, err)
	}
	return !existed, nil
}

// RemoveZoneDelegation deletes the NS delegation record for zoneName from the
// parent zone. Used on node teardown once the per-account zone is destroyed, so
// the parent zone doesn't keep a dangling delegation. No-op when the record
// isn't there (idempotent) or when args are empty.
func RemoveZoneDelegation(ctx context.Context, r53 Route53API, parentZoneID, zoneName string) error {
	if parentZoneID == "" || zoneName == "" {
		return nil
	}
	existing, err := getNSRecord(ctx, r53, parentZoneID, zoneName)
	if err != nil {
		return err
	}
	if existing == nil {
		return nil // nothing to remove
	}
	_, err = r53.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(parentZoneID),
		ChangeBatch: &types.ChangeBatch{
			Comment: aws.String("interactive-session subdomain delegation removed (node torn down)"),
			Changes: []types.Change{{
				Action:            types.ChangeActionDelete,
				ResourceRecordSet: existing, // must match the live record exactly
			}},
		},
	})
	if err != nil {
		return fmt.Errorf("deleting NS delegation for %s in parent zone %s: %w", zoneName, parentZoneID, err)
	}
	return nil
}

// getNSRecord returns the live NS record set for name in the zone, or nil if absent.
func getNSRecord(ctx context.Context, r53 Route53API, parentZoneID, name string) (*types.ResourceRecordSet, error) {
	fqdn := name
	if !strings.HasSuffix(fqdn, ".") {
		fqdn += "."
	}
	out, err := r53.ListResourceRecordSets(ctx, &route53.ListResourceRecordSetsInput{
		HostedZoneId:    aws.String(parentZoneID),
		StartRecordName: aws.String(name),
		StartRecordType: types.RRTypeNs,
		MaxItems:        aws.Int32(1),
	})
	if err != nil {
		return nil, fmt.Errorf("listing NS record for %s: %w", name, err)
	}
	for i, rr := range out.ResourceRecordSets {
		if rr.Type == types.RRTypeNs && aws.ToString(rr.Name) == fqdn {
			return &out.ResourceRecordSets[i], nil
		}
	}
	return nil, nil
}

func nsRecordExists(ctx context.Context, r53 Route53API, parentZoneID, name string) (bool, error) {
	rr, err := getNSRecord(ctx, r53, parentZoneID, name)
	return rr != nil, err
}
