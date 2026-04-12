resource "aws_db_instance" "postgres" {
  identifier        = "database-1"
  instance_class    = "db.t4g.micro"
  engine            = "postgres"
  engine_version    = "17.6"
  username          = "postgres"
  allocated_storage = 20
  storage_type      = "gp3"
  storage_encrypted = true

  manage_master_user_password = true

  # No DB name was set at creation time
  db_name = null

  multi_az            = false
  publicly_accessible = false

  # Protect prod data — disable these if you intentionally need to destroy
  deletion_protection       = true
  skip_final_snapshot       = false
  final_snapshot_identifier = "${var.app_name}-final-snapshot"

  # Match existing settings to avoid drift
  backup_retention_period               = 7
  copy_tags_to_snapshot                 = true
  monitoring_interval                   = 60
  performance_insights_enabled          = true
  performance_insights_retention_period = 7
  max_allocated_storage                 = 1000

  # Reference managed resources instead of hardcoded IDs
  db_subnet_group_name   = "default-vpc-0d316d9a4f491cf84"
  vpc_security_group_ids = [var.existing_rds_sg_id]

  tags = {
    Env     = "prod"
    Project = "OnyxChat"
  }

  lifecycle {
    ignore_changes = [
      password,
      # AWS may update these automatically
      engine_version,
      ca_cert_identifier,
    ]
  }
}

# Read the RDS-managed password from Secrets Manager so Terraform can
# keep the SSM DSN parameter in sync automatically.
data "aws_secretsmanager_secret_version" "rds_master" {
  secret_id = aws_db_instance.postgres.master_user_secret[0].secret_arn
}
