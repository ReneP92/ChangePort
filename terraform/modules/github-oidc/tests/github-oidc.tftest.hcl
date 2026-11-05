# Applies the module to a mocked AWS provider and checks who may assume the
# plan role and what it may read, without an AWS account.

mock_provider "aws" {
  mock_data "aws_partition" {
    defaults = {
      partition = "aws"
    }
  }
  mock_resource "aws_iam_openid_connect_provider" {
    defaults = {
      arn = "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com"
    }
  }
}

variables {
  name              = "changeport-test"
  github_repository = "ReneP92/ChangePort"
  state_bucket_arn  = "arn:aws:s3:::changeport-test-terraform-state"
}

run "pull_requests_and_main_plan" {
  command = apply

  assert {
    condition     = aws_iam_role.plan.name == "changeport-test-terraform-plan"
    error_message = "The plan role is named after the account."
  }
  assert {
    condition = jsondecode(aws_iam_role.plan.assume_role_policy).Statement[0].Condition.StringEquals == {
      "token.actions.githubusercontent.com:aud" = "sts.amazonaws.com"
      "token.actions.githubusercontent.com:sub" = ["repo:ReneP92/ChangePort:pull_request", "repo:ReneP92/ChangePort:ref:refs/heads/main"]
    }
    error_message = "Only pull requests and pushes to main of the repository may assume the plan role."
  }
}

run "reads_only_what_terraform_needs" {
  command = apply

  assert {
    condition     = aws_iam_role_policy_attachment.plan_read_only.policy_arn == "arn:aws:iam::aws:policy/ReadOnlyAccess"
    error_message = "The plan role reads with ReadOnlyAccess."
  }
  assert {
    condition     = alltrue([for statement in jsondecode(aws_iam_role_policy.plan.policy).Statement : statement.Effect == "Deny" || startswith(join(",", statement.Action), "identitystore:Get")])
    error_message = "Beyond ReadOnlyAccess, the plan role may only look up Identity Center users and groups."
  }
  assert {
    condition = anytrue([for statement in jsondecode(aws_iam_role_policy.plan.policy).Statement :
      statement.Effect == "Deny" && try(statement.NotResource, "") == "arn:aws:s3:::changeport-test-terraform-state/*" && contains(statement.Action, "s3:GetObject")
    ])
    error_message = "The plan role may read no objects but the state."
  }
  assert {
    condition = anytrue([for statement in jsondecode(aws_iam_role_policy.plan.policy).Statement :
      statement.Effect == "Deny" && contains(statement.Action, "secretsmanager:GetSecretValue") && contains(statement.Action, "kms:Decrypt")
    ])
    error_message = "The plan role may not read secrets or decrypt."
  }
}
