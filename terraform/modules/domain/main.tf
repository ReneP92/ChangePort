# Changeport's domain, registered at Cloudflare, which keeps its zone: a
# registrar's domains use its name servers (docs/PROJECT_PLAN.md, Domain).
# Terraform writes the records it needs there with an API token
# (terraform/README.md); the customer endpoints' zones are delegated to
# Route 53 (terraform/modules/kafka-zone).

data "cloudflare_zone" "domain" {
  filter = {
    name = var.domain
  }
}

# Who may issue certificates for the domain and its names: Let's Encrypt,
# which cert-manager uses, and Amazon, whose ACM certificate the landing
# site's distribution serves (terraform/modules/static-site). Cloudflare adds
# its own certificate authorities to the answer while its Universal SSL is on;
# a vendor that serves one of the domain's names with a certificate of its
# own, such as a custom domain, needs its authority here too. The zones
# delegated to Route 53 say their own.
resource "cloudflare_dns_record" "caa" {
  for_each = toset(["letsencrypt.org", "amazon.com"])

  zone_id = data.cloudflare_zone.domain.zone_id
  name    = var.domain
  type    = "CAA"
  ttl     = 3600
  proxied = false
  data = {
    flags = 0
    tag   = "issue"
    value = each.key
  }
  comment = "Changeport's Terraform"
}
