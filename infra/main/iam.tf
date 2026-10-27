# The role the EC2 instance runs as. Everything on the instance (the AI
# service's Bedrock calls, deploy.sh, the SSM agent) gets temporary
# credentials for this role from the instance metadata service; no keys.

resource "aws_iam_role" "app" {
  name = "mogumogu-app"

  # Only the EC2 service may hand this role to an instance.
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

# Lets the SSM agent register the instance: Session Manager log-in and Run
# Command (used by deploy). AWS-managed, the standard choice for this.
resource "aws_iam_role_policy_attachment" "app_ssm" {
  role       = aws_iam_role.app.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

locals {
  account_id = data.aws_caller_identity.current.account_id
  # jp.anthropic.claude-haiku-4-5-... -> anthropic.claude-haiku-4-5-...
  foundation_model_id   = replace(var.bedrock_model_id, "/^[a-z]+\\./", "")
  inference_profile_arn = "arn:aws:bedrock:${var.region}:${local.account_id}:inference-profile/${var.bedrock_model_id}"
}

resource "aws_iam_role_policy" "app" {
  name = "mogumogu-app"
  role = aws_iam_role.app.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      # One model, through the Japan cross-region profile only. A call goes to
      # the profile, which forwards it to the model in one of the Japan
      # regions; both steps need permission. The condition stops the model
      # from being called directly, outside the profile.
      {
        Sid      = "BedrockProfile"
        Effect   = "Allow"
        Action   = "bedrock:InvokeModel"
        Resource = local.inference_profile_arn
      },
      {
        Sid      = "BedrockModelViaProfile"
        Effect   = "Allow"
        Action   = "bedrock:InvokeModel"
        Resource = "arn:aws:bedrock:ap-northeast-*::foundation-model/${local.foundation_model_id}"
        Condition = {
          StringEquals = { "bedrock:InferenceProfileArn" = local.inference_profile_arn }
        }
      },
      # Pull images. The token call cannot be limited to a repository.
      {
        Sid      = "EcrToken"
        Effect   = "Allow"
        Action   = "ecr:GetAuthorizationToken"
        Resource = "*"
      },
      {
        Sid    = "EcrPull"
        Effect = "Allow"
        Action = [
          "ecr:BatchGetImage",
          "ecr:GetDownloadUrlForLayer",
          "ecr:BatchCheckLayerAvailability",
        ]
        Resource = [for r in aws_ecr_repository.app : r.arn]
      },
      # Secrets and settings for .env. Decrypting with the default aws/ssm key
      # needs no extra KMS permission.
      {
        Sid      = "Parameters"
        Effect   = "Allow"
        Action   = ["ssm:GetParameter", "ssm:GetParameters", "ssm:GetParametersByPath"]
        Resource = "arn:aws:ssm:${var.region}:${local.account_id}:parameter/mogumogu/*"
      },
      # Deploy files (read only).
      {
        Sid      = "DeployFiles"
        Effect   = "Allow"
        Action   = "s3:GetObject"
        Resource = "${aws_s3_bucket.deploy.arn}/*"
      },
    ]
  })
}

resource "aws_iam_instance_profile" "app" {
  name = "mogumogu-app"
  role = aws_iam_role.app.name
}
