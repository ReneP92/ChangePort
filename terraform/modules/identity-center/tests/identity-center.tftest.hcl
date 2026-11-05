# Applies the module to a mocked AWS provider and checks the permission sets
# and who may use them in which account, without an AWS account.

mock_provider "aws" {
  mock_data "aws_ssoadmin_instances" {
    defaults = {
      arns               = ["arn:aws:sso:::instance/ssoins-1234567890abcdef"]
      identity_store_ids = ["d-1234567890"]
    }
  }
  mock_resource "aws_ssoadmin_permission_set" {
    defaults = {
      arn = "arn:aws:sso:::permissionSet/ssoins-1234567890abcdef/ps-1234567890abcdef"
    }
  }
  mock_resource "aws_identitystore_group" {
    defaults = {
      group_id = "9067d5b2c1-11111111-2222-3333-4444-555555555555"
    }
  }
  mock_data "aws_identitystore_user" {
    defaults = {
      user_id = "9067d5b2c1-aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
    }
  }
}

variables {
  name           = "changeport-test"
  administrators = ["rene"]
  account_ids = {
    management = "111111111111"
    dev        = "222222222222"
    prod       = "333333333333"
  }
}

run "permission_sets" {
  command = apply

  assert {
    condition = { for name, attachment in aws_ssoadmin_managed_policy_attachment.this : name => attachment.managed_policy_arn } == {
      AdministratorAccess = "arn:aws:iam::aws:policy/AdministratorAccess"
      ViewOnlyAccess      = "arn:aws:iam::aws:policy/job-function/ViewOnlyAccess"
    }
    error_message = "AdministratorAccess may change everything; ViewOnlyAccess sees configuration but no data."
  }
  assert {
    condition     = output.permission_sets == tolist(["AdministratorAccess", "ViewOnlyAccess"])
    error_message = "The EKS clusters look the AdministratorAccess roles up by the permission set's name."
  }
}

run "administrators_reach_every_account" {
  command = apply

  assert {
    condition     = length(aws_ssoadmin_account_assignment.administrators) == 6
    error_message = "Administrators get both permission sets in all three accounts."
  }
  assert {
    condition     = toset([for assignment in aws_ssoadmin_account_assignment.administrators : assignment.target_id]) == toset(["111111111111", "222222222222", "333333333333"])
    error_message = "Every account is assigned."
  }
  assert {
    condition     = alltrue([for assignment in aws_ssoadmin_account_assignment.administrators : assignment.principal_type == "GROUP"])
    error_message = "Access goes to the group, never to a user directly."
  }
  assert {
    condition     = length(aws_identitystore_group_membership.administrator) == 1
    error_message = "Every administrator is in the group."
  }
}

run "nobody_refused" {
  command = plan

  variables {
    administrators = []
  }

  expect_failures = [var.administrators]
}
