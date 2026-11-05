# Plans the module against a mocked AWS provider and checks the bucket's
# name, protections and history without an AWS account.

mock_provider "aws" {
  mock_resource "aws_s3_bucket" {
    defaults = {
      arn = "arn:aws:s3:::changeport-test-terraform-state"
    }
  }
}

variables {
  name = "changeport-test"
}

run "bucket" {
  command = plan

  assert {
    condition     = aws_s3_bucket.state.bucket == "changeport-test-terraform-state"
    error_message = "The bucket must be named as the account's backend block expects."
  }
  assert {
    condition = alltrue([
      aws_s3_bucket_public_access_block.state.block_public_acls,
      aws_s3_bucket_public_access_block.state.block_public_policy,
      aws_s3_bucket_public_access_block.state.ignore_public_acls,
      aws_s3_bucket_public_access_block.state.restrict_public_buckets,
    ])
    error_message = "All public access must be blocked."
  }
  assert {
    condition     = one(one(aws_s3_bucket_server_side_encryption_configuration.state.rule).apply_server_side_encryption_by_default).sse_algorithm == "AES256"
    error_message = "The state must be encrypted at rest."
  }
  assert {
    condition     = one(aws_s3_bucket_versioning.state.versioning_configuration).status == "Enabled"
    error_message = "Every version of the state must be kept, so a broken one can be rolled back."
  }
  assert {
    condition     = one(one(aws_s3_bucket_lifecycle_configuration.state.rule).noncurrent_version_expiration).noncurrent_days == 90
    error_message = "Old versions of the state expire after 90 days."
  }
}

# The policy names the bucket's ARN, known once the mocked bucket exists.
run "tls_only" {
  command = apply

  assert {
    condition = anytrue([for statement in jsondecode(aws_s3_bucket_policy.state.policy).Statement :
      statement.Effect == "Deny" && try(statement.Condition.Bool["aws:SecureTransport"], "") == "false"
    ])
    error_message = "Requests without TLS must be denied."
  }
  assert {
    condition = anytrue([for statement in jsondecode(aws_s3_bucket_policy.state.policy).Statement :
      statement.Effect == "Deny" && try(statement.Condition.NumericLessThan["s3:TlsVersion"], "") == "1.2"
    ])
    error_message = "TLS older than 1.2 must be denied."
  }
}

run "short_history_refused" {
  command = plan

  variables {
    noncurrent_version_expiration_days = 7
  }

  expect_failures = [var.noncurrent_version_expiration_days]
}
