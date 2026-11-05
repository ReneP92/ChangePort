#!/usr/bin/env bash
# Runs the Terraform of one AWS account, in terraform/environments/<account>:
#   bootstrap  creates the account's state bucket and CI's plan role with
#              local state, then moves that state into the bucket; once per
#              account, before anything else
#   plan       plans without the state lock, which a plan does not need, so
#              CI's role may only read; in GitHub Actions it prints only the
#              addresses of what would change, as the log is public
#   apply      plans and applies once you approve
# Credentials come from the AWS CLI profile changeport-<account> (aws sso
# login --profile changeport-<account>) unless AWS_PROFILE is set or GitHub
# Actions provides them. Inputs come from terraform.tfvars next to the
# account's configuration. Bootstrapping or applying management or prod
# needs CONFIRM=<account>.
# Usage: hack/terraform.sh bootstrap|plan|apply management|dev|prod
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

usage="usage: hack/terraform.sh bootstrap|plan|apply management|dev|prod"
action=${1:?$usage}
account=${2:?$usage}
case "$action" in bootstrap | plan | apply) ;; *) die "$usage" ;; esac
case "$account" in management | dev | prod) ;; *) die "unknown account $account; use management, dev or prod" ;; esac
if [[ $action != plan && $account != dev && ${CONFIRM:-} != "$account" ]]; then
  die "$action in $account needs CONFIRM=$account"
fi

command -v aws >/dev/null || die "the AWS CLI is needed"
[[ -n ${GITHUB_ACTIONS:-} ]] || export AWS_PROFILE=${AWS_PROFILE:-changeport-$account}

dir=terraform/environments/$account
bucket=changeport-$account-terraform-state
plan=$BIN_DIR/terraform-$account.tfplan
[[ -f $dir/terraform.tfvars ]] || die "$dir/terraform.tfvars is missing; copy terraform.tfvars.example next to it and fill it in"

identity=$(aws sts get-caller-identity --query Arn --output text) ||
  die "no AWS credentials; run aws sso login --profile ${AWS_PROFILE:-changeport-$account}"
log "Running as $identity"

# Whether the state bucket is this account's, does not exist yet, or belongs
# to another account, which means these are the wrong credentials.
bucket_state() {
  local out
  if out=$(aws s3api head-bucket --bucket "$bucket" 2>&1); then
    echo ours
  elif [[ $out == *"(404)"* || $out == *"Not Found"* ]]; then
    echo missing
  elif [[ $out == *"(403)"* || $out == *"Forbidden"* ]]; then
    echo foreign
  else
    die "cannot reach s3://$bucket: $out"
  fi
}
state=$(bucket_state) || exit 1
case "$state" in
  foreign) die "s3://$bucket belongs to another account; are these the $account account's credentials?" ;;
  missing) [[ $action == bootstrap ]] || die "s3://$bucket does not exist yet; run just tf-bootstrap $account first" ;;
esac

# Prints what the saved plan would change, one resource address per line,
# and adds it to the job's summary in GitHub Actions.
summarize_plan() {
  local changes
  changes=$(terraform -chdir="$dir" show -json "$plan" | jq -r '
    .resource_changes[]? | select(.change.actions != ["no-op"] and .change.actions != ["read"])
    | "\(.change.actions | join("/")) \(.address)"')
  {
    printf '### Terraform plan: %s\n\n' "$account"
    if [[ -z $changes ]]; then
      printf 'No changes.\n'
    else
      fence='```'
      printf '%s\n%s\n%s\n' "$fence" "$changes" "$fence"
    fi
  } | tee -a "${GITHUB_STEP_SUMMARY:-/dev/null}"
}

case "$action" in
  bootstrap)
    if [[ $state == ours ]]; then
      log "s3://$bucket exists already"
      terraform -chdir="$dir" init -input=false
      exit 0
    fi
    # A backend override keeps the state local until the bucket exists.
    override=$dir/backend_override.tf
    trap 'rm -f "$override"' EXIT
    printf 'terraform {\n  backend "local" {}\n}\n' >"$override"
    log "Creating s3://$bucket and the plan role with local state"
    terraform -chdir="$dir" init -input=false -reconfigure
    terraform -chdir="$dir" apply -target=module.state_backend -target=module.github_oidc
    rm "$override"
    log "Moving the state into s3://$bucket"
    terraform -chdir="$dir" init -input=false -migrate-state -force-copy
    rm -f "$dir/terraform.tfstate" "$dir/terraform.tfstate.backup"
    ;;
  plan)
    terraform -chdir="$dir" init -input=false >/dev/null
    if [[ -n ${GITHUB_ACTIONS:-} ]]; then
      terraform -chdir="$dir" plan -input=false -lock=false -out="$plan" >/dev/null
      summarize_plan
    else
      terraform -chdir="$dir" plan -input=false -lock=false -out="$plan"
    fi
    ;;
  apply)
    terraform -chdir="$dir" init -input=false >/dev/null
    terraform -chdir="$dir" apply
    ;;
esac
