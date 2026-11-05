# Base infrastructure of the prod environment, in its own AWS account of the
# Organization (terraform/environments/management). Nothing here has been
# applied yet; terraform/README.md lists the steps.

provider "aws" {
  region = var.region

  default_tags {
    tags = {
      Project     = "changeport"
      Environment = "prod"
    }
  }
}

locals {
  name              = "changeport-prod"
  github_repository = "ReneP92/ChangePort"
}

# The state of this account, which `just tf-bootstrap prod` creates.
module "state_backend" {
  source = "../../modules/state-backend"

  name = local.name
}

module "github_oidc" {
  source = "../../modules/github-oidc"

  name              = local.name
  github_repository = local.github_repository
  state_bucket_arn  = module.state_backend.bucket_arn
}

module "vpc" {
  source = "../../modules/vpc"

  name               = local.name
  cidr_block         = var.vpc_cidr_block
  availability_zones = var.availability_zones

  # A NAT gateway per zone, so the loss of one zone leaves the others
  # their way to the internet.
  nat_gateway_per_zone = true
}

module "eks" {
  source = "../../modules/eks"

  name               = local.name
  kubernetes_version = var.kubernetes_version
  vpc_id             = module.vpc.vpc_id
  subnet_ids         = module.vpc.private_subnet_ids

  api_allowed_cidrs             = var.api_allowed_cidrs
  administrator_permission_sets = var.administrator_permission_sets
  kafka_dns_zone_ids            = var.kafka_dns_zone_ids

  # The customers' clusters run here.
  deletion_protection = true
}

module "s3" {
  source = "../../modules/s3"

  name = local.name
  # Old versions of deleted or overwritten segments are kept for a week.
  versioning = true
}

module "iam" {
  source = "../../modules/iam"

  name                      = local.name
  cluster_arn               = module.eks.cluster_arn
  tiered_storage_bucket_arn = module.s3.tiered_storage_bucket_arn
}

# The platform's own images, which every account in the organization pulls.
module "ecr" {
  source = "../../modules/ecr"

  name                     = local.name
  github_repository        = local.github_repository
  github_oidc_provider_arn = module.github_oidc.provider_arn
}
