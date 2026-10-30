# Container logs (plans/observability.md). Docker's awslogs driver on the
# instance writes every container's output here, one stream per container;
# the instance role may only write to this group (iam.tf).

resource "aws_cloudwatch_log_group" "app" {
  name = "/mogumogu/app"
  # Nginx lines carry the client IP, so logs are not kept for long.
  retention_in_days = 14
}

# --- One alarm: server errors seen by visitors ---

# Counts Nginx access lines with a 5xx status. Only Nginx lines have
# remote_addr; the backend logs the same request again without it, so
# matching on remote_addr counts each request once.
resource "aws_cloudwatch_log_metric_filter" "http_5xx" {
  name           = "http-5xx"
  log_group_name = aws_cloudwatch_log_group.app.name
  pattern        = "{ ($.status >= 500) && ($.remote_addr = \"*\") }"

  metric_transformation {
    namespace = "mogumogu"
    name      = "Http5xx"
    value     = "1"
    unit      = "Count"
  }
}

resource "aws_sns_topic" "alerts" {
  name = "mogumogu-alerts"
}

# AWS emails a confirmation link after apply; alerts arrive only once it is
# clicked. Same address as the budget alerts.
resource "aws_sns_topic_subscription" "alerts_email" {
  topic_arn = aws_sns_topic.alerts.arn
  protocol  = "email"
  endpoint  = var.budget_email
}

# Fires on any 5xx within 5 minutes, and mails again when it clears. No
# requests means no data, which counts as fine. A dead instance writes no
# logs, so this alarm cannot notice that.
resource "aws_cloudwatch_metric_alarm" "http_5xx" {
  alarm_name        = "mogumogu-http-5xx"
  alarm_description = "Visitors got a 5xx from the site. See docs/operations.md, 6.6."
  namespace         = "mogumogu"
  metric_name       = "Http5xx"
  statistic         = "Sum"
  period            = 300

  evaluation_periods  = 1
  threshold           = 1
  comparison_operator = "GreaterThanOrEqualToThreshold"
  treat_missing_data  = "notBreaching"

  alarm_actions = [aws_sns_topic.alerts.arn]
  ok_actions    = [aws_sns_topic.alerts.arn]
}
