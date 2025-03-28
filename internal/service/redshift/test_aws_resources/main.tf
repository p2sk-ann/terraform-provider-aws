variable "aws_region" {
  description = "AWS region to deploy resources"
  type        = string
  default     = "us-west-2"
}

provider "aws" {
  region = "us-west-2"
}

data "aws_caller_identity" "current" {}

resource "aws_vpc" "main" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_hostnames = true
  enable_dns_support   = true
  tags                 = { Name = "sample-vpc" }
}

data "aws_availability_zones" "available" {
  state = "available"
}

resource "aws_subnet" "private_a" {
  vpc_id                  = aws_vpc.main.id
  cidr_block              = "10.0.1.0/24"
  availability_zone       = data.aws_availability_zones.available.names[0]
  map_public_ip_on_launch = false
  tags                    = { Name = "sample-private-subnet-a" }
}
resource "aws_subnet" "private_b" {
  vpc_id                  = aws_vpc.main.id
  cidr_block              = "10.0.2.0/24"
  availability_zone       = data.aws_availability_zones.available.names[1]
  map_public_ip_on_launch = false
  tags                    = { Name = "sample-private-subnet-b" }
}
resource "aws_subnet" "private_c" {
  vpc_id                  = aws_vpc.main.id
  cidr_block              = "10.0.3.0/24"
  availability_zone       = data.aws_availability_zones.available.names[2]
  map_public_ip_on_launch = false
  tags                    = { Name = "sample-private-subnet-c" }
}

resource "aws_vpc_endpoint" "dynamodb" {
  vpc_id            = aws_vpc.main.id
  service_name      = "com.amazonaws.${var.aws_region != null ? var.aws_region : "us-west-2"}.dynamodb"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = [aws_vpc.main.default_route_table_id]
  tags              = { Name = "dynamodb-endpoint" }
}

resource "aws_vpc_endpoint" "s3" {
  vpc_id            = aws_vpc.main.id
  service_name      = "com.amazonaws.${var.aws_region}.s3"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = [aws_vpc.main.default_route_table_id]
  tags              = { Name = "s3-endpoint" }
}

resource "aws_vpc_endpoint" "kms" {
  vpc_id              = aws_vpc.main.id
  service_name        = "com.amazonaws.${var.aws_region}.kms"
  vpc_endpoint_type   = "Interface"
  subnet_ids          = [aws_subnet.private_a.id, aws_subnet.private_b.id]
  private_dns_enabled = true
  security_group_ids  = [aws_security_group.redshift.id]
  tags                = { Name = "kms-endpoint" }
}

resource "aws_security_group" "redshift" {
  name        = "redshift-sg"
  description = "Allow Redshift cluster internal communication only"
  vpc_id      = aws_vpc.main.id
  tags        = { Name = "sample-redshift-sg" }

  ingress {
    description = "Redshift internal"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    self        = true
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_dynamodb_table" "sample" {
  name         = "sample_table"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "ID"

  attribute {
    name = "ID"
    type = "S"
  }

  point_in_time_recovery {
    enabled = true
  }

  tags = { Name = "sample_table" }
}

# !! おそらく serverless側と共通
resource "aws_dynamodb_resource_policy" "dynamodb_policy" {
  resource_arn = aws_dynamodb_table.sample.arn
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Principal = {
          Service = "redshift.amazonaws.com"
        }
        Action = [
          "dynamodb:ExportTableToPointInTime",
          "dynamodb:DescribeTable"
        ]
        Resource = "arn:aws:dynamodb:${var.aws_region}:${data.aws_caller_identity.current.account_id}:table/${aws_dynamodb_table.sample.name}"
        Condition = {
          StringEquals = {
            "aws:SourceAccount" = data.aws_caller_identity.current.account_id
          }
          ArnEquals = {
            "aws:SourceArn" = "arn:aws:redshift:${var.aws_region}:${data.aws_caller_identity.current.account_id}:integration:*"
          }
        }
      },
      {
        Effect = "Allow"
        Principal = {
          Service = "redshift.amazonaws.com"
        }
        Action   = "dynamodb:DescribeExport"
        Resource = "arn:aws:dynamodb:${var.aws_region}:${data.aws_caller_identity.current.account_id}:table/${aws_dynamodb_table.sample.name}/export/*"
        Condition = {
          StringEquals = {
            "aws:SourceAccount" = data.aws_caller_identity.current.account_id
          }
          ArnEquals = {
            "aws:SourceArn" = "arn:aws:redshift:${var.aws_region}:${data.aws_caller_identity.current.account_id}:integration:*"
          }
        }
      }
    ]
  })
}

resource "null_resource" "dynamodb_seed" {
  provisioner "local-exec" {
    command = <<EOT
        aws dynamodb put-item \
          --table-name sample_table \
          --item '{"ID": {"S": "001"}, "name": {"S": "Alice"}, "age": {"N": "30"}}' --region us-west-2
  
        aws dynamodb put-item \
          --table-name sample_table \
          --item '{"ID": {"S": "002"}, "name": {"S": "Bob"}, "age": {"N": "25"}}' --region us-west-2
  
        aws dynamodb put-item \
          --table-name sample_table \
          --item '{"ID": {"S": "003"}, "name": {"S": "Charlie"}, "age": {"N": "35"}}' --region us-west-2
  
        aws dynamodb put-item \
          --table-name sample_table \
          --item '{"ID": {"S": "004"}, "name": {"S": "Diana"}, "age": {"N": "28"}}' --region us-west-2
  
        aws dynamodb put-item \
          --table-name sample_table \
          --item '{"ID": {"S": "005"}, "name": {"S": "Eve"}, "age": {"N": "22"}}' --region us-west-2
      EOT
  }

  depends_on = [aws_dynamodb_table.sample]
}

// for switching
resource "aws_dynamodb_table" "sample2" {
  name         = "sample_table2"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "ID"

  attribute {
    name = "ID"
    type = "S"
  }

  point_in_time_recovery {
    enabled = true
  }

  tags = { Name = "sample_table" }
}

resource "aws_dynamodb_resource_policy" "dynamodb_policy2" {
  resource_arn = aws_dynamodb_table.sample2.arn
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Principal = {
          Service = "redshift.amazonaws.com"
        }
        Action = [
          "dynamodb:ExportTableToPointInTime",
          "dynamodb:DescribeTable"
        ]
        Resource = "arn:aws:dynamodb:${var.aws_region}:${data.aws_caller_identity.current.account_id}:table/${aws_dynamodb_table.sample2.name}"
        Condition = {
          StringEquals = {
            "aws:SourceAccount" = data.aws_caller_identity.current.account_id
          }
          ArnEquals = {
            "aws:SourceArn" = "arn:aws:redshift:${var.aws_region}:${data.aws_caller_identity.current.account_id}:integration:*"
          }
        }
      },
      {
        Effect = "Allow"
        Principal = {
          Service = "redshift.amazonaws.com"
        }
        Action   = "dynamodb:DescribeExport"
        Resource = "arn:aws:dynamodb:${var.aws_region}:${data.aws_caller_identity.current.account_id}:table/${aws_dynamodb_table.sample2.name}/export/*"
        Condition = {
          StringEquals = {
            "aws:SourceAccount" = data.aws_caller_identity.current.account_id
          }
          ArnEquals = {
            "aws:SourceArn" = "arn:aws:redshift:${var.aws_region}:${data.aws_caller_identity.current.account_id}:integration:*"
          }
        }
      }
    ]
  })
}

resource "aws_redshift_subnet_group" "main" {
  name        = "sample-redshift-subnet-group"
  description = "Subnet group for Redshift cluster (private subnets)"
  subnet_ids  = [aws_subnet.private_a.id, aws_subnet.private_b.id]
  tags        = { Name = "sample-redshift-subnet-group" }
}

resource "aws_redshift_parameter_group" "case_sensitive" {
  name   = "sample-redshift-params"
  family = "redshift-1.0"
  parameter {
    name  = "enable_case_sensitive_identifier"
    value = "true"
  }
  tags = { Name = "case-sensitive-params" }
}

resource "aws_redshift_cluster" "main" {
  cluster_identifier           = "sample-redshift-cluster"
  node_type                    = "ra3.xlplus"
  number_of_nodes              = 1
  database_name                = "dev"
  master_username              = "redshiftadmin"
  master_password              = "Ddnf109DHccnwockr74d9dkDNGysfk"
  cluster_subnet_group_name    = aws_redshift_subnet_group.main.name
  cluster_parameter_group_name = aws_redshift_parameter_group.case_sensitive.name
  vpc_security_group_ids       = [aws_security_group.redshift.id]
  publicly_accessible          = false
  encrypted                    = true
  enhanced_vpc_routing         = true
  tags                         = { Name = "sample-redshift-cluster" }
  skip_final_snapshot          = true
}

resource "aws_redshift_resource_policy" "redshift_policy" {
  resource_arn = aws_redshift_cluster.main.cluster_namespace_arn

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Principal = {
          Service = "redshift.amazonaws.com"
        }
        Action   = "redshift:AuthorizeInboundIntegration"
        Resource = aws_redshift_cluster.main.cluster_namespace_arn
        Condition = {
          StringEquals = {
            "aws:SourceArn" = [
              aws_dynamodb_table.sample.arn,
              aws_dynamodb_table.sample2.arn
            ]
          }
        }
      },
      {
        Effect = "Allow"
        Principal = {
          AWS = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:root"
        }
        Action   = "redshift:CreateInboundIntegration"
        Resource = aws_redshift_cluster.main.cluster_namespace_arn
      }
    ]
  })
}

resource "aws_kms_key" "redshift_dynamodb_key" {
  description             = "KMS key for Redshift to decrypt DynamoDB data"
  deletion_window_in_days = 7
}

resource "aws_kms_alias" "redshift_dynamodb_key_alias" {
  name          = "alias/test-redshift-dynamodb-key"
  target_key_id = aws_kms_key.redshift_dynamodb_key.key_id
}

resource "aws_kms_key_policy" "redshift_kms_policy_attachment" {
  key_id = aws_kms_key.redshift_dynamodb_key.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "AllowFullAccessToCurrentAccount"
        Effect = "Allow"
        Principal = {
          AWS = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:root"
        }
        Action   = "kms:*"
        Resource = "*"
      },
      {
        Sid    = "Statement to allow Amazon Redshift service to perform Decrypt operation on the source DynamoDB Table"
        Effect = "Allow"
        Principal = {
          Service = "redshift.amazonaws.com"
        }
        Action = [
          "kms:Decrypt",
          "kms:CreateGrant"
        ]
        Resource = "*"
        Condition = {
          StringEquals = {
            "aws:SourceAccount" = "${data.aws_caller_identity.current.account_id}"
          }
          ArnEquals = {
            "aws:SourceArn" = "arn:aws:redshift:${var.aws_region}:${data.aws_caller_identity.current.account_id}:integration:*"
          }
        }
      }
    ]
  })
}

## !! -----------------------------------  serverless -----------------------------------
resource "aws_redshiftserverless_namespace" "serverless_namespace" {
  namespace_name      = "sample-redshift-serverless-namespace"
  admin_username      = "redshiftadmin"
  admin_user_password = "Ddnf109DHccnwockr74d9dkDNGysfk"
  db_name             = "dev"
}

resource "aws_redshiftserverless_workgroup" "serverless_workgroup" {
  workgroup_name = "sample-redshift-serverless-workgroup"
  namespace_name = aws_redshiftserverless_namespace.serverless_namespace.namespace_name

  base_capacity        = 8
  publicly_accessible  = false
  enhanced_vpc_routing = true

  subnet_ids         = [aws_subnet.private_a.id, aws_subnet.private_b.id, aws_subnet.private_c.id]
  security_group_ids = [aws_security_group.redshift.id]

  config_parameter {
    parameter_key   = "enable_case_sensitive_identifier"
    parameter_value = "true"
  }
}

# これでOKであることを確認。（コンソールでの自動修正で生成されたpolicyと一致）
# KMSは、provisionedと併用可能
# The "aws_redshiftserverless_resource_policy" resource doesn't support the following action types.
# Therefore we need to use the "aws_redshift_resource_policy" resource for RedShift-serverless instead.
resource "aws_redshift_resource_policy" "serverless_integration_policy" {
  resource_arn = aws_redshiftserverless_namespace.serverless_namespace.arn

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Principal = {
          Service = "redshift.amazonaws.com"
        }
        Action   = "redshift:AuthorizeInboundIntegration"
        Resource = aws_redshiftserverless_namespace.serverless_namespace.arn
        Condition = {
          StringEquals = {
            "aws:SourceArn" = [
              aws_dynamodb_table.sample.arn,
              aws_dynamodb_table.sample2.arn
            ]
          }
        }
      },
      {
        Effect = "Allow"
        Principal = {
          AWS = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:root"
        }
        Action   = "redshift:CreateInboundIntegration"
        Resource = aws_redshiftserverless_namespace.serverless_namespace.arn
      }
    ]
  })
}
