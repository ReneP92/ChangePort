# GitHub Actions in this account: the OpenID Connect provider that lets
# workflows assume roles without an access key, and the role CI plans the
# account's Terraform with. Other roles for GitHub Actions, such as the image
# push role in terraform/modules/ecr, trust the same provider.

data "aws_partition" "current" {}

resource "aws_iam_openid_connect_provider" "github" {
  url            = "https://token.actions.githubusercontent.com"
  client_id_list = ["sts.amazonaws.com"]
}

# Plans read everything Terraform manages and change nothing. CI plans
# without the state lock (hack/terraform.sh), so the role needs no write
# access, not even to the lock file.
resource "aws_iam_role" "plan" {
  name        = "${var.name}-terraform-plan"
  description = "Plans this account's Terraform from GitHub Actions; may only read"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Federated = aws_iam_openid_connect_provider.github.arn }
      Action    = "sts:AssumeRoleWithWebIdentity"
      Condition = {
        StringEquals = {
          "token.actions.githubusercontent.com:aud" = "sts.amazonaws.com"
          # Pull requests and pushes to main. Pull requests from forks get
          # no OpenID Connect token, so these are the repository's own.
          "token.actions.githubusercontent.com:sub" = [
            "repo:${var.github_repository}:pull_request",
            "repo:${var.github_repository}:ref:refs/heads/main",
          ]
        }
      }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "plan_read_only" {
  role       = aws_iam_role.plan.name
  policy_arn = "arn:${data.aws_partition.current.partition}:iam::aws:policy/ReadOnlyAccess"
}

# ReadOnlyAccess also reads data, such as customers' tiered storage segments
# and secrets. Terraform needs none of it, so the role is denied all of it
# except the state.
resource "aws_iam_role_policy" "plan" {
  name   = "plan"
  role   = aws_iam_role.plan.id
  policy = jsonencode(local.plan_policy)
}

locals {
  plan_policy = {
    Version = "2012-10-17"
    Statement = [
      {
        Sid         = "NoObjectsButTheState"
        Effect      = "Deny"
        Action      = ["s3:GetObject", "s3:GetObjectVersion"]
        NotResource = "${var.state_bucket_arn}/*"
      },
      {
        Sid    = "NoData"
        Effect = "Deny"
        Action = [
          "dynamodb:BatchGetItem",
          "dynamodb:GetItem",
          "dynamodb:Query",
          "dynamodb:Scan",
          "ec2:GetConsoleOutput",
          "ec2:GetPasswordData",
          "kms:Decrypt",
          "lambda:GetFunction",
          "logs:FilterLogEvents",
          "logs:GetLogEvents",
          "logs:StartQuery",
          "secretsmanager:GetSecretValue",
          "ssm:GetParameter",
          "ssm:GetParameterHistory",
          "ssm:GetParameters",
          "ssm:GetParametersByPath",
        ]
        Resource = "*"
      },
      {
        # Terraform finds Identity Center users and groups by name, which
        # ReadOnlyAccess does not cover.
        Sid      = "IdentityStoreLookups"
        Effect   = "Allow"
        Action   = ["identitystore:GetGroupId", "identitystore:GetGroupMembershipId", "identitystore:GetUserId"]
        Resource = "*"
      },
    ]
  }
}
