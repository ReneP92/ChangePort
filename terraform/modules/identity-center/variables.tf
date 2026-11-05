variable "name" {
  description = "Name prefix of the administrators' group, for example changeport."
  type        = string
}

variable "account_ids" {
  description = "The accounts administrators may sign in to, by name: the management account and every member account."
  type        = map(string)
}

variable "administrators" {
  description = "User names in IAM Identity Center of the people who administer every account."
  type        = list(string)

  validation {
    condition     = length(var.administrators) > 0
    error_message = "Name at least one administrator, or nobody can sign in."
  }
}

variable "session_duration" {
  description = "How long a sign-in to an account lasts, as an ISO 8601 duration."
  type        = string
  default     = "PT8H"
}
