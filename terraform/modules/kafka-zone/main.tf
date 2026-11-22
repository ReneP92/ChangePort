# The zone of an environment's customer endpoints, such as
# bootstrap.<customer>.kafka.<domain>, in Route 53, where external-dns
# publishes their records and cert-manager proves them to Let's Encrypt
# (terraform/modules/eks). The domain itself stays on Cloudflare, its
# registrar, which delegates this zone here with NS records.

resource "aws_route53_zone" "kafka" {
  name    = var.name
  comment = "Changeport's customer endpoints"

  lifecycle {
    postcondition {
      condition     = length(self.name_servers) == 4
      error_message = "Route 53 gives a zone four name servers, which Cloudflare delegates to."
    }
  }
}

# Only Let's Encrypt issues certificates here, wildcards included, which
# the customers' clusters use (internal/provisioning).
resource "aws_route53_record" "caa" {
  zone_id = aws_route53_zone.kafka.zone_id
  name    = var.name
  type    = "CAA"
  ttl     = 3600
  records = ["0 issue \"letsencrypt.org\""]
}

# The delegation, in the domain's zone on Cloudflare: by count, as the name
# servers are known only once the zone exists.
resource "cloudflare_dns_record" "delegation" {
  count = 4

  zone_id = var.cloudflare_zone_id
  name    = var.name
  type    = "NS"
  content = sort(aws_route53_zone.kafka.name_servers)[count.index]
  ttl     = 3600
  proxied = false
  comment = "Delegates the zone to Route 53 (Changeport's Terraform)"
}
