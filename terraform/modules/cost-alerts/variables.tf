variable "name" {
  description = "Name prefix of the budgets, monitors and subscription, for example changeport."
  type        = string
}

variable "accounts" {
  description = "The accounts to watch, by name, each with its ID and its monthly budget in USD."
  type = map(object({
    id                 = string
    monthly_budget_usd = number
  }))

  validation {
    condition     = alltrue([for account in values(var.accounts) : account.monthly_budget_usd > 0])
    error_message = "Give every account a budget above zero."
  }
}

variable "alert_emails" {
  description = "Email addresses that get the budget and anomaly alerts."
  type        = list(string)

  validation {
    condition     = length(var.alert_emails) > 0
    error_message = "Name at least one address, or the alerts reach nobody."
  }
}

variable "anomaly_threshold_usd" {
  description = "Unexpected spend in USD an anomaly must reach before it is emailed."
  type        = number
  default     = 20
}
