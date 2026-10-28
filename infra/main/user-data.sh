#!/bin/bash
# Runs once, as root, on the instance's first boot. Prepares the host for
# Docker Compose; the app itself is installed by deploy.sh (milestone 2).
# Output: /var/log/cloud-init-output.log on the instance.
set -euxo pipefail

# Docker from the Amazon Linux repository.
dnf install -y docker
# Rotate container logs so they cannot fill the disk.
mkdir -p /etc/docker
cat >/etc/docker/daemon.json <<'JSON'
{ "log-driver": "json-file", "log-opts": { "max-size": "10m", "max-file": "3" } }
JSON
systemctl enable --now docker

# The Compose plugin is not packaged for Amazon Linux; install a pinned
# release and check its checksum.
COMPOSE_VERSION=v5.6.0
COMPOSE_SHA256=733ec76717ceb59052a9609b9dadfb523b2df8eab57a54212872d10a58078ea2
mkdir -p /usr/local/lib/docker/cli-plugins
curl -fsSL -o /usr/local/lib/docker/cli-plugins/docker-compose \
  "https://github.com/docker/compose/releases/download/${COMPOSE_VERSION}/docker-compose-linux-aarch64"
echo "${COMPOSE_SHA256}  /usr/local/lib/docker/cli-plugins/docker-compose" | sha256sum -c -
chmod +x /usr/local/lib/docker/cli-plugins/docker-compose

# 1 GB swap: headroom for the 1 GB instance while images are pulled and
# containers start.
dd if=/dev/zero of=/swapfile bs=1M count=1024
chmod 600 /swapfile
mkswap /swapfile
swapon /swapfile
echo '/swapfile none swap sw 0 0' >>/etc/fstab

# Where deploy.sh keeps the compose file and the generated .env.
install -d -m 700 /opt/mogumogu
