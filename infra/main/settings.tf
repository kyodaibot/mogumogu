# Non-secret settings deploy.sh writes into the server's .env, kept here so
# variables.tf stays the one place to change them. Secrets are not here; they
# come from scripts/put-secrets.sh.

resource "aws_ssm_parameter" "domain" {
  name  = "/mogumogu/domain"
  type  = "String"
  value = var.domain
}

resource "aws_ssm_parameter" "bedrock_model_id" {
  name  = "/mogumogu/bedrock_model_id"
  type  = "String"
  value = var.bedrock_model_id
}
