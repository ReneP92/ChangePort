# The bucket that holds an account's Terraform state. Terraform locks the
# state with a lock file next to it in the bucket (use_lockfile), so there is
# no DynamoDB table. `just tf-bootstrap <account>` creates the bucket with
# local state and then moves that state into it.

resource "aws_s3_bucket" "state" {
  bucket = "${var.name}-terraform-state"
}

resource "aws_s3_bucket_public_access_block" "state" {
  bucket = aws_s3_bucket.state.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# Object ACLs are off: the bucket owns everything written to it.
resource "aws_s3_bucket_ownership_controls" "state" {
  bucket = aws_s3_bucket.state.id

  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "state" {
  bucket = aws_s3_bucket.state.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

# Every apply writes a new version, so a broken state can be rolled back.
resource "aws_s3_bucket_versioning" "state" {
  bucket = aws_s3_bucket.state.id

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "state" {
  bucket = aws_s3_bucket.state.id

  rule {
    id     = "expire-old-versions"
    status = "Enabled"
    filter {}

    noncurrent_version_expiration {
      noncurrent_days = var.noncurrent_version_expiration_days
    }
    abort_incomplete_multipart_upload {
      days_after_initiation = 1
    }
  }

  depends_on = [aws_s3_bucket_versioning.state]
}

# Requests without TLS, or with TLS older than 1.2, are refused.
resource "aws_s3_bucket_policy" "state" {
  bucket = aws_s3_bucket.state.id
  policy = jsonencode(local.bucket_policy)

  depends_on = [aws_s3_bucket_public_access_block.state]
}

locals {
  bucket_and_objects = [aws_s3_bucket.state.arn, "${aws_s3_bucket.state.arn}/*"]

  bucket_policy = {
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "DenyInsecureTransport"
        Effect    = "Deny"
        Principal = "*"
        Action    = "s3:*"
        Resource  = local.bucket_and_objects
        Condition = { Bool = { "aws:SecureTransport" = "false" } }
      },
      {
        Sid       = "DenyOldTLS"
        Effect    = "Deny"
        Principal = "*"
        Action    = "s3:*"
        Resource  = local.bucket_and_objects
        Condition = { NumericLessThan = { "s3:TlsVersion" = "1.2" } }
      },
    ]
  }
}
