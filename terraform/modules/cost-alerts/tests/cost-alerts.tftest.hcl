# Applies the module to a mocked AWS provider and checks the budgets and the
# anomaly alerts of each account, without an AWS account.

mock_provider "aws" {
  mock_resource "aws_ce_anomaly_monitor" {
    defaults = {
      arn = "arn:aws:ce::111111111111:anomalymonitor/mocked"
    }
  }
}

variables {
  name         = "changeport-test"
  alert_emails = ["alerts@example.com"]
  accounts = {
    dev  = { id = "222222222222", monthly_budget_usd = 300 }
    prod = { id = "333333333333", monthly_budget_usd = 1000 }
  }
}

run "budgets" {
  command = apply

  assert {
    condition     = { for account, budget in aws_budgets_budget.account : account => budget.limit_amount } == { dev = "300", prod = "1000" }
    error_message = "Every account gets its own monthly budget."
  }
  assert {
    condition     = alltrue([for account, budget in aws_budgets_budget.account : toset(one(budget.cost_filter).values) == toset([var.accounts[account].id])])
    error_message = "A budget counts only its own account's spend."
  }
  assert {
    condition = alltrue([for budget in aws_budgets_budget.account :
      toset([for notification in budget.notification : "${notification.notification_type}/${notification.threshold}"]) == toset(["FORECASTED/100", "ACTUAL/80", "ACTUAL/100"])
    ])
    error_message = "Each budget warns when the forecast passes it, and at 80% and 100% of what was spent."
  }
}

run "anomalies" {
  command = apply

  assert {
    condition     = alltrue([for account, monitor in aws_ce_anomaly_monitor.account : jsondecode(monitor.monitor_specification).Dimensions == { Key = "LINKED_ACCOUNT", MatchOptions = null, Values = [var.accounts[account].id] }])
    error_message = "Every account has an anomaly monitor of its own."
  }
  assert {
    condition     = length(aws_ce_anomaly_subscription.accounts.monitor_arn_list) == 2
    error_message = "One subscription emails the anomalies of every account."
  }
  assert {
    condition     = toset(one(one(aws_ce_anomaly_subscription.accounts.threshold_expression).dimension).values) == toset(["20"])
    error_message = "Anomalies are emailed from $20 of unexpected spend."
  }
}

run "nobody_refused" {
  command = plan

  variables {
    alert_emails = []
  }

  expect_failures = [var.alert_emails]
}
