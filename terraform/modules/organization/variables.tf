variable "name" {
  description = "Name prefix of the guardrails, the trail and its bucket, for example changeport-management."
  type        = string
}

variable "region" {
  description = "The only region member accounts may use, besides global services."
  type        = string
}

variable "accounts" {
  description = "The member accounts by environment, each with its name and its root user's email address, which must be unique across AWS. An account that already exists is imported by its ID and keeps the name and email it has."
  type = map(object({
    name  = string
    email = string
  }))

  validation {
    condition     = alltrue([for account in values(var.accounts) : can(regex("^[^@\\s]+@[^@\\s]+\\.[^@\\s]+$", account.email))])
    error_message = "Give every account an email address."
  }
}

variable "trail_retention_days" {
  description = "Days the organization trail's log files are kept."
  type        = number
  default     = 365

  validation {
    condition     = var.trail_retention_days >= 90
    error_message = "Keep the trail's log files for at least 90 days."
  }
}
