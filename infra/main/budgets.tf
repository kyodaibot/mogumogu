# One monthly cost budget for the whole account, on usage before credits. Emails go out when the actual
# cost passes 50%, 75% and 100% of the limit, and when AWS forecasts it will
# pass the limit by month end. Forecasts need some spending history, so that
# one may stay quiet at first.
resource "aws_budgets_budget" "monthly" {
  name         = "mogumogu-monthly"
  budget_type  = "COST"
  limit_amount = tostring(var.monthly_budget_usd)
  limit_unit   = "USD"
  time_unit    = "MONTHLY"

  # Count usage before credits. With credits included the cost stays near 0
  # and no alert fires until the credits are gone.
  cost_types {
    include_credit = false
  }

  dynamic "notification" {
    for_each = [50, 75]
    content {
      comparison_operator        = "GREATER_THAN"
      threshold                  = notification.value
      threshold_type             = "PERCENTAGE"
      notification_type          = "ACTUAL"
      subscriber_email_addresses = [var.budget_email]
    }
  }

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 100
    threshold_type             = "PERCENTAGE"
    notification_type          = "ACTUAL"
    subscriber_email_addresses = [var.budget_email]
  }

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 100
    threshold_type             = "PERCENTAGE"
    notification_type          = "FORECASTED"
    subscriber_email_addresses = [var.budget_email]
  }
}
