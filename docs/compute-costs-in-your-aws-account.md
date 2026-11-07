# Seeing what Pennsieve compute costs in your AWS account

Pennsieve compute nodes run in your own AWS account, so their cost appears on
your AWS bill. Everything Pennsieve builds there carries tags, and with those
tags AWS can show the cost per compute node.

## 1. Activate the tags for billing

AWS only reports cost by a tag once the tag is activated as a *cost
allocation tag*. Pennsieve can't do this for you: its role has no access to
your billing.

1. Sign in to the AWS account that pays the bill. With AWS Organizations,
   that's the management (payer) account, not the account the nodes run in.
2. Open **Billing and Cost Management → Cost allocation tags**.
3. Under **User-defined cost allocation tags**, select these and choose
   **Activate**:
   - `ComputeNodeId`: the compute node's ID in Pennsieve
   - `NodeIdentifier`: the first eight characters of that ID
   - `ManagedBy`: `pennsieve-compute-provisioner` on everything Pennsieve built
   - `Scope`: `shared` on the network secure nodes in an account share

   To split cost per workflow run as well, also activate these, which the
   tasks a run starts carry:
   - `ExecutionRunId`: the run's ID
   - `WorkflowId`: the workflow's ID
   - `Tenant`: who the run was for, as `org:<id>` (a workspace) or `user:<id>`
   - `AppImageDigest`: the app version's image (`sha256:…`)

   All of these values are IDs, never names. Leave `NodeId` and
   `ProcessorId` inactive: Pennsieve uses them internally, and they aren't
   meant for billing.

   A tag is listed there only after AWS has seen it on a resource, which can
   take up to a day after a node is created or updated.

Costs are reported by tag **from activation onward**, not for earlier months,
and appear in Cost Explorer after up to 24 hours.

## 2. See the cost

In **Cost Explorer**, group by **Tag → `ComputeNodeId`** (or filter on
`ManagedBy` = `pennsieve-compute-provisioner` to see all of Pennsieve's cost
together). The same tags are available in the Cost and Usage Report and in
AWS Budgets, for example for a monthly budget per node.

## What the tags cover

- **Per node**: what each node is built from (its network endpoints, ECS
  cluster, storage, keys, log groups, functions), its GPU instances and the
  tasks its runs start. Nodes get the tags when they are created or next
  updated with the provisioner releases that add them.
- **Per run, workflow and tenant**: the tasks (and their disks) each run
  starts. Apps that run as Lambda functions are shared by every run of that
  app on the node, so their cost is per node and app version, not per run.
- **Not per node**:
  - the network that secure nodes in one account share (its NAT gateway and,
    with notebook sessions, its load balancer): tagged `Scope` = `shared`,
    one cost for all those nodes;
  - data transfer, which AWS doesn't attribute to a tagged resource;
  - untagged cost from before the tags were activated.

## Questions

Pennsieve support can help you read the cost of a node.
