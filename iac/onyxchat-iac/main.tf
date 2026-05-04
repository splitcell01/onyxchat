data "aws_vpc" "default" {
  default = true
}

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }
}

# Construct the DSN from the RDS-managed Secrets Manager secret so this
# parameter stays in sync automatically on every terraform apply.
resource "aws_ssm_parameter" "db_dsn" {
  name  = var.db_dsn_ssm_param
  type  = "SecureString"
  value = "postgres://${local.rds_creds.username}:${urlencode(local.rds_creds.password)}@${aws_db_instance.postgres.address}:${aws_db_instance.postgres.port}/${var.db_name}"
}

data "aws_ssm_parameter" "db_host" {
  name = "/onyxchat/prod/SM_DB_HOST"
}

data "aws_ssm_parameter" "db_port" {
  name = "/onyxchat/prod/SM_DB_PORT"
}

data "aws_ssm_parameter" "jwt_secret" {
  name = "/onyxchat/prod/JWT_SECRET"
}

data "aws_ssm_parameter" "redis_auth_token" {
  name = "/onyxchat/prod/SM_REDIS_AUTH_TOKEN"
}

# ── IAM ────────────────────────────────────────────────────────────────────────

data "aws_iam_policy_document" "ecs_task_assume_role" {
  statement {
    effect = "Allow"
    principals {
      type        = "Service"
      identifiers = ["ecs-tasks.amazonaws.com"]
    }
    actions = ["sts:AssumeRole"]
  }
}

# Task execution role: used by ECS agent to pull image + inject secrets at startup
resource "aws_iam_role" "task_execution_role" {
  name               = "${var.app_name}-task-exec"
  assume_role_policy = data.aws_iam_policy_document.ecs_task_assume_role.json
}

resource "aws_iam_role_policy_attachment" "task_exec_attach" {
  role       = aws_iam_role.task_execution_role.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

# SSM read policy — attached only to execution role so the ECS agent can inject
# secrets at container startup. The running app (task_role) does NOT get this.
resource "aws_iam_policy" "task_ssm_read" {
  name = "${var.app_name}-task-ssm-read"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = ["ssm:GetParameter", "ssm:GetParameters"]
        Resource = [
          aws_ssm_parameter.db_dsn.arn,
          data.aws_ssm_parameter.jwt_secret.arn,
          data.aws_ssm_parameter.redis_auth_token.arn,
        ]
      }
    ]
  })
}

resource "aws_iam_role_policy_attachment" "task_exec_ssm_attach" {
  role       = aws_iam_role.task_execution_role.name
  policy_arn = aws_iam_policy.task_ssm_read.arn
}

# Task role: assumed by the running container at runtime.
# Only grant what the application actually needs — no SSM access by default.
resource "aws_iam_role" "task_role" {
  name               = "${var.app_name}-task-role"
  assume_role_policy = data.aws_iam_policy_document.ecs_task_assume_role.json
}

# SSM permissions required for ECS Exec (allows `aws ecs execute-command`)
resource "aws_iam_role_policy" "task_exec_ssm" {
  name = "${var.app_name}-task-exec-ssm"
  role = aws_iam_role.task_role.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "ssmmessages:CreateControlChannel",
          "ssmmessages:CreateDataChannel",
          "ssmmessages:OpenControlChannel",
          "ssmmessages:OpenDataChannel"
        ]
        Resource = "*"
      }
    ]
  })
}

# ── Security Groups ────────────────────────────────────────────────────────────

resource "aws_security_group" "alb_sg" {
  name        = "${var.app_name}-alb-sg"
  description = "ALB SG"
  vpc_id      = data.aws_vpc.default.id

  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_security_group" "ecs_sg" {
  name        = "${var.app_name}-ecs-sg"
  description = "ECS tasks SG"
  vpc_id      = data.aws_vpc.default.id

  ingress {
    from_port       = var.app_port
    to_port         = var.app_port
    protocol        = "tcp"
    security_groups = [aws_security_group.alb_sg.id]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_security_group" "redis_sg" {
  name        = "${var.app_name}-redis-sg"
  description = "ElastiCache Redis SG - only ECS tasks may connect"
  vpc_id      = data.aws_vpc.default.id

  ingress {
    from_port       = 6379
    to_port         = 6379
    protocol        = "tcp"
    security_groups = [aws_security_group.ecs_sg.id]
    description     = "Allow ECS tasks to reach Redis"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

# Allow ECS -> RDS on 5432
resource "aws_security_group_rule" "rds_ingress_from_ecs" {
  type                     = "ingress"
  security_group_id        = var.existing_rds_sg_id
  from_port                = var.db_port
  to_port                  = var.db_port
  protocol                 = "tcp"
  source_security_group_id = aws_security_group.ecs_sg.id
  description              = "Allow ECS tasks to reach RDS"
}

# ── ElastiCache Redis ──────────────────────────────────────────────────────────

resource "aws_elasticache_subnet_group" "redis" {
  name       = "${var.app_name}-redis-subnet-group"
  subnet_ids = local.subnets
}

# Using replication_group instead of cluster to support encryption-in-transit
resource "aws_elasticache_replication_group" "redis" {
  replication_group_id = "${var.app_name}-redis"
  description          = "OnyxChat Redis"

  node_type            = "cache.t4g.micro"
  num_cache_clusters   = 1
  parameter_group_name = "default.redis7"
  engine_version       = "7.1"
  port                 = 6379

  subnet_group_name  = aws_elasticache_subnet_group.redis.name
  security_group_ids = [aws_security_group.redis_sg.id]

  # Encryption at rest and in transit
  at_rest_encryption_enabled = true
  transit_encryption_enabled = true
  auth_token                 = var.redis_auth_token

  # Disable automatic minor version upgrades in prod to avoid surprises
  auto_minor_version_upgrade = false

  tags = {
    Env     = "prod"
    Project = "OnyxChat"
  }
}

# ── Load Balancer ──────────────────────────────────────────────────────────────

resource "aws_lb" "alb" {
  name               = "onyxchat-alb"
  load_balancer_type = "application"
  internal           = false
  security_groups    = [aws_security_group.alb_sg.id]
  subnets            = local.subnets

  # Protect against accidental deletion
  enable_deletion_protection = true
}

resource "aws_lb_target_group" "tg" {
  name        = "${var.app_name}-tg"
  port        = var.app_port
  protocol    = "HTTP"
  vpc_id      = data.aws_vpc.default.id
  target_type = "ip"

  health_check {
    path                = "/health/ready"
    protocol            = "HTTP"
    matcher             = "200-399"
    interval            = 30
    timeout             = 5
    healthy_threshold   = 2
    unhealthy_threshold = 2
  }
}

resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.alb.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type = "redirect"
    redirect {
      port        = "443"
      protocol    = "HTTPS"
      status_code = "HTTP_301"
    }
  }
}

resource "aws_lb_listener" "https" {
  load_balancer_arn = aws_lb.alb.arn
  port              = 443
  protocol          = "HTTPS"
  ssl_policy        = "ELBSecurityPolicy-TLS13-1-2-Res-PQ-2025-09"
  certificate_arn   = "arn:aws:acm:us-east-1:${var.aws_account_id}:certificate/e0d0104e-c869-4472-9275-514e31801844"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.tg.arn
  }
}

# ── ECS ────────────────────────────────────────────────────────────────────────

resource "aws_ecs_cluster" "this" {
  name = "${var.app_name}-cluster"

  setting {
    name  = "containerInsights"
    value = "disabled"
  }
}

resource "aws_cloudwatch_log_group" "app" {
  name              = "/ecs/${var.app_name}"
  retention_in_days = 14
}

resource "aws_ecs_task_definition" "app" {
  family                   = "${var.app_name}-task"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = "256"
  memory                   = "512"

  execution_role_arn = aws_iam_role.task_execution_role.arn
  task_role_arn      = aws_iam_role.task_role.arn

  container_definitions = jsonencode([
    {
      name      = var.app_name
      image     = var.image_uri
      essential = true

      portMappings = [
        { containerPort = var.app_port, hostPort = var.app_port, protocol = "tcp" }
      ]

      environment = local.app_env

      secrets = [
        { name = "SM_DB_DSN", valueFrom = aws_ssm_parameter.db_dsn.arn },
        { name = "JWT_SECRET", valueFrom = data.aws_ssm_parameter.jwt_secret.arn },
        { name = "SM_REDIS_AUTH_TOKEN", valueFrom = data.aws_ssm_parameter.redis_auth_token.arn },
      ]

      logConfiguration = {
        logDriver = "awslogs"
        options = {
          awslogs-group         = aws_cloudwatch_log_group.app.name
          awslogs-region        = "us-east-1"
          awslogs-stream-prefix = "ecs"
        }
      }
    }
  ])
}

resource "aws_ecs_service" "app" {
  name            = "${var.app_name}-svc"
  cluster         = aws_ecs_cluster.this.id
  task_definition = aws_ecs_task_definition.app.arn
  desired_count   = 2 # minimum 2 for prod redundancy
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = local.subnets
    security_groups  = [aws_security_group.ecs_sg.id]
    assign_public_ip = true
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.tg.arn
    container_name   = var.app_name
    container_port   = var.app_port
  }

  enable_execute_command = true

  # Roll back automatically if a new deployment fails health checks
  deployment_circuit_breaker {
    enable   = true
    rollback = true
  }

  deployment_controller {
    type = "ECS"
  }

  depends_on = [aws_lb_listener.https]
}

# ── ECS Autoscaling ────────────────────────────────────────────────────────────

resource "aws_appautoscaling_target" "ecs" {
  max_capacity       = 6
  min_capacity       = 2
  resource_id        = "service/${aws_ecs_cluster.this.name}/${aws_ecs_service.app.name}"
  scalable_dimension = "ecs:service:DesiredCount"
  service_namespace  = "ecs"

  depends_on = [aws_ecs_service.app]
}

resource "aws_appautoscaling_policy" "cpu" {
  name               = "${var.app_name}-cpu-scaling"
  policy_type        = "TargetTrackingScaling"
  resource_id        = aws_appautoscaling_target.ecs.resource_id
  scalable_dimension = aws_appautoscaling_target.ecs.scalable_dimension
  service_namespace  = aws_appautoscaling_target.ecs.service_namespace

  target_tracking_scaling_policy_configuration {
    predefined_metric_specification {
      predefined_metric_type = "ECSServiceAverageCPUUtilization"
    }
    target_value       = 70.0
    scale_in_cooldown  = 300
    scale_out_cooldown = 60
  }
}

resource "aws_appautoscaling_policy" "memory" {
  name               = "${var.app_name}-memory-scaling"
  policy_type        = "TargetTrackingScaling"
  resource_id        = aws_appautoscaling_target.ecs.resource_id
  scalable_dimension = aws_appautoscaling_target.ecs.scalable_dimension
  service_namespace  = aws_appautoscaling_target.ecs.service_namespace

  target_tracking_scaling_policy_configuration {
    predefined_metric_specification {
      predefined_metric_type = "ECSServiceAverageMemoryUtilization"
    }
    target_value       = 70.0
    scale_in_cooldown  = 300
    scale_out_cooldown = 60
  }
}

# ── Locals ─────────────────────────────────────────────────────────────────────

locals {
  subnets   = data.aws_subnets.default.ids
  rds_creds = jsondecode(data.aws_secretsmanager_secret_version.rds_master.secret_string)

  app_env = [
    { name = "SM_ENV", value = "prod" },
    { name = "SM_SERVER_ADDR", value = ":${var.app_port}" },
    { name = "SM_REDIS_ADDR", value = "${aws_elasticache_replication_group.redis.primary_endpoint_address}:6379" },
    { name = "SM_ALLOWED_ORIGINS", value = "https://onyxchat.dev,https://www.onyxchat.dev" },
  ]
}

output "effective_image_uri" {
  value = var.image_uri
}
