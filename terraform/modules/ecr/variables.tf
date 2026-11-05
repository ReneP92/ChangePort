variable "name" {
  description = "Name prefix of the push role, for example changeport-prod."
  type        = string
}

variable "repositories" {
  description = "The images, each pushed to the repository changeport/<image>."
  type        = list(string)
  default     = ["kafka", "connect", "agent", "registry-gateway", "provisioner", "console"]
}

variable "github_repository" {
  description = "The GitHub repository whose workflows on main may push, as owner/name, in the case GitHub shows it: the token's sub claim keeps it, and IAM compares it case-sensitively."
  type        = string

  validation {
    condition     = can(regex("^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$", var.github_repository))
    error_message = "Name the repository as owner/name."
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
