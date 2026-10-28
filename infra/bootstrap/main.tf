# Creates the S3 bucket that holds the Terraform state of infra/main.
# Run once, by hand. Its own state stays local (and is not committed): the
# bucket cannot store the state of the code that creates it.
#
#   cd infra/bootstrap
#   AWS_PROFILE=Teiyou terraform init
#   AWS_PROFILE=Teiyou terraform apply

terraform {
  required_version = "~> 1.17"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.70"
    }
  }
}

provider "aws" {
  region = "ap-northeast-1"

  default_tags {
    tags = {
      Project   = "mogumogu"
      ManagedBy = "terraform/bootstrap"
    }
  }
}

# Bucket names are global across all AWS accounts. The name is not a secret:
# only identities with permission in this account can read the bucket.
resource "aws_s3_bucket" "state" {
  bucket = "mogumogu-tfstate-teiyou416"

  # Losing the state means Terraform no longer knows what it created.
  lifecycle {
    prevent_destroy = true
  }
}

# Every write keeps the previous version, so a broken state can be restored.
resource "aws_s3_bucket_versioning" "state" {
  bucket = aws_s3_bucket.state.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "state" {
  bucket = aws_s3_bucket.state.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_public_access_block" "state" {
  bucket                  = aws_s3_bucket.state.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# Old state versions are only needed for a while.
resource "aws_s3_bucket_lifecycle_configuration" "state" {
  bucket = aws_s3_bucket.state.id
  rule {
    id     = "expire-old-versions"
    status = "Enabled"
    filter {}
    noncurrent_version_expiration {
      noncurrent_days = 90
    }
  }
}

output "state_bucket" {
  value = aws_s3_bucket.state.bucket
}
