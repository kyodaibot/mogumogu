variable "region" {
  description = "AWS region for every resource; the same region Bedrock is called in."
  type        = string
  default     = "ap-northeast-1"
}

variable "domain" {
  description = "Public host name of the demo. Its A record is kept by hand at name.com."
  type        = string
  default     = "mogumogu.teiyou416.ninja"
}

variable "monthly_budget_usd" {
  description = "Monthly cost (USD) above which AWS Budgets sends an email."
  type        = number
  default     = 50
}

variable "budget_email" {
  description = "Address that receives the budget alerts. Set in terraform.tfvars, not committed."
  type        = string
  # Keeps the address out of plan output.
  sensitive = true
}

variable "db_instance_class" {
  description = "RDS instance size. db.t4g.micro: 2 vCPU (burstable), 1 GB memory."
  type        = string
  default     = "db.t4g.micro"
}

variable "instance_type" {
  description = "EC2 size (arm64). t4g.micro: 2 vCPU (burstable), 1 GB memory; t4g.small doubles the memory."
  type        = string
  default     = "t4g.micro"
}

variable "bedrock_model_id" {
  description = "The only Bedrock inference profile the instance may call; also passed to the AI service."
  type        = string
  default     = "jp.anthropic.claude-haiku-4-5-20251001-v1:0"
}

# The repository allowed to deploy, as it appears at the start of the "sub"
# claim of its OIDC tokens. GitHub writes owner and repository with their
# numeric IDs (immutable subject), so a new account that later takes the same
# names does not match. Look it up with:
#   gh api repos/<owner>/<repo>/actions/oidc/customization/sub
variable "github_subject_prefix" {
  description = "Start of the OIDC sub claim of the repository allowed to deploy."
  type        = string
  default     = "repo:kyodaibot@341212141/mogumogu@1431039109"
}
