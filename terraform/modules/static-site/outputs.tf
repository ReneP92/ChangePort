output "url" {
  description = "Where the site is served."
  value       = "https://${var.domain}"
}

output "bucket" {
  description = "The bucket the site workflow syncs into, the SITE_BUCKET repository variable on GitHub."
  value       = aws_s3_bucket.site.bucket
}

output "distribution_id" {
  description = "The distribution whose cache the site workflow invalidates, the SITE_DISTRIBUTION_ID repository variable on GitHub."
  value       = aws_cloudfront_distribution.site.id
}

output "publish_role_arn" {
  description = "The role the site workflow assumes, the SITE_PUBLISH_ROLE_ARN repository variable on GitHub."
  value       = aws_iam_role.publish.arn
}
