variable "name" {
  description = "Name prefix of the plan role, for example changeport-dev."
  type        = string
}

variable "github_repository" {
  description = "The GitHub repository whose workflows may plan, as owner/name, in the case GitHub shows it: the token's sub claim keeps it, and IAM compares it case-sensitively."
  type        = string

  validation {
    condition     = can(regex("^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$", var.github_repository))
    error_message = "Name the repository as owner/name."
  }
}

variable "state_bucket_arn" {
  description = "ARN of the account's Terraform state bucket, the only bucket whose objects the plan role may read."
  type        = string
}
