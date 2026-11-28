#!/usr/bin/env bash
# Runs the Snowflake destination's tests (internal/snowflake) against the
# Snowflake test account, taken from the environment or else from .env
# (README.md, Snowflake). They use its credits, so `just test` runs them only
# where the environment has the account, as CI's does for the changes that
# need them. SNOWFLAKE_SPIKE=1 adds the spike's measurements.
# Usage: hack/snowflake-test.sh [go test flags]
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

for variable in SNOWFLAKE_ACCOUNT SNOWFLAKE_TEST_USER SNOWFLAKE_TEST_PRIVATE_KEY; do
  [[ -n ${!variable:-} ]] || export "$variable=$(dotenv_value "$variable")"
  [[ -n ${!variable} ]] || die "no $variable in the environment or .env (README.md, Snowflake)"
done
go test -race -count=1 "$@" ./internal/snowflake
