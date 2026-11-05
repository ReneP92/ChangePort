output "permission_sets" {
  description = "Names of the permission sets administrators sign in with."
  value       = sort(keys(aws_ssoadmin_permission_set.this))
}
