output "alb_dns_name" {
  value = aws_lb.alb.dns_name
}

output "alb_arn" {
  value = aws_lb.alb.arn
}

output "http_listener_arn" {
  value = aws_lb_listener.http.arn
}

output "https_listener_arn" {
  value = aws_lb_listener.https.arn
}

output "target_group_arn" {
  value = aws_lb_target_group.tg.arn
}

output "ecs_cluster_name" {
  value = aws_ecs_cluster.this.name
}

output "ecs_service_name" {
  value = aws_ecs_service.app.name
}

output "task_definition_arn" {
  value = aws_ecs_task_definition.app.arn
}

output "log_group_name" {
  value = aws_cloudwatch_log_group.app.name
}

output "task_execution_role_arn" {
  value = aws_iam_role.task_execution_role.arn
}

output "task_role_arn" {
  value = aws_iam_role.task_role.arn
}

output "ssm_db_dsn_param_name" {
  value = aws_ssm_parameter.db_dsn.name
}

output "redis_endpoint" {
  value       = "${aws_elasticache_replication_group.redis.primary_endpoint_address}:6379"
  description = "ElastiCache Redis endpoint passed to ECS as SM_REDIS_ADDR"
}
