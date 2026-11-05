variable "name" {
  description = "Name prefix of the bucket, for example changeport-dev, which makes changeport-dev-terraform-state, the bucket the account's backend block names."
  type        = string
}

variable "noncurrent_version_expiration_days" {
  description = "Days an old version of the state is kept."
  type        = number
  default     = 90

  validation {
    condition     = var.noncurrent_version_expiration_days >= 30
    error_message = "Keep old versions of the state for at least 30 days."
  }
}
