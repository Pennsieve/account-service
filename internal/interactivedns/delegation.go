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

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/route53/types"
)

// Route53API is the subset of the Route53 client this package uses (kept small
// for testability).
type Route53API interface {
	ChangeResourceRecordSets(ctx context.Context, params *route53.ChangeResourceRecordSetsInput, optFns ...func(*route53.Options)) (*route53.ChangeResourceRecordSetsOutput, error)
}

// EnsureZoneDelegation upserts an NS record for zoneName in the Pennsieve parent
// hosted zone (parentZoneID), pointing at nameServers. Idempotent: an UPSERT
// refreshes the record if the node's zone was recreated with new name servers.
//
// It is a no-op (nil) when zoneName, nameServers, or parentZoneID is empty so
// callers can invoke it unconditionally for any provisioning event.
func EnsureZoneDelegation(ctx context.Context, r53 Route53API, parentZoneID, zoneName string, nameServers []string) error {
	if parentZoneID == "" || zoneName == "" || len(nameServers) == 0 {
		return nil
	}

	records := make([]types.ResourceRecord, 0, len(nameServers))
	for _, ns := range nameServers {
		if ns == "" {
			continue
		}
		records = append(records, types.ResourceRecord{Value: aws.String(ns)})
	}
	if len(records) == 0 {
		return nil
	}

	_, err := r53.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
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
		return fmt.Errorf("upserting NS delegation for %s in parent zone %s: %w", zoneName, parentZoneID, err)
	}
	return nil
}
