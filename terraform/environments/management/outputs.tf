output "organization_id" {
  description = "ID of the Organization (o-...)."
  value       = module.organization.organization_id
}

output "account_ids" {
  description = "IDs of the accounts by name, for the changeport-<name> profiles in ~/.aws/config."
  value       = local.account_ids
}

output "cloudtrail_bucket" {
  description = "The bucket the organization trail writes to."
  value       = module.organization.cloudtrail_bucket
}

output "terraform_plan_role_arn" {
  description = "The role CI plans this account with: set it as the TERRAFORM_PLAN_ROLE_ARN_MANAGEMENT repository variable on GitHub."
  value       = module.github_oidc.plan_role_arn
}
