# terraform

The AWS accounts and the base infrastructure of each environment. Every
directory in `environments/` is one AWS account of the Organization and
composes the modules in `modules/`:

- `management`: the Organization, its guardrails, single sign-on and cost
  alerts. No workload runs here.
- `dev` and `prod`: VPC, EKS, S3 and IAM, and in prod the image registry.

Nothing has been applied yet; [Setting up the accounts](#setting-up-the-accounts)
lists the steps.

Per-customer resources are not managed here; the Go provisioning service
creates them through the Kubernetes API.

```bash
just tf-fmt tf-validate tf-test
just tf-plan dev                 # management, dev or prod
just tf-apply dev                # management and prod need CONFIRM=<account>
```

The recipes download the Terraform version that `hack/lib.sh` pins into
`bin/`, checksum-verified, and CI runs the same recipes. `tf-test` runs each
module's `tests/` with `terraform test` against a mocked AWS provider, so it
needs no AWS account. The lock files pin the AWS provider for Linux and macOS
on amd64 and arm64; after changing a provider version, run
`terraform providers lock -platform=linux_amd64 -platform=linux_arm64
-platform=darwin_amd64 -platform=darwin_arm64` in each environment and module
with tests.

Each account's inputs, such as who administers it or where its EKS API may
be reached from, are in `terraform.tfvars` next to its configuration, which
git ignores; `terraform.tfvars.example` lists them. The recipes sign in with
the AWS CLI profile `changeport-<account>` unless `AWS_PROFILE` is set, and
refuse to run when the credentials belong to another account than the state
bucket's.

## Accounts (`environments/management`)

The Organization has three accounts in us-east-1. The management account
holds only billing, single sign-on and the guardrails; workloads run in dev
and prod, and the design partner's cluster in prod from day one.

- **Guardrails** (`modules/organization`): one service control policy holds
  dev and prod (never the management account) to us-east-1 and global
  services, keeps them in the Organization, and stops them from deleting,
  stopping or changing trails. One CloudTrail trail records every account's
  management events in every region into `changeport-management-cloudtrail`,
  with log file validation; log files are kept for a year.
- **Root users:** the member accounts have no root credentials. When one needs
  its root user, for example to unlock a bucket policy that locks everyone
  out, the management account opens a short root session in it
  (`aws sts assume-root`). Only the management account's root user has a
  password, with MFA.
- **Sign-in** (`modules/identity-center`): people sign in through IAM
  Identity Center; nobody has an IAM user or an access key. The group
  `changeport-administrators` may use either permission set in every account:
  `AdministratorAccess` for changes, and `ViewOnlyAccess`, which sees
  resources and their configuration but no data in them. The EKS clusters
  make the `AdministratorAccess` roles their administrators.
- **Cost alerts** (`modules/cost-alerts`): each account has a monthly budget
  (management $10, dev $300, prod $1,000 by default), which emails when the
  forecast passes it and at 80% and 100% of what was spent. Cost Anomaly
  Detection watches each account and emails a daily summary of anomalies
  from $20.

## State (`modules/state-backend`)

Each account keeps its state in a bucket of its own,
`changeport-<account>-terraform-state`: versioned, so a broken state can be
rolled back for 90 days, encrypted, and refusing requests without TLS 1.2.
Terraform locks the state with a lock file next to it (`use_lockfile`), so
there is no DynamoDB table. `just tf-bootstrap <account>` creates the bucket
with local state and moves the state into it, once per account.

## Plans in CI (`modules/github-oidc`)

Each account has a GitHub Actions OpenID Connect provider and a role,
`changeport-<account>-terraform-plan`, that pull requests and pushes to main
of `ReneP92/ChangePort` may assume, with no access key. The role reads with
`ReadOnlyAccess` but is denied the data in the account: objects in buckets
other than the state, secrets, parameters, decryption and logs. Plans run
without the state lock, which a plan does not need, so the role writes
nothing.

The `Terraform plan` jobs in CI plan each account on every pull request and
push to main. The log is public, so they print only the addresses of the
resources that would change; `just tf-plan <account>` shows the details. An
account's job is skipped until it has the repository variable
`TERRAFORM_PLAN_ROLE_ARN_<ACCOUNT>` and the secret
`TERRAFORM_TFVARS_<ACCOUNT>`, the contents of its `terraform.tfvars`.

## Setting up the accounts

Once, in this order. The console steps come first because Terraform needs
the Organization and single sign-on before it can sign in.

1. **Management account.** Sign up a new AWS account for management, turn on
   MFA for its root user, and in AWS Organizations create the Organization.
   Invite the existing dev account (Add an AWS account, Invite) and accept
   the invitation in the dev account.
2. **Single sign-on.** In IAM Identity Center, enable it for the
   Organization in us-east-1 and create your user. Create a temporary
   permission set `Bootstrap` with the `AdministratorAccess` policy and
   assign it to your user in the management account. Open Cost Explorer in
   Billing and Cost Management once, which turns it on for anomaly
   detection.
3. **Organization.** Set up the profile and apply, which imports the
   Organization and the dev account and creates the prod account, the
   guardrails, the trail, the permission sets and the cost alerts:

   ```bash
   aws configure sso --profile changeport-management   # with the Bootstrap permission set
   cp terraform/environments/management/terraform.tfvars.example terraform/environments/management/terraform.tfvars   # and fill it in
   CONFIRM=management just tf-bootstrap management
   CONFIRM=management just tf-apply management
   terraform -chdir=terraform/environments/management output account_ids
   ```

4. **Profiles.** Point `changeport-management` at the `AdministratorAccess`
   permission set and add `changeport-dev` and `changeport-prod` the same way
   with the account IDs above. Then delete the `Bootstrap` permission set.
5. **Root users.** In the management account's IAM console, Root access
   management, delete the dev account's root user credentials. Prod was
   created without any.
6. **dev and prod.** Fill in each `terraform.tfvars` and create the state
   buckets and plan roles. Applying the rest brings up the environments
   (#28, #31).

   ```bash
   just tf-bootstrap dev
   CONFIRM=prod just tf-bootstrap prod
   ```

7. **CI.** Give each account's job its role and inputs:

   ```bash
   for account in management dev prod; do
     key=$(echo "$account" | tr '[:lower:]' '[:upper:]')
     gh variable set "TERRAFORM_PLAN_ROLE_ARN_$key" --body "$(terraform -chdir=terraform/environments/$account output -raw terraform_plan_role_arn)"
     gh secret set "TERRAFORM_TFVARS_$key" <"terraform/environments/$account/terraform.tfvars"
   done
   ```

## Network (`modules/vpc`)

Each environment has one VPC in us-east-1 across three availability zones,
named by zone ID (`use1-az1`, `use1-az2`, `use1-az4`) so they are the same
zones in every account. `use1-az3` is left out because EKS does not run
control planes there.

| | dev | prod |
| --- | --- | --- |
| CIDR block | `10.10.0.0/16` | `10.20.0.0/16` |
| NAT gateways | one, shared | one per zone |

- **Private subnets**, a /18 per zone, hold the EKS nodes and pods: EKS Auto
  Mode gives every pod a VPC address, so they take three quarters of the VPC.
  They carry `kubernetes.io/role/internal-elb` for internal load balancers and
  `changeport.io/customer-nodes=true`, which the per-customer NodeClasses
  select (`eksDedicatedNodes` in `internal/provisioning/spec.go`).
- **Public subnets**, a /24 per zone in the last quarter, hold the NAT
  gateways and the internet-facing Network Load Balancers of the customers'
  Kafka listeners. They carry `kubernetes.io/role/elb`.
- **An S3 gateway endpoint** on the private route tables carries tiered
  storage traffic, which would otherwise pay NAT gateway charges per byte.

Prod has a NAT gateway per zone, so a zone outage cannot cut the other zones
off from the internet; dev shares one to save about $65 a month.

## Cluster (`modules/eks`)

Each environment has one EKS cluster in Auto Mode, on Kubernetes 1.37 with
standard support only, in the private subnets. AWS runs node provisioning, OS
patching, EBS volumes and load balancers.

- **Nodes:** the platform components run on Auto Mode's built-in `system` and
  `general-purpose` node pools. Each customer's brokers run on a NodePool and
  NodeClass of their own, which the provisioning service creates
  (`eksDedicatedNodes` in `internal/provisioning/spec.go`). Those NodeClasses
  use the role `changeport-<env>-customer-node` made here, which has an EC2
  access entry with `AmazonEKSAutoNodePolicy`, and select the cluster security
  group by its `changeport.io/customer-nodes=true` tag. The cluster role may
  tag what it launches, for each customer's `changeport.io/tenant`.
- **Access:** access entries only (`API` mode). The roles IAM Identity Center
  creates for the `administrator_permission_sets` (`AdministratorAccess` by
  default) are cluster administrators. The public API endpoint only answers
  the `api_allowed_cidrs`, which each apply must be given; `0.0.0.0/0` is
  refused.
- **DNS:** once the domain is registered (#8), `kafka_dns_zone_ids` gives
  external-dns and cert-manager Pod Identity roles that may change only the
  `kafka.*` zones. Without zones, neither role exists.
- **Deletion protection** is on in prod and off in dev, which can be rebuilt.

After an apply, sign in through single sign-on and point kubectl at the
cluster:

```bash
aws sso login
aws eks update-kubeconfig --region us-east-1 --name "$(terraform -chdir=terraform/environments/dev output -raw cluster_name)"
kubectl get nodes
```

## Tiered storage (`modules/s3`, `modules/iam`)

Each environment has one bucket for Kafka tiered storage,
`changeport-<env>-tiered-storage`, and one role the brokers assume through
EKS Pod Identity, `changeport-<env>-kafka-broker`, the names
`eksTieredStorage` in `internal/provisioning/spec.go` expects.

- **The bucket** blocks all public access, has object ACLs off, encrypts at
  rest (SSE-S3) and refuses requests without TLS or with TLS older than 1.2.
  Incomplete multipart uploads are aborted after a day. Prod keeps old
  versions of deleted or overwritten objects for a week; dev is unversioned.
- **The role** trusts only the Pod Identity of the environment's own cluster.
  Its policy reaches objects under `${aws:PrincipalTag/kubernetes-namespace}/`
  only: the provisioner associates each customer's broker service account
  with the role, with session tags on, so every session carries the pod's
  namespace, which is also the customer's key prefix (`customer-<id>/`).
  Sessions of namespaces not named `customer-*` get nothing.

After an apply, check with the IAM policy simulator that two customers cannot
reach each other's prefix:

```bash
just tiered-storage-isolation-test dev
```

## Images (`modules/ecr`, prod only)

The platform's own images live in ECR in the prod account: `changeport/kafka`,
`changeport/connect`, `changeport/agent`, `changeport/provisioner` and
`changeport/console`, with
immutable tags and scan on push. Each repository keeps its newest 200 tagged
images; untagged manifests, such as the per-architecture images of a
multi-architecture image, are kept. Every account in the organization may
pull (`aws:PrincipalOrgID`); only `changeport-prod-image-push` may push, and
only GitHub Actions runs for pushes to main of `ReneP92/ChangePort` may assume
it, through the account's OpenID Connect provider (`modules/github-oidc`),
with no access key. IAM compares the repository's name case-sensitively, so
it is written as GitHub shows it.

`.github/workflows/publish-images.yml` builds each image whose sources changed
on main (`hack/changed-images.sh`) for amd64 and arm64, pushes it tagged with
the commit, and opens or updates a pull request from `bot/eks-images` that
sets the new digests in `internal/provisioning/eks-images.json`, which the EKS
platforms embed, and `deploy/components/eks-images` (`go run ./cmd/eksimages`).
Merging that pull request is what changes the images EKS runs; reverting it
goes back. Pull requests the workflow opens do not run CI, so it runs the
tests of those files itself.

Once prod is applied, two settings turn publishing on:

1. The repository variable `ECR_PUSH_ROLE_ARN`, from
   `terraform -chdir=terraform/environments/prod output -raw image_push_role_arn`.
   Until it is set, the workflow does nothing.
2. "Allow GitHub Actions to create and approve pull requests" in the
   repository's Actions settings, for the pull request.

Then run the workflow once by hand (Actions, Publish images, Run workflow) to
publish every image.
