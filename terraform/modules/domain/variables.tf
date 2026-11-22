variable "domain" {
  description = "Changeport's domain, such as changeport.app, whose zone is on Cloudflare, its registrar."
  type        = string

  validation {
    condition     = can(regex("^([a-z0-9]([a-z0-9-]*[a-z0-9])?\\.)+[a-z]{2,}$", var.domain))
    error_message = "Name the domain in lowercase, without a scheme or a trailing dot, such as changeport.app."
  }
}
