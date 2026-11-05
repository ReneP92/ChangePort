output "provider_arn" {
  description = "ARN of the GitHub Actions OpenID Connect provider, which other roles for GitHub Actions trust."
  value       = aws_iam_openid_connect_provider.github.arn
}

output "plan_role_arn" {
  description = "The role CI plans with: set it as the TERRAFORM_PLAN_ROLE_ARN_<ACCOUNT> repository variable on GitHub."
  value       = aws_iam_role.plan.arn
}
