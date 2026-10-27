#!/bin/bash
# Runs on the EC2 instance as root, started by `make deploy` through SSM Run
# Command after the files in deploy/ were copied to /opt/mogumogu.
#
#   deploy.sh <commit tag>
#
# Writes .env from SSM Parameter Store, pulls the images for the tag from ECR
# and starts them. Deploying an older tag is a rollback. The database is only
# migrated forward; see docs/operations.md before rolling back across a
# migration.
set -euo pipefail

tag=${1:?usage: deploy.sh <commit tag>}
[[ $tag =~ ^[0-9a-f]{7,40}$ ]] || { echo "not a commit tag: $tag"; exit 1; }

export AWS_REGION=ap-northeast-1
cd /opt/mogumogu

param() {
  aws ssm get-parameter --name "/mogumogu/$1" --with-decryption --query Parameter.Value --output text
}

account=$(aws sts get-caller-identity --query Account --output text)
registry=$account.dkr.ecr.$AWS_REGION.amazonaws.com

# Readable by root only. Written to a temp file and moved, so a failed read
# never leaves a half-written .env behind.
umask 077
cat >.env.new <<ENV
TAG=$tag
REGISTRY=$registry
DOMAIN=$(param domain)
BEDROCK_MODEL_ID=$(param bedrock_model_id)
DB_HOST=$(param db_host)
DB_PASSWORD=$(param db_password)
JWT_SECRET=$(param jwt_secret)
CSRF_SECRET=$(param csrf_secret)
ENV
mv .env.new .env

echo "Pulling $tag"
aws ecr get-login-password | docker login --username AWS --password-stdin "$registry" >/dev/null
docker compose pull --quiet
docker logout "$registry" >/dev/null

# Migrations run first (migrate must exit 0), then the services start in
# order. --wait returns once all are healthy, and fails if one is not.
echo "Starting $tag"
docker compose up -d --remove-orphans --wait --wait-timeout 180

# Images no container uses any more; a rollback pulls them again.
docker image prune -af >/dev/null
docker compose ps --format 'table {{.Service}}\t{{.Image}}\t{{.Status}}'
echo "Deployed $tag"
