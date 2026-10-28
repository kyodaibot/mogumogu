#!/usr/bin/env bash
# Creates the production secrets in SSM Parameter Store as SecureString
# (encrypted with the account's default aws/ssm key). Run once:
#
#   AWS_PROFILE=Teiyou scripts/put-secrets.sh
#
# A parameter that already exists is left alone, so running it again is safe.
# To change a secret, see "更换密钥" in docs/operations.md. Values are never
# printed or passed on the command line; each lives briefly in a temp file
# only you can read, removed on exit.
set -euo pipefail

: "${AWS_PROFILE:?set AWS_PROFILE, e.g. AWS_PROFILE=Teiyou}"
export AWS_REGION=ap-northeast-1

umask 077
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT

put() {
  local name=$1 value=$2
  if aws ssm get-parameter --name "$name" --query Parameter.Name --output text >/dev/null 2>&1; then
    echo "exists, unchanged: $name"
    return
  fi
  # The value goes in through a private temp file, so it never shows up in
  # `ps`. (The CLI cannot read --cli-input-json from a pipe.)
  printf '{"Name":"%s","Type":"SecureString","Value":"%s","Tags":[{"Key":"Project","Value":"mogumogu"}]}' \
    "$name" "$value" >"$tmp"
  aws ssm put-parameter --cli-input-json "file://$tmp" >/dev/null
  echo "created: $name"
}

# Same strength as .env.example asks for. base64 has no spaces, quotes, $ or #,
# so the values also work in the server's .env file.
put /mogumogu/jwt_secret "$(openssl rand -base64 48)"
put /mogumogu/csrf_secret "$(openssl rand -base64 48)"
# Hex only: RDS rejects / @ " and spaces in the master password.
put /mogumogu/db_password "$(openssl rand -hex 32)"
