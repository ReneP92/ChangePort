variable "name" {
  description = "Name prefix of the plan role, for example changeport-dev."
  type        = string
}

variable "github_repository_subject" {
  description = "The GitHub repository as its OpenID Connect tokens' sub claim names it: owner@owner-id/name@repository-id, as the repository uses immutable subjects, which survive a rename (gh api repos/<owner>/<name>/actions/oidc/customization/sub). IAM compares it case-sensitively."
  type        = string

  validation {
    condition     = can(regex("^[A-Za-z0-9_.-]+@[0-9]+/[A-Za-z0-9_.-]+@[0-9]+$", var.github_repository_subject))
    error_message = "Name the repository as owner@owner-id/name@repository-id, as its tokens' sub claim does."
  }
}

variable "state_bucket_arn" {
  description = "ARN of the account's Terraform state bucket, the only bucket whose objects the plan role may read."
  type        = string
}
