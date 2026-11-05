# Emails about spend, per account, from the management account, which sees
# every account's bill: a monthly budget that warns before it is reached, and
# Cost Anomaly Detection for spend that jumps within a month.

resource "aws_budgets_budget" "account" {
  for_each = var.accounts

  name         = "${var.name}-${each.key}"
  budget_type  = "COST"
  limit_amount = tostring(each.value.monthly_budget_usd)
  limit_unit   = "USD"
  time_unit    = "MONTHLY"

  cost_filter {
    name   = "LinkedAccount"
    values = [each.value.id]
  }

  # Forecast first, so there is time to act, then what was spent.
  notification {
    comparison_operator        = "GREATER_THAN"
    notification_type          = "FORECASTED"
    threshold                  = 100
    threshold_type             = "PERCENTAGE"
    subscriber_email_addresses = var.alert_emails
  }
  notification {
    comparison_operator        = "GREATER_THAN"
    notification_type          = "ACTUAL"
    threshold                  = 80
    threshold_type             = "PERCENTAGE"
    subscriber_email_addresses = var.alert_emails
  }
  notification {
    comparison_operator        = "GREATER_THAN"
    notification_type          = "ACTUAL"
    threshold                  = 100
    threshold_type             = "PERCENTAGE"
    subscriber_email_addresses = var.alert_emails
  }
}

resource "aws_ce_anomaly_monitor" "account" {
  for_each = var.accounts

  name         = "${var.name}-${each.key}"
  monitor_type = "CUSTOM"

  # Every key is given, as AWS returns them, so the plan stays empty.
  monitor_specification = jsonencode({
    And            = null
    CostCategories = null
    Dimensions = {
      Key          = "LINKED_ACCOUNT"
      MatchOptions = null
      Values       = [each.value.id]
    }
    Not  = null
    Or   = null
    Tags = null
  })
}

# Email can only be sent as a daily or weekly summary.
resource "aws_ce_anomaly_subscription" "accounts" {
  name             = var.name
  frequency        = "DAILY"
  monitor_arn_list = [for monitor in aws_ce_anomaly_monitor.account : monitor.arn]

  dynamic "subscriber" {
    for_each = var.alert_emails

    content {
      type    = "EMAIL"
      address = subscriber.value
    }
  }

  threshold_expression {
    dimension {
      key           = "ANOMALY_TOTAL_IMPACT_ABSOLUTE"
      match_options = ["GREATER_THAN_OR_EQUAL"]
      values        = [tostring(var.anomaly_threshold_usd)]
    }
  }
}
