output "organization_id" {
  description = "ID of the Organization (o-...)."
  value       = aws_organizations_organization.this.id
}

output "account_ids" {
  description = "IDs of the member accounts by environment."
  value       = { for environment, account in aws_organizations_account.member : environment => account.id }
}

output "cloudtrail_bucket" {
  description = "The bucket the organization trail writes to."
  value       = aws_s3_bucket.cloudtrail.bucket
}
