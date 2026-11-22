# Applies the module to mocked AWS and Cloudflare providers and checks the
# zone, who may issue certificates in it and its delegation from Cloudflare,
# without an AWS account or a Cloudflare token.

mock_provider "aws" {
  mock_resource "aws_route53_zone" {
    defaults = {
      zone_id = "Z0123456789ABCDEFGHIJ"
      # Out of order, as Route 53 lists them.
      name_servers = ["ns-2.awsdns-02.org", "ns-1.awsdns-01.co.uk", "ns-4.awsdns-04.com", "ns-3.awsdns-03.net"]
    }
  }
}

mock_provider "cloudflare" {}

variables {
  name               = "kafka.changeport.example"
  cloudflare_zone_id = "023e105f4ecef8ad9ca31a8372d0c353"
}

run "the_zone_is_the_customer_endpoints" {
  command = apply

  assert {
    condition     = aws_route53_zone.kafka.name == "kafka.changeport.example"
    error_message = "The zone is named as the customers' names end."
  }
  assert {
    condition     = output.zone_id == "Z0123456789ABCDEFGHIJ"
    error_message = "The zone's ID is what external-dns and cert-manager may change."
  }
}

run "only_lets_encrypt_issues_certificates" {
  command = apply

  assert {
    condition     = aws_route53_record.caa.name == "kafka.changeport.example" && aws_route53_record.caa.type == "CAA" && toset(aws_route53_record.caa.records) == toset(["0 issue \"letsencrypt.org\""])
    error_message = "The zone allows Let's Encrypt alone, for wildcards too."
  }
}

run "cloudflare_delegates_the_zone" {
  command = apply

  assert {
    condition     = [for record in cloudflare_dns_record.delegation : record.content] == ["ns-1.awsdns-01.co.uk", "ns-2.awsdns-02.org", "ns-3.awsdns-03.net", "ns-4.awsdns-04.com"]
    error_message = "Each of the zone's name servers is delegated to, in a stable order."
  }
  assert {
    condition     = alltrue([for record in cloudflare_dns_record.delegation : record.type == "NS" && record.name == "kafka.changeport.example" && record.zone_id == "023e105f4ecef8ad9ca31a8372d0c353" && !record.proxied])
    error_message = "The delegation is NS records of the zone's name, in the domain's zone on Cloudflare."
  }
}

run "four_name_servers_or_none" {
  command = apply
  # A zone of its own, not the one the runs above created.
  state_key = "three_name_servers"

  override_resource {
    target = aws_route53_zone.kafka
    values = {
      name_servers = ["ns-1.awsdns-01.co.uk", "ns-2.awsdns-02.org", "ns-3.awsdns-03.net"]
    }
  }

  expect_failures = [aws_route53_zone.kafka]
}
