variable "name" {
  description = "Name prefix of the push role, for example changeport-prod."
  type        = string
}

variable "repositories" {
  description = "The images, each pushed to the repository changeport/<image>."
  type        = list(string)
  default     = ["kafka", "connect", "agent", "registry-gateway", "provisioner", "console"]
}

variable "github_repository_subject" {
  description = "The GitHub repository as its OpenID Connect tokens' sub claim names it: owner@owner-id/name@repository-id, as the repository uses immutable subjects, which survive a rename (gh api repos/<owner>/<name>/actions/oidc/customization/sub). IAM compares it case-sensitively."
  type        = string

  validation {
    condition     = can(regex("^[A-Za-z0-9_.-]+@[0-9]+/[A-Za-z0-9_.-]+@[0-9]+$", var.github_repository_subject))
    error_message = "Name the repository as owner@owner-id/name@repository-id, as its tokens' sub claim does."
  }
}

variable "github_oidc_provider_arn" {
  description = "ARN of the account's GitHub Actions OpenID Connect provider (terraform/modules/github-oidc)."
  type        = string
}

variable "keep_tagged_images" {
  description = "How many of the newest tagged images each repository keeps."
  type        = number
  default     = 200

  validation {
    condition     = var.keep_tagged_images >= 50
    error_message = "Keep at least 50 images, so the pinned digests survive."
  }
}
