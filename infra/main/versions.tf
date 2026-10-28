# Everything the demo environment needs (plans/phase-5.md). The state lives in
# the bucket created by infra/bootstrap.
#
#   cd infra/main
#   AWS_PROFILE=Teiyou terraform init
#   AWS_PROFILE=Teiyou terraform plan
#   AWS_PROFILE=Teiyou terraform apply

terraform {
  required_version = "~> 1.17"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.70"
    }
  }

  # Backend settings cannot use variables, so the bucket name is written here.
  backend "s3" {
    bucket  = "mogumogu-tfstate-teiyou416"
    key     = "main/terraform.tfstate"
    region  = "ap-northeast-1"
    encrypt = true
    # A lock file next to the state stops two applies running at once.
    use_lockfile = true
  }
}

provider "aws" {
  region = var.region

  default_tags {
    tags = {
      Project   = "mogumogu"
      ManagedBy = "terraform/main"
    }
  }
}
