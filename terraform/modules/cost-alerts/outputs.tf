output "budgets" {
  description = "Names of the budgets, by account."
  value       = { for account, budget in aws_budgets_budget.account : account => budget.name }
}
