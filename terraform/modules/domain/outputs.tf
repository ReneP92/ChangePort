output "cloudflare_zone_id" {
  description = "ID of the domain's zone on Cloudflare."
  value       = data.cloudflare_zone.domain.zone_id
}
