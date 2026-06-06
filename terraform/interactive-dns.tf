// Interactive-session parent DNS zone.
//
// Per-account interactive (Jupyter) sessions resolve at
// {accountKey}.{interactive_parent_domain}; the per-account zone is created in
// the customer's account by the provisioner and delegated here. This file owns
// the PARENT zone (in the Pennsieve account where account-service runs) and the
// NS delegation from the Pennsieve root zone down to it.
//
// Gated on var.interactive_parent_domain: empty ⇒ nothing is created and
// INTERACTIVE_PARENT_ZONE_ID is "", so the eventbridge handler's NS delegation
// is a no-op. Set the domain per environment to turn the feature's DNS on.
//
// A hosted-zone name is global, so the domain MUST differ per environment
// (e.g. compute-dev.pennsieve.net vs compute.pennsieve.net) and must match the
// provisioner shared-infra `compute_domain` for that env.

locals {
  interactive_dns_enabled = var.interactive_parent_domain != ""
}

resource "aws_route53_zone" "interactive_parent" {
  count = local.interactive_dns_enabled ? 1 : 0
  name  = var.interactive_parent_domain

  tags = {
    Name        = var.interactive_parent_domain
    Environment = var.environment_name
    ManagedBy   = "account-service"
    Purpose     = "interactive-session-parent"
  }
}

// Delegate the parent domain from the Pennsieve root zone (same account, owned
// by us). One NS record; existing root-zone records are untouched.
data "aws_route53_zone" "interactive_root" {
  count        = local.interactive_dns_enabled ? 1 : 0
  name         = var.interactive_root_zone_name
  private_zone = false
}

resource "aws_route53_record" "interactive_delegation" {
  count   = local.interactive_dns_enabled ? 1 : 0
  zone_id = data.aws_route53_zone.interactive_root[0].zone_id
  name    = var.interactive_parent_domain
  type    = "NS"
  ttl     = 300
  records = aws_route53_zone.interactive_parent[0].name_servers
}

output "interactive_parent_zone_id" {
  description = "Hosted zone ID of the interactive parent zone (empty when disabled)."
  value       = try(aws_route53_zone.interactive_parent[0].zone_id, "")
}

output "interactive_parent_name_servers" {
  description = "Name servers of the interactive parent zone (for reference/verification)."
  value       = try(aws_route53_zone.interactive_parent[0].name_servers, [])
}
