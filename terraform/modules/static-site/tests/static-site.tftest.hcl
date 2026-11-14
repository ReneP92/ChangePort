# Applies the module to a mocked AWS provider and checks who may read the
# bucket, what the distribution serves and tells browsers, the records of the
# domain and who may publish, without an AWS account.

mock_provider "aws" {
  mock_data "aws_region" {
    defaults = {
      region = "us-east-1"
    }
  }
  mock_data "aws_cloudfront_cache_policy" {
    defaults = {
      id = "658327ea-f89d-4fab-a63d-7e88639e58f6"
    }
  }
  mock_resource "aws_s3_bucket" {
    defaults = {
      arn                         = "arn:aws:s3:::changeport-test-site"
      bucket_regional_domain_name = "changeport-test-site.s3.us-east-1.amazonaws.com"
    }
  }
  mock_resource "aws_acm_certificate" {
    defaults = {
      arn = "arn:aws:acm:us-east-1:123456789012:certificate/mocked"
      domain_validation_options = [
        {
          domain_name           = "changeport.example"
          resource_record_name  = "_a.changeport.example."
          resource_record_type  = "CNAME"
          resource_record_value = "_a.acm-validations.aws."
        },
        {
          domain_name           = "www.changeport.example"
          resource_record_name  = "_b.www.changeport.example."
          resource_record_type  = "CNAME"
          resource_record_value = "_b.acm-validations.aws."
        },
      ]
    }
  }
  mock_resource "aws_acm_certificate_validation" {
    defaults = {
      certificate_arn = "arn:aws:acm:us-east-1:123456789012:certificate/mocked"
    }
  }
  mock_resource "aws_cloudfront_distribution" {
    defaults = {
      arn            = "arn:aws:cloudfront::123456789012:distribution/EDFDVBD6EXAMPLE"
      id             = "EDFDVBD6EXAMPLE"
      domain_name    = "d111111abcdef8.cloudfront.net"
      hosted_zone_id = "Z2FDTNDATAQYW2"
    }
  }
  mock_resource "aws_cloudfront_function" {
    defaults = {
      arn = "arn:aws:cloudfront::123456789012:function/changeport-test-site-www-redirect"
    }
  }
  mock_resource "aws_iam_role" {
    defaults = {
      arn = "arn:aws:iam::123456789012:role/changeport-test-site-publish"
    }
  }
}

variables {
  name                      = "changeport-test"
  domain                    = "changeport.example"
  zone_id                   = "Z0123456789ABCDEFGHIJ"
  github_repository_subject = "ReneP92@40995280/ChangePort@1412834684"
  github_oidc_provider_arn  = "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com"
}

run "only_the_distribution_reads_the_bucket" {
  command = apply

  assert {
    condition = (
      aws_s3_bucket_public_access_block.site.block_public_acls &&
      aws_s3_bucket_public_access_block.site.block_public_policy &&
      aws_s3_bucket_public_access_block.site.ignore_public_acls &&
      aws_s3_bucket_public_access_block.site.restrict_public_buckets
    )
    error_message = "The bucket is private."
  }
  assert {
    condition = jsondecode(aws_s3_bucket_policy.site.policy).Statement[0] == {
      Sid       = "CloudFrontReads"
      Effect    = "Allow"
      Principal = { Service = "cloudfront.amazonaws.com" }
      Action    = "s3:GetObject"
      Resource  = "arn:aws:s3:::changeport-test-site/*"
      Condition = { StringEquals = { "AWS:SourceArn" = "arn:aws:cloudfront::123456789012:distribution/EDFDVBD6EXAMPLE" } }
    }
    error_message = "Only the site's own distribution may read, and only objects."
  }
  assert {
    condition     = [for statement in jsondecode(aws_s3_bucket_policy.site.policy).Statement : statement.Sid] == ["CloudFrontReads", "DenyInsecureTransport", "DenyOldTLS"]
    error_message = "Requests without TLS 1.2 are refused."
  }
  assert {
    condition     = aws_cloudfront_origin_access_control.site.signing_behavior == "always" && aws_cloudfront_origin_access_control.site.signing_protocol == "sigv4"
    error_message = "CloudFront signs every request to the bucket."
  }
}

run "the_distribution_serves_the_domain_securely" {
  command = apply

  assert {
    condition     = toset(aws_cloudfront_distribution.site.aliases) == toset(["changeport.example", "www.changeport.example"])
    error_message = "The distribution answers for the domain and its www."
  }
  assert {
    condition     = toset(concat([aws_acm_certificate.site.domain_name], tolist(aws_acm_certificate.site.subject_alternative_names))) == toset(["changeport.example", "www.changeport.example"])
    error_message = "The certificate covers the domain and its www."
  }
  assert {
    condition     = toset([for record in aws_route53_record.certificate_validation : record.name]) == toset(["_a.changeport.example.", "_b.www.changeport.example."])
    error_message = "The certificate is validated through the domain's zone."
  }
  assert {
    condition = (
      one(aws_cloudfront_distribution.site.viewer_certificate).minimum_protocol_version == "TLSv1.2_2021" &&
      one(aws_cloudfront_distribution.site.viewer_certificate).ssl_support_method == "sni-only"
    )
    error_message = "Browsers connect with TLS 1.2 or newer."
  }
  assert {
    condition     = one(aws_cloudfront_distribution.site.default_cache_behavior).viewer_protocol_policy == "redirect-to-https"
    error_message = "Plain HTTP redirects to HTTPS."
  }
  assert {
    condition     = toset(one(aws_cloudfront_distribution.site.default_cache_behavior).allowed_methods) == toset(["GET", "HEAD"])
    error_message = "The site is read only."
  }
  assert {
    condition     = one(one(aws_cloudfront_distribution.site.default_cache_behavior).function_association).event_type == "viewer-request"
    error_message = "Requests to www are redirected before anything else."
  }
  assert {
    condition = alltrue([for response in aws_cloudfront_distribution.site.custom_error_response :
      response.response_code == 404 && response.response_page_path == "/404.html"
    ]) && length(aws_cloudfront_distribution.site.custom_error_response) == 2
    error_message = "A page the bucket lacks is the site's 404 page, with a 404."
  }
}

run "the_redirect_names_the_domain" {
  command = apply

  assert {
    condition     = strcontains(aws_cloudfront_function.www_redirect.code, "\"www.changeport.example\"") && strcontains(aws_cloudfront_function.www_redirect.code, "\"https://changeport.example\"")
    error_message = "www.<domain> redirects to https://<domain>."
  }
  assert {
    condition     = aws_cloudfront_function.www_redirect.publish
    error_message = "The function is published, so the distribution runs it."
  }
}

run "browsers_are_told_to_keep_safe" {
  command = apply

  assert {
    condition = (
      one(one(aws_cloudfront_response_headers_policy.site.security_headers_config).strict_transport_security).access_control_max_age_sec >= 31536000 &&
      one(one(aws_cloudfront_response_headers_policy.site.security_headers_config).strict_transport_security).include_subdomains
    )
    error_message = "Browsers keep to HTTPS for the domain and its subdomains for at least a year."
  }
  assert {
    condition     = one(one(aws_cloudfront_response_headers_policy.site.security_headers_config).frame_options).frame_option == "DENY"
    error_message = "No other site may frame the site."
  }
  assert {
    condition     = strcontains(one(one(aws_cloudfront_response_headers_policy.site.security_headers_config).content_security_policy).content_security_policy, "frame-ancestors 'none'")
    error_message = "The Content-Security-Policy refuses framing too."
  }
}

run "the_domain_and_www_point_at_the_distribution" {
  command = apply

  assert {
    condition     = toset(keys(aws_route53_record.site)) == toset(["changeport.example A", "changeport.example AAAA", "www.changeport.example A", "www.changeport.example AAAA"])
    error_message = "The domain and its www resolve over IPv4 and IPv6."
  }
  assert {
    condition     = alltrue([for record in aws_route53_record.site : one(record.alias).name == "d111111abcdef8.cloudfront.net" && record.zone_id == "Z0123456789ABCDEFGHIJ"])
    error_message = "Each record is an alias of the distribution, in the domain's zone."
  }
}

run "only_main_publishes" {
  command = apply

  assert {
    condition     = jsondecode(aws_iam_role.publish.assume_role_policy).Statement[0].Condition.StringEquals == { "token.actions.githubusercontent.com:aud" = "sts.amazonaws.com", "token.actions.githubusercontent.com:sub" = "repo:ReneP92@40995280/ChangePort@1412834684:ref:refs/heads/main" }
    error_message = "Only workflow runs on main may assume the publish role."
  }
  assert {
    condition = [for statement in jsondecode(aws_iam_role_policy.publish.policy).Statement : statement.Resource] == [
      "arn:aws:s3:::changeport-test-site",
      "arn:aws:s3:::changeport-test-site/*",
      "arn:aws:cloudfront::123456789012:distribution/EDFDVBD6EXAMPLE",
    ]
    error_message = "The publish role reaches the site's bucket and distribution only."
  }
  assert {
    condition     = output.publish_role_arn == "arn:aws:iam::123456789012:role/changeport-test-site-publish" && output.bucket == "changeport-test-site" && output.distribution_id == "EDFDVBD6EXAMPLE"
    error_message = "The outputs name what the site workflow needs."
  }
}

run "another_region_is_refused" {
  command = plan

  override_data {
    target = data.aws_region.current
    values = {
      region = "eu-west-1"
    }
  }

  expect_failures = [aws_acm_certificate.site]
}

run "bad_domains_are_refused" {
  command = plan

  variables {
    domain = "https://Changeport.example/"
  }

  expect_failures = [var.domain]
}

# The plain owner/name is not what this repository's tokens carry, so a role
# trusting it could never be assumed.
run "subjects_without_ids_are_refused" {
  command = plan

  variables {
    github_repository_subject = "ReneP92/ChangePort"
  }

  expect_failures = [var.github_repository_subject]
}
