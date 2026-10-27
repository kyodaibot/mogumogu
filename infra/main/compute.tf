# The single application server. It runs the containers with Docker Compose;
# images and deploy files arrive in milestone 2.

# The latest Amazon Linux 2023 image for arm64 (Graviton), published by AWS.
data "aws_ssm_parameter" "al2023_arm64" {
  name = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-arm64"
}

resource "aws_instance" "app" {
  ami                    = data.aws_ssm_parameter.al2023_arm64.value
  instance_type          = var.instance_type
  subnet_id              = aws_subnet.public.id
  vpc_security_group_ids = [aws_security_group.app.id]
  iam_instance_profile   = aws_iam_instance_profile.app.name

  # Burstable instances default to "unlimited" on t4g, which bills for CPU
  # above the baseline. "standard" slows down instead of costing more.
  credit_specification {
    cpu_credits = "standard"
  }

  metadata_options {
    http_endpoint = "enabled"
    # IMDSv2 only: credentials need a session token, which blocks the
    # classic SSRF trick of making the server fetch the metadata URL.
    http_tokens = "required"
    # Containers sit one network hop behind the host; with the default of 1
    # the AI service could not reach the metadata service for credentials.
    http_put_response_hop_limit = 2
  }

  root_block_device {
    volume_size = 20
    volume_type = "gp3"
    encrypted   = true
  }

  user_data = file("${path.module}/user-data.sh")

  tags = { Name = "mogumogu-app" }

  lifecycle {
    # A newer AMI must not replace the running server on the next apply.
    # Replacing it is a deliberate step (terraform apply -replace=...).
    ignore_changes = [ami, user_data]
  }
}

# A fixed public address: the DNS A record at name.com points here and does
# not change when the instance is stopped or replaced.
resource "aws_eip" "app" {
  domain   = "vpc"
  instance = aws_instance.app.id
  tags     = { Name = "mogumogu-app" }
}
