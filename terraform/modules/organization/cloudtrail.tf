# One trail records the management events of every account in the
# Organization, in every region, into a bucket in the management account that
# no member account can reach.

locals {
  trail_name = "${var.name}-organization"
  # Built rather than read from the trail, which needs the bucket policy
  # that names it first.
  trail_arn = "arn:${local.partition}:cloudtrail:${var.region}:${local.account}:trail/${local.trail_name}"
}

resource "aws_cloudtrail" "organization" {
  name                          = local.trail_name
  s3_bucket_name                = aws_s3_bucket.cloudtrail.id
  is_organization_trail         = true
  is_multi_region_trail         = true
  include_global_service_events = true
  enable_log_file_validation    = true

  depends_on = [aws_organizations_organization.this, aws_s3_bucket_policy.cloudtrail]
}

resource "aws_s3_bucket" "cloudtrail" {
  bucket = "${var.name}-cloudtrail"
}

resource "aws_s3_bucket_public_access_block" "cloudtrail" {
  bucket = aws_s3_bucket.cloudtrail.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# Object ACLs are off: the bucket owns everything written to it.
resource "aws_s3_bucket_ownership_controls" "cloudtrail" {
  bucket = aws_s3_bucket.cloudtrail.id

  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "cloudtrail" {
  bucket = aws_s3_bucket.cloudtrail.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

# A deleted log file can be recovered until its old version expires.
resource "aws_s3_bucket_versioning" "cloudtrail" {
  bucket = aws_s3_bucket.cloudtrail.id

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "cloudtrail" {
  bucket = aws_s3_bucket.cloudtrail.id

  rule {
    id     = "expire-logs"
    status = "Enabled"
    filter {}

    expiration {
      days = var.trail_retention_days
    }
    noncurrent_version_expiration {
      noncurrent_days = 30
    }
  }

  depends_on = [aws_s3_bucket_versioning.cloudtrail]
}

resource "aws_s3_bucket_policy" "cloudtrail" {
  bucket = aws_s3_bucket.cloudtrail.id
  policy = jsonencode(local.cloudtrail_bucket_policy)

  depends_on = [aws_s3_bucket_public_access_block.cloudtrail]
}

locals {
  cloudtrail_bucket       = aws_s3_bucket.cloudtrail.arn
  cloudtrail_from_trail   = { "aws:SourceArn" = local.trail_arn }
  cloudtrail_owner_writes = { "s3:x-amz-acl" = "bucket-owner-full-control" }

  cloudtrail_bucket_policy = {
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "CloudTrailChecksTheBucket"
        Effect    = "Allow"
        Principal = { Service = "cloudtrail.amazonaws.com" }
        Action    = "s3:GetBucketAcl"
        Resource  = local.cloudtrail_bucket
        Condition = { StringEquals = local.cloudtrail_from_trail }
      },
      {
        # The management account's own events, and every member account's
        # under the Organization's ID.
        Sid       = "CloudTrailWrites"
        Effect    = "Allow"
        Principal = { Service = "cloudtrail.amazonaws.com" }
        Action    = "s3:PutObject"
        Resource = [
          "${local.cloudtrail_bucket}/AWSLogs/${local.account}/*",
          "${local.cloudtrail_bucket}/AWSLogs/${aws_organizations_organization.this.id}/*",
        ]
        Condition = { StringEquals = merge(local.cloudtrail_from_trail, local.cloudtrail_owner_writes) }
      },
      {
        Sid       = "DenyInsecureTransport"
        Effect    = "Deny"
        Principal = "*"
        Action    = "s3:*"
        Resource  = [local.cloudtrail_bucket, "${local.cloudtrail_bucket}/*"]
        Condition = { Bool = { "aws:SecureTransport" = "false" } }
      },
    ]
  }
}
