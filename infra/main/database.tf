# PostgreSQL on RDS, in the private subnets, reachable only from the app
# security group (network.tf).

# The master password created by scripts/put-secrets.sh. "ephemeral" means
# Terraform reads it during plan/apply but never stores it in the state.
data "aws_caller_identity" "current" {}

ephemeral "aws_ssm_parameter" "db_password" {
  arn             = "arn:aws:ssm:${var.region}:${data.aws_caller_identity.current.account_id}:parameter/mogumogu/db_password"
  with_decryption = true
}

resource "aws_db_subnet_group" "main" {
  name       = "mogumogu"
  subnet_ids = aws_subnet.private[*].id
}

resource "aws_db_instance" "main" {
  identifier = "mogumogu"

  engine = "postgres"
  # Major version only, matching local postgres:17.x. Minor upgrades are
  # applied by AWS in the maintenance window, without drifting from this code.
  engine_version             = "17"
  auto_minor_version_upgrade = true
  instance_class             = var.db_instance_class

  allocated_storage = 20 # GB, the gp3 minimum
  storage_type      = "gp3"
  storage_encrypted = true

  db_name  = "mogumogu"
  username = "mogumogu"
  # Write-only: sent to AWS on apply, never saved in the state. To change the
  # password, update the SSM parameter and bump the version.
  password_wo         = ephemeral.aws_ssm_parameter.db_password.value
  password_wo_version = 1

  db_subnet_group_name   = aws_db_subnet_group.main.name
  vpc_security_group_ids = [aws_security_group.db.id]
  publicly_accessible    = false
  multi_az               = false

  # Daily automatic backups, kept 7 days. Times are UTC: 03:00-04:00 and
  # Monday 04:00-05:00 in Tokyo, when nobody is using the demo.
  backup_retention_period = 7
  backup_window           = "18:00-19:00"
  maintenance_window      = "sun:19:00-sun:20:00"
  copy_tags_to_snapshot   = true

  # A deletion has to be a deliberate two-step change, and still leaves a
  # final snapshot behind.
  deletion_protection       = true
  skip_final_snapshot       = false
  final_snapshot_identifier = "mogumogu-final"
}

# The database host name, for deploy.sh to write into the server's .env.
# Not a secret, so a plain String.
resource "aws_ssm_parameter" "db_host" {
  name  = "/mogumogu/db_host"
  type  = "String"
  value = aws_db_instance.main.address
}
