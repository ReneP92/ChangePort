# The Organization's management account: billing, single sign-on and the
# guardrails over dev and prod. No workload runs here. Setting it up starts
# in the console; terraform/README.md lists the steps.

provider "aws" {
  region = var.region

  default_tags {
    tags = {
      Project     = "changeport"
      Environment = "management"
    }
  }
}

locals {
  name              = "changeport-management"
  github_repository = "ReneP92/ChangePort"
}

data "aws_caller_identity" "current" {}

# The state of this account, which `just tf-bootstrap management` creates.
module "state_backend" {
  source = "../../modules/state-backend"

  name = local.name
}

module "github_oidc" {
  source = "../../modules/github-oidc"

  name              = local.name
  github_repository = local.github_repository
  state_bucket_arn  = module.state_backend.bucket_arn
}

module "organization" {
  source = "../../modules/organization"

  name     = local.name
  region   = var.region
  accounts = var.accounts
}

locals {
  account_ids = merge({ management = data.aws_caller_identity.current.account_id }, module.organization.account_ids)
}

module "identity_center" {
  source = "../../modules/identity-center"

  name           = "changeport"
  administrators = var.administrators
  account_ids    = local.account_ids
}

module "cost_alerts" {
  source = "../../modules/cost-alerts"

  name         = "changeport"
  alert_emails = var.alert_emails
  accounts = { for name, id in local.account_ids :
    name => { id = id, monthly_budget_usd = var.monthly_budgets_usd[name] }
  }
}

# Made in the console before Terraform can run: the Organization, which IAM
# Identity Center needs, and the dev account, which joins by invitation.
data "aws_organizations_organization" "current" {}

import {
  to = module.organization.aws_organizations_organization.this
  id = data.aws_organizations_organization.current.id
}

import {
  to = module.organization.aws_organizations_account.member["dev"]
  id = var.dev_account_id
}
