# Where releases come from: container images in ECR, and the deploy files
# (compose file, Caddyfile, deploy.sh) in a small S3 bucket.

locals {
  services = ["backend", "web", "ai"]
}

resource "aws_ecr_repository" "app" {
  for_each = toset(local.services)
  name     = "mogumogu/${each.key}"

  # A tag is a commit; it must always mean the same image, or a rollback to
  # that commit could deploy something else.
  image_tag_mutability = "IMMUTABLE"

  # Free basic scan for known vulnerabilities in OS packages.
  image_scanning_configuration {
    scan_on_push = true
  }
}

# Keep the last 10 images per repository; older ones are deleted. Enough to
# roll back a few releases while keeping storage cost near zero.
resource "aws_ecr_lifecycle_policy" "app" {
  for_each   = aws_ecr_repository.app
  repository = each.value.name

  policy = jsonencode({
    rules = [{
      rulePriority = 1
      description  = "Keep the last 10 images"
      selection = {
        tagStatus   = "any"
        countType   = "imageCountMoreThan"
        countNumber = 10
      }
      action = { type = "expire" }
    }]
  })
}

resource "aws_s3_bucket" "deploy" {
  bucket = "mogumogu-deploy-teiyou416"
}

resource "aws_s3_bucket_server_side_encryption_configuration" "deploy" {
  bucket = aws_s3_bucket.deploy.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_public_access_block" "deploy" {
  bucket                  = aws_s3_bucket.deploy.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}
