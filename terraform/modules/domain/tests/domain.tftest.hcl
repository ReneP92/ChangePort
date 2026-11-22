# Applies the module to a mocked Cloudflare provider and checks who may
# issue certificates for the domain, without a Cloudflare token.

mock_provider "cloudflare" {
  mock_data "cloudflare_zone" {
    defaults = {
      zone_id = "023e105f4ecef8ad9ca31a8372d0c353"
    }
  }
}

variables {
  domain = "changeport.example"
}

run "lets_encrypt_and_amazon_issue_certificates" {
  command = apply

  assert {
    condition     = toset([for record in cloudflare_dns_record.caa : "${record.data.flags} ${record.data.tag} ${record.data.value}"]) == toset(["0 issue letsencrypt.org", "0 issue amazon.com"])
    error_message = "Let's Encrypt, for cert-manager, and Amazon, for the landing site's ACM certificate, may issue certificates, and no one else."
  }
  assert {
    condition     = alltrue([for record in cloudflare_dns_record.caa : record.type == "CAA" && record.name == "changeport.example" && record.zone_id == "023e105f4ecef8ad9ca31a8372d0c353"])
    error_message = "The records are the domain's own, in its zone on Cloudflare."
  }
  assert {
    condition     = output.cloudflare_zone_id == "023e105f4ecef8ad9ca31a8372d0c353"
    error_message = "The zone is found by the domain's name."
  }
}
