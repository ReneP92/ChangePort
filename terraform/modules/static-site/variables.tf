variable "name" {
  description = "Name prefix of the bucket, the distribution's parts and the publish role, for example changeport-prod."
  type        = string
}

variable "domain" {
  description = "The domain the site is served at, such as changeport.io; www.<domain> redirects to it."
  type        = string

  validation {
    condition     = can(regex("^([a-z0-9]([a-z0-9-]*[a-z0-9])?\\.)+[a-z]{2,}$", var.domain))
    error_message = "Name the domain in lowercase, without a scheme or a trailing dot, such as changeport.io."
  }
}

variable "zone_id" {
  description = "ID of the Route 53 hosted zone of the domain, where the site's records and the certificate's validation go."
  type        = string
}

variable "github_repository" {
  description = "The GitHub repository whose workflows on main may publish, as owner/name, in the case GitHub shows it: the token's sub claim keeps it, and IAM compares it case-sensitively."
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

variable "price_class" {
  description = "Where CloudFront serves from: PriceClass_100 is North America and Europe, where Changeport's customers are; visitors elsewhere are served from there too."
  type        = string
  default     = "PriceClass_100"

  validation {
    condition     = contains(["PriceClass_100", "PriceClass_200", "PriceClass_All"], var.price_class)
    error_message = "Pick PriceClass_100, PriceClass_200 or PriceClass_All."
  }
}
