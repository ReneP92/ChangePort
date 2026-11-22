variable "name" {
  description = "The zone the environment's customers' public names live under, such as kafka.changeport.app: <customer>.<name> (internal/provisioning)."
  type        = string

  validation {
    condition     = can(regex("^([a-z0-9]([a-z0-9-]*[a-z0-9])?\\.)+[a-z]{2,}$", var.name))
    error_message = "Name the zone in lowercase, without a trailing dot, such as kafka.changeport.app."
  }
}

variable "cloudflare_zone_id" {
  description = "ID of the domain's zone on Cloudflare, which delegates this zone to Route 53."
  type        = string
}
