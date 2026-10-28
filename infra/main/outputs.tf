# Contain the account ID: fine in your terminal, but do not paste them into
# public places.
output "ecr_repositories" {
  value = { for k, r in aws_ecr_repository.app : k => r.repository_url }
}

output "deploy_bucket" {
  value = aws_s3_bucket.deploy.bucket
}

# Point the A record for var.domain at this address (at name.com).
output "public_ip" {
  value = aws_eip.app.public_ip
}

output "instance_id" {
  value = aws_instance.app.id
}
