output "zone_id" {
  description = "ID of the zone in Route 53, which external-dns and cert-manager may change (terraform/modules/eks)."
  value       = aws_route53_zone.kafka.zone_id
}

output "name_servers" {
  description = "The zone's name servers, which Cloudflare delegates to."
  value       = sort(aws_route53_zone.kafka.name_servers)
}
