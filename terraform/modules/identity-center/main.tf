# Who signs in to which account: people sign in through IAM Identity Center,
# and nobody has an IAM user. Its instance and the users are made in the
# console (terraform/README.md); the permission sets, the administrators'
# group and what it may do in each account are made here.

data "aws_ssoadmin_instances" "this" {}

locals {
  instance_arn      = one(data.aws_ssoadmin_instances.this.arns)
  identity_store_id = one(data.aws_ssoadmin_instances.this.identity_store_ids)

  # Each permission set is one AWS managed policy. The EKS clusters make the
  # AdministratorAccess roles their administrators
  # (administrator_permission_sets in terraform/modules/eks).
  permission_sets = {
    AdministratorAccess = "Full access, for changes"
    ViewOnlyAccess      = "Sees resources and their configuration, not the data in them"
  }

  assignments = { for pair in setproduct(keys(local.permission_sets), keys(var.account_ids)) :
    "${pair[0]}/${pair[1]}" => { permission_set = pair[0], account_id = var.account_ids[pair[1]] }
  }
}

resource "aws_ssoadmin_permission_set" "this" {
  for_each = local.permission_sets

  name             = each.key
  description      = each.value
  instance_arn     = local.instance_arn
  session_duration = var.session_duration
}

resource "aws_ssoadmin_managed_policy_attachment" "this" {
  for_each = local.permission_sets

  instance_arn       = local.instance_arn
  permission_set_arn = aws_ssoadmin_permission_set.this[each.key].arn
  managed_policy_arn = "arn:aws:iam::aws:policy/${each.key == "ViewOnlyAccess" ? "job-function/" : ""}${each.key}"
}

resource "aws_identitystore_group" "administrators" {
  identity_store_id = local.identity_store_id
  display_name      = "${var.name}-administrators"
  description       = "Administer every account of the Organization"
}

data "aws_identitystore_user" "administrator" {
  for_each = toset(var.administrators)

  identity_store_id = local.identity_store_id

  alternate_identifier {
    unique_attribute {
      attribute_path  = "UserName"
      attribute_value = each.key
    }
  }
}

resource "aws_identitystore_group_membership" "administrator" {
  for_each = data.aws_identitystore_user.administrator

  identity_store_id = local.identity_store_id
  group_id          = aws_identitystore_group.administrators.group_id
  member_id         = each.value.user_id
}

# Administrators may sign in to every account with either permission set,
# and pick the one the task needs.
resource "aws_ssoadmin_account_assignment" "administrators" {
  for_each = local.assignments

  instance_arn       = local.instance_arn
  permission_set_arn = aws_ssoadmin_permission_set.this[each.value.permission_set].arn
  principal_type     = "GROUP"
  principal_id       = aws_identitystore_group.administrators.group_id
  target_type        = "AWS_ACCOUNT"
  target_id          = each.value.account_id

  # A permission set's role in an account is made with its first assignment,
  # so the policy must be attached first.
  depends_on = [aws_ssoadmin_managed_policy_attachment.this]
}
