# Plans the module against a mocked AWS provider and checks the guardrails,
# the member accounts and the organization trail without an AWS account.
# Only plans: the accounts may not be destroyed, which a test's cleanup would.

mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = {
      account_id = "111111111111"
    }
  }
  mock_data "aws_partition" {
    defaults = {
      partition = "aws"
    }
  }
}

variables {
  name   = "changeport-test"
  region = "us-east-1"
  accounts = {
    dev  = { name = "changeport-dev", email = "dev@example.com" }
    prod = { name = "changeport-prod", email = "prod@example.com" }
  }
}

run "accounts" {
  command = plan

  assert {
    condition     = { for environment, account in aws_organizations_account.member : environment => account.name } == { dev = "changeport-dev", prod = "changeport-prod" }
    error_message = "Every environment gets an account of its own."
  }
  assert {
    condition     = toset(aws_organizations_organization.this.aws_service_access_principals) == toset(["cloudtrail.amazonaws.com", "iam.amazonaws.com", "sso.amazonaws.com"])
    error_message = "The Organization trusts CloudTrail, IAM for root access and IAM Identity Center."
  }
  assert {
    condition     = toset(aws_iam_organizations_features.this.enabled_features) == toset(["RootCredentialsManagement", "RootSessions"])
    error_message = "The member accounts' root users are managed from the management account."
  }
}

run "guardrails" {
  command = plan

  assert {
    condition     = length(aws_organizations_policy.guardrails.content) <= 5120
    error_message = "A service control policy may be at most 5120 characters."
  }
  assert {
    condition = anytrue([for statement in jsondecode(aws_organizations_policy.guardrails.content).Statement :
      statement.Effect == "Deny" && try(statement.Condition.StringNotEquals["aws:RequestedRegion"], []) == ["us-east-1"] && contains(statement.NotAction, "iam:*")
    ])
    error_message = "Member accounts may use us-east-1 and global services only."
  }
  assert {
    condition = anytrue([for statement in jsondecode(aws_organizations_policy.guardrails.content).Statement :
      statement.Effect == "Deny" && try(statement.Action, "") == "organizations:LeaveOrganization"
    ])
    error_message = "Member accounts may not leave the Organization."
  }
  assert {
    condition = anytrue([for statement in jsondecode(aws_organizations_policy.guardrails.content).Statement :
      statement.Effect == "Deny" && contains(flatten([try(statement.Action, [])]), "cloudtrail:StopLogging") && contains(flatten([try(statement.Action, [])]), "cloudtrail:DeleteTrail")
    ])
    error_message = "Member accounts may not stop or delete trails."
  }
}

run "organization_trail" {
  command = plan

  assert {
    condition = alltrue([
      aws_cloudtrail.organization.is_organization_trail,
      aws_cloudtrail.organization.is_multi_region_trail,
      aws_cloudtrail.organization.include_global_service_events,
      aws_cloudtrail.organization.enable_log_file_validation,
    ])
    error_message = "One validated trail records every account in every region, global services included."
  }
  assert {
    condition     = aws_s3_bucket.cloudtrail.bucket == "changeport-test-cloudtrail"
    error_message = "The trail writes to the management account's bucket."
  }
  assert {
    condition     = one(one(aws_s3_bucket_lifecycle_configuration.cloudtrail.rule).expiration).days == 365
    error_message = "The trail's log files are kept for a year."
  }
}

run "short_trail_retention_refused" {
  command = plan

  variables {
    trail_retention_days = 30
  }

  expect_failures = [var.trail_retention_days]
}
