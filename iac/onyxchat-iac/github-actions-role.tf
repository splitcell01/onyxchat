data "aws_caller_identity" "current" {}

data "aws_iam_openid_connect_provider" "github" {
  url = "https://token.actions.githubusercontent.com"
}

resource "aws_iam_role" "github_deploy" {
  name = "onyxchat-github-deploy"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Principal = {
        Federated = data.aws_iam_openid_connect_provider.github.arn
      }
      Action = "sts:AssumeRoleWithWebIdentity"
      Condition = {
        StringEquals = {
          "token.actions.githubusercontent.com:aud" = "sts.amazonaws.com"
        }
        StringLike = {
          "token.actions.githubusercontent.com:sub" = [
            "repo:colecschmidt/onyxchat:ref:refs/heads/main",
            "repo:colecschmidt/onyxchat-iac:ref:refs/heads/main"
          ]
        }
      }
    }]
  })
}

resource "aws_iam_role_policy" "github_deploy" {
  name = "onyxchat-github-deploy-policy"
  role = aws_iam_role.github_deploy.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid      = "ECRAuth"
        Effect   = "Allow"
        Action   = "ecr:GetAuthorizationToken"
        Resource = "*"
      },
      {
        Sid    = "ECRPush"
        Effect = "Allow"
        Action = [
          "ecr:DescribeRepositories",
          "ecr:BatchCheckLayerAvailability",
          "ecr:InitiateLayerUpload",
          "ecr:UploadLayerPart",
          "ecr:CompleteLayerUpload",
          "ecr:PutImage",
          "ecr:BatchGetImage"
        ]
        Resource = "arn:aws:ecr:us-east-1:${data.aws_caller_identity.current.account_id}:repository/onyxchat"
      },
      {
        Sid    = "ECSDeployment"
        Effect = "Allow"
        Action = [
          "ecs:UpdateService",
          "ecs:UpdateCluster",
          "ecs:DescribeServices",
          "ecs:DescribeClusters",
          "ecs:RegisterTaskDefinition",
          "ecs:DeregisterTaskDefinition",
          "ecs:DescribeTaskDefinition",
          "ecs:TagResource"
        ]
        Resource = "*"
      },
      {
        Sid    = "TerraformState"
        Effect = "Allow"
        Action = [
          "s3:GetObject",
          "s3:PutObject",
          "s3:DeleteObject",
          "s3:ListBucket"
        ]
        Resource = [
          "arn:aws:s3:::onyxchat-tfstate-cole-2025",
          "arn:aws:s3:::onyxchat-tfstate-cole-2025/*"
        ]
      },
      {
        Sid    = "TerraformStateLock"
        Effect = "Allow"
        Action = [
          "dynamodb:GetItem",
          "dynamodb:PutItem",
          "dynamodb:DeleteItem"
        ]
        Resource = "arn:aws:dynamodb:us-east-1:${data.aws_caller_identity.current.account_id}:table/onyxchat-tflock"
      },
      {
        Sid    = "TerraformRead"
        Effect = "Allow"
        Action = [
          "ec2:DescribeVpcs",
          "ec2:DescribeSubnets",
          "ec2:DescribeSecurityGroups",
          "ec2:DescribeSecurityGroupRules",
          "ec2:DescribeVpcAttribute",
          "iam:GetRole",
          "iam:GetPolicy",
          "iam:GetPolicyVersion",
          "iam:ListAttachedRolePolicies",
          "iam:ListRolePolicies",
          "iam:GetRolePolicy",
          "iam:ListPolicyVersions",
          "ssm:GetParameter",
          "ssm:GetParameters",
          "ssm:DescribeParameters",
          "rds:DescribeDBInstances",
          "rds:DescribeDBSubnetGroups",
          "elasticache:DescribeReplicationGroups",
          "elasticache:DescribeCacheSubnetGroups",
          "elasticache:DescribeCacheParameters",
          "elasticache:ListTagsForResource",
          "elasticloadbalancing:DescribeLoadBalancers",
          "elasticloadbalancing:DescribeTargetGroups",
          "elasticloadbalancing:DescribeListeners",
          "elasticloadbalancing:DescribeListenerAttributes",
          "elasticloadbalancing:DescribeTargetGroupAttributes",
          "elasticloadbalancing:DescribeLoadBalancerAttributes",
          "elasticloadbalancing:DescribeTags",
          "elasticloadbalancing:DescribeListenerCertificates",
          "elasticloadbalancing:DescribeRules",
          "logs:DescribeLogGroups",
          "logs:ListTagsForResource",
          "budgets:ViewBudget",
          "ce:GetAnomalyMonitors",
          "ce:GetAnomalySubscriptions",
          "acm:DescribeCertificate",
          "acm:ListCertificates"
        ]
        Resource = "*"
      },
      {
        Sid    = "TerraformWrite"
        Effect = "Allow"
        Action = [
          "iam:TagRole",
          "iam:GetOpenIDConnectProvider",
          "iam:ListOpenIDConnectProviders",
          "ec2:CreateSecurityGroup",
          "ec2:DeleteSecurityGroup",
          "ec2:AuthorizeSecurityGroupIngress",
          "ec2:RevokeSecurityGroupIngress",
          "ec2:AuthorizeSecurityGroupEgress",
          "ec2:RevokeSecurityGroupEgress",
          "ec2:CreateTags",
          "ec2:DeleteTags",
          "elasticloadbalancing:ModifyLoadBalancerAttributes",
          "elasticloadbalancing:ModifyTargetGroupAttributes",
          "elasticloadbalancing:CreateListener",
          "elasticloadbalancing:DeleteListener",
          "elasticloadbalancing:ModifyListener",
          "elasticloadbalancing:CreateRule",
          "elasticloadbalancing:DeleteRule",
          "elasticloadbalancing:ModifyRule",
          "elasticloadbalancing:AddTags",
          "elasticache:ModifyReplicationGroup",
          "elasticache:AddTagsToResource",
          "logs:CreateLogGroup",
          "logs:PutRetentionPolicy",
          "logs:TagResource",
          "logs:ListTagsLogGroup",
          "rds:ModifyDBInstance",
          "rds:AddTagsToResource",
        ]
        Resource = "*"
      },
      {
        Sid    = "PassRoleToECS"
        Effect = "Allow"
        Action = "iam:PassRole"
        Resource = [
          "arn:aws:iam::${data.aws_caller_identity.current.account_id}:role/onyxchat-task-exec",
          "arn:aws:iam::${data.aws_caller_identity.current.account_id}:role/onyxchat-task-role"
        ]
        Condition = {
          StringLike = {
            "iam:PassedToService" = "ecs-tasks.amazonaws.com"
          }
        }
      }
    ]
  })
}