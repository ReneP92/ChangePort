# The AWS Organization, run from its management account, which holds nothing
# but billing, single sign-on and these guardrails: the member accounts, a
# service control policy every member account is held to, root access
# managed centrally, and one CloudTrail trail for every account.

data "aws_caller_identity" "current" {}

data "aws_partition" "current" {}

locals {
  account   = data.aws_caller_identity.current.account_id
  partition = data.aws_partition.current.partition
}

# The Organization is created in the console first, as IAM Identity Center
# needs it before anyone can run Terraform, and imported
# (terraform/environments/management).
resource "aws_organizations_organization" "this" {
  feature_set = "ALL"

  aws_service_access_principals = [
    "cloudtrail.amazonaws.com",
    # Manages the member accounts' root users (aws_iam_organizations_features).
    "iam.amazonaws.com",
    # IAM Identity Center (terraform/modules/identity-center).
    "sso.amazonaws.com",
  ]
  enabled_policy_types = ["SERVICE_CONTROL_POLICY"]
}

# An account created here gets no root password; one that joins by
# invitation is imported, and its root credentials are deleted afterwards
# (terraform/README.md).
resource "aws_organizations_account" "member" {
  for_each = var.accounts

  name  = each.value.name
  email = each.value.email

  lifecycle {
    # Removing an account from the Organization takes its workloads out of
    # every guardrail; it is never what an apply should do.
    prevent_destroy = true
    # Only used when an account is created, and unknown for an imported one,
    # so a difference would replace the account.
    ignore_changes = [iam_user_access_to_billing, role_name]
  }
}

# Member accounts have no root credentials; when one needs its root user, for
# example to unlock a bucket policy that locks everyone out, the management
# account opens a short root session in it (sts:AssumeRoot).
resource "aws_iam_organizations_features" "this" {
  enabled_features = ["RootCredentialsManagement", "RootSessions"]

  depends_on = [aws_organizations_organization.this]
}

# Service control policies hold every member account, never the management
# account; at most five may apply to an account, so the guardrails share one.
resource "aws_organizations_policy" "guardrails" {
  name        = "${var.name}-guardrails"
  description = "Only ${var.region} and global services, no leaving the Organization, no tampering with CloudTrail"
  type        = "SERVICE_CONTROL_POLICY"
  content     = jsonencode(local.guardrails)
}

resource "aws_organizations_policy_attachment" "guardrails" {
  policy_id = aws_organizations_policy.guardrails.id
  target_id = aws_organizations_organization.this.roots[0].id
}

locals {
  # Global services, whose requests can name another region.
  global_actions = [
    "account:*",
    "aws-marketplace-management:*",
    "aws-marketplace:*",
    "aws-portal:*",
    "billing:*",
    "budgets:*",
    "ce:*",
    "cloudfront:*",
    "consolidatedbilling:*",
    "cur:*",
    "ec2:DescribeRegions",
    "freetier:*",
    "globalaccelerator:*",
    "health:*",
    "iam:*",
    "invoicing:*",
    "organizations:*",
    "payments:*",
    "pricing:*",
    "route53:*",
    "route53domains:*",
    "s3:GetAccountPublicAccessBlock",
    "s3:ListAllMyBuckets",
    "shield:*",
    "sts:*",
    "support:*",
    "tax:*",
    "trustedadvisor:*",
    "waf:*",
  ]

  guardrails = {
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "OnlyOurRegion"
        Effect    = "Deny"
        NotAction = local.global_actions
        Resource  = "*"
        Condition = { StringNotEquals = { "aws:RequestedRegion" = [var.region] } }
      },
      {
        Sid      = "StayInTheOrganization"
        Effect   = "Deny"
        Action   = "organizations:LeaveOrganization"
        Resource = "*"
      },
      {
        # The organization trail lives in the management account; member
        # accounts have no trails of their own to stop either.
        Sid    = "KeepCloudTrail"
        Effect = "Deny"
        Action = [
          "cloudtrail:DeleteTrail",
          "cloudtrail:PutEventSelectors",
          "cloudtrail:PutInsightSelectors",
          "cloudtrail:StopLogging",
          "cloudtrail:UpdateTrail",
        ]
        Resource = "*"
      },
    ]
  }
}
