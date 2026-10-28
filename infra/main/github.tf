# Lets the deploy job in GitHub Actions (.github/workflows/ci.yml) work in
# this account without stored keys. On each run GitHub signs a short-lived
# token saying which repository, branch and workflow the run belongs to; AWS
# checks the signature and the conditions below, then hands out temporary
# credentials for the deploy role (valid one hour).

# Tells IAM to trust tokens signed by GitHub Actions. One per account; AWS
# verifies GitHub's certificate itself, so no thumbprint is needed.
resource "aws_iam_openid_connect_provider" "github" {
  url            = "https://token.actions.githubusercontent.com"
  client_id_list = ["sts.amazonaws.com"]
}

resource "aws_iam_role" "deploy" {
  name = "mogumogu-deploy"

  # Only pushes to master in this repository. Pull requests (sub ends in
  # ":pull_request"), other branches, tags and forks get a different "sub"
  # and are refused.
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Federated = aws_iam_openid_connect_provider.github.arn }
      Action    = "sts:AssumeRoleWithWebIdentity"
      Condition = {
        StringEquals = {
          "token.actions.githubusercontent.com:aud" = "sts.amazonaws.com"
          "token.actions.githubusercontent.com:sub" = "repo:${var.github_repository}:ref:refs/heads/master"
        }
      }
    }]
  })
}

# What `make push` and `make deploy` do, and nothing else. Running a command
# on the instance is effectively root there, which is why only master may
# take this role.
resource "aws_iam_role_policy" "deploy" {
  name = "mogumogu-deploy"
  role = aws_iam_role.deploy.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid      = "EcrToken"
        Effect   = "Allow"
        Action   = "ecr:GetAuthorizationToken"
        Resource = "*"
      },
      # Push images, and check whether a tag is already there. Tags are
      # immutable (registry.tf), so an existing image cannot be replaced.
      {
        Sid    = "EcrPush"
        Effect = "Allow"
        Action = [
          "ecr:BatchCheckLayerAvailability",
          "ecr:InitiateLayerUpload",
          "ecr:UploadLayerPart",
          "ecr:CompleteLayerUpload",
          "ecr:PutImage",
          "ecr:BatchGetImage",
          "ecr:DescribeImages",
        ]
        Resource = [for r in aws_ecr_repository.app : r.arn]
      },
      # Upload deploy/ (`aws s3 sync --delete`).
      {
        Sid      = "DeployFilesList"
        Effect   = "Allow"
        Action   = "s3:ListBucket"
        Resource = aws_s3_bucket.deploy.arn
      },
      {
        Sid      = "DeployFiles"
        Effect   = "Allow"
        Action   = ["s3:PutObject", "s3:DeleteObject"]
        Resource = "${aws_s3_bucket.deploy.arn}/*"
      },
      # Find the instance. Describe calls cannot be limited to a resource.
      {
        Sid      = "FindInstance"
        Effect   = "Allow"
        Action   = "ec2:DescribeInstances"
        Resource = "*"
      },
      # Run the shell-script document on this one instance, then read the
      # result (GetCommandInvocation cannot be limited to a resource).
      {
        Sid    = "RunDeploy"
        Effect = "Allow"
        Action = "ssm:SendCommand"
        Resource = [
          "arn:aws:ssm:${var.region}::document/AWS-RunShellScript",
          aws_instance.app.arn,
        ]
      },
      {
        Sid      = "ReadDeployResult"
        Effect   = "Allow"
        Action   = "ssm:GetCommandInvocation"
        Resource = "*"
      },
    ]
  })
}
