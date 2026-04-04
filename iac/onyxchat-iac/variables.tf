variable "app_name" {
  type    = string
  default = "onyxchat"
}

variable "aws_account_id" {
  type        = string
  description = "AWS account ID. Used for constructing ARNs."
}

variable "image_uri" {
  type = string
  validation {
    condition     = length(trimspace(var.image_uri)) > 0
    error_message = "image_uri must be a non-empty string."
  }
}

variable "db_port" {
  type    = number
  default = 5432
}

variable "db_name" {
  type    = string
  default = "postgres"
}

variable "db_user" {
  type    = string
  default = "postgres"
}

variable "existing_rds_sg_id" {
  type        = string
  description = "Security group ID of the existing RDS instance."
}

variable "app_port" {
  type    = number
  default = 8080
}

variable "jwt_secret_ssm_param" {
  type    = string
  default = "/onyxchat/prod/JWT_SECRET"
}

variable "db_dsn_ssm_param" {
  type    = string
  default = "/onyxchat/prod/SM_DB_DSN"
}

variable "alert_email" {
  type        = string
  description = "Email address for CloudWatch alarm notifications."
}

variable "redis_auth_token" {
  type        = string
  sensitive   = true
  description = "Auth token for ElastiCache Redis transit encryption. Store in SSM and pass via -var or TF_VAR_redis_auth_token."
  validation {
    condition     = length(var.redis_auth_token) >= 16
    error_message = "redis_auth_token must be at least 16 characters."
  }
}