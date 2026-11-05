variable "region" {
  description = "AWS region of the Organization, the only one member accounts may use besides global services."
  type        = string
  default     = "us-east-1"
}

variable "accounts" {
  description = "The member accounts, dev and prod, each with its name and its root user's email address. dev already exists and keeps the name and email it has."
  type = map(object({
    name  = string
    email = string
  }))

  validation {
    condition     = toset(keys(var.accounts)) == toset(["dev", "prod"])
    error_message = "Give the dev and the prod account."
  }
}

variable "dev_account_id" {
  description = "ID of the dev account, which joins the Organization by invitation and is imported."
  type        = string

  validation {
    condition     = can(regex("^[0-9]{12}$", var.dev_account_id))
    error_message = "An account ID is 12 digits."
  }
}

variable "administrators" {
  description = "User names in IAM Identity Center of the people who administer every account."
  type        = list(string)
}

variable "alert_emails" {
  description = "Email addresses that get the budget and cost anomaly alerts."
  type        = list(string)
}

variable "monthly_budgets_usd" {
  description = "Monthly budget of each account in USD, warned about before it is reached."
  type        = map(number)
  default = {
    management = 10
    dev        = 300
    prod       = 1000
  }

  validation {
    condition     = toset(keys(var.monthly_budgets_usd)) == toset(["management", "dev", "prod"])
    error_message = "Give a budget for management, dev and prod."
  }
}
