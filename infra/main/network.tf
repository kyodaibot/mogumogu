# A small VPC without a NAT gateway (about 45 USD a month):
# - one public subnet for the EC2 instance, which reaches ECR, Bedrock and SSM
#   through the internet gateway with its own public address;
# - two private subnets in different zones for RDS (a DB subnet group needs
#   two zones). They have no route to the internet, and RDS needs none.

data "aws_availability_zones" "available" {
  state = "available"
}

locals {
  # The first two zones, e.g. ap-northeast-1a and ap-northeast-1c.
  zones = slice(data.aws_availability_zones.available.names, 0, 2)
}

resource "aws_vpc" "main" {
  cidr_block = "10.0.0.0/16"
  # Needed for the RDS endpoint's DNS name to resolve inside the VPC.
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "mogumogu" }
}

resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id
  tags   = { Name = "mogumogu" }
}

# --- Public: the EC2 instance ---

resource "aws_subnet" "public" {
  vpc_id            = aws_vpc.main.id
  cidr_block        = "10.0.0.0/24"
  availability_zone = local.zones[0]
  # The instance gets a fixed Elastic IP instead (step 7).
  map_public_ip_on_launch = false

  tags = { Name = "mogumogu-public" }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.main.id
  }

  tags = { Name = "mogumogu-public" }
}

resource "aws_route_table_association" "public" {
  subnet_id      = aws_subnet.public.id
  route_table_id = aws_route_table.public.id
}

# --- Private: RDS ---

resource "aws_subnet" "private" {
  count             = 2
  vpc_id            = aws_vpc.main.id
  cidr_block        = "10.0.${10 + count.index}.0/24"
  availability_zone = local.zones[count.index]

  tags = { Name = "mogumogu-private-${count.index}" }
}

# Only the implicit local route: no way in or out of the VPC.
resource "aws_route_table" "private" {
  vpc_id = aws_vpc.main.id
  tags   = { Name = "mogumogu-private" }
}

resource "aws_route_table_association" "private" {
  count          = 2
  subnet_id      = aws_subnet.private[count.index].id
  route_table_id = aws_route_table.private.id
}

# --- Security groups ---

# Every VPC comes with a default group that allows traffic between its
# members. Managing it here with no rules leaves it unusable by accident.
resource "aws_default_security_group" "default" {
  vpc_id = aws_vpc.main.id
}

# The EC2 instance: HTTP and HTTPS from anywhere, nothing else. No SSH; log in
# with SSM Session Manager. HTTP stays open for certificate issuance and the
# redirect to HTTPS.
resource "aws_security_group" "app" {
  name        = "mogumogu-app"
  description = "EC2 instance: public HTTP and HTTPS"
  vpc_id      = aws_vpc.main.id
}

resource "aws_vpc_security_group_ingress_rule" "app_http" {
  security_group_id = aws_security_group.app.id
  description       = "HTTP, redirected to HTTPS by Caddy"
  ip_protocol       = "tcp"
  from_port         = 80
  to_port           = 80
  cidr_ipv4         = "0.0.0.0/0"
}

resource "aws_vpc_security_group_ingress_rule" "app_https" {
  security_group_id = aws_security_group.app.id
  description       = "HTTPS"
  ip_protocol       = "tcp"
  from_port         = 443
  to_port           = 443
  cidr_ipv4         = "0.0.0.0/0"
}

# Outbound: ECR, Bedrock, SSM, Let's Encrypt and RDS.
resource "aws_vpc_security_group_egress_rule" "app_all" {
  security_group_id = aws_security_group.app.id
  description       = "All outbound"
  ip_protocol       = "-1"
  cidr_ipv4         = "0.0.0.0/0"
}

# RDS: PostgreSQL from the EC2 instance only. No outbound rules.
resource "aws_security_group" "db" {
  name        = "mogumogu-db"
  description = "RDS: PostgreSQL from the app instance only"
  vpc_id      = aws_vpc.main.id
}

resource "aws_vpc_security_group_ingress_rule" "db_from_app" {
  security_group_id            = aws_security_group.db.id
  description                  = "PostgreSQL from the app instance"
  ip_protocol                  = "tcp"
  from_port                    = 5432
  to_port                      = 5432
  referenced_security_group_id = aws_security_group.app.id
}
