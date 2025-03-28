variable "aws_region" {
  description = "AWS region to deploy resources"
  type        = string
  default     = "us-west-2"
}

provider "aws" {
  region = "us-west-2"
}

data "aws_caller_identity" "current" {}

resource "aws_redshift_integration" "sample" {
  integration_name = "hogehoge223"
  description      = "fugafuga223"
  source_arn       = "arn:aws:dynamodb:us-west-2:490004623576:table/sample_table"
  # source_arn = "arn:aws:dynamodb:us-west-2:490004623576:table/sample_table"
  # target_arn = "arn:aws:redshift:us-west-2:490004623576:namespace:8c12dff8-d488-4bed-8363-6ba77bd0c3a4"
  target_arn = "arn:aws:redshift-serverless:us-west-2:490004623576:namespace/c3465720-ce4e-4d0a-9e5b-aa2ce48d683f"

  # kms_key_id = "arn:aws:kms:us-west-2:490004623576:key/13d6f1e4-0c19-46bb-bf92-215b413be14e"
  # additional_encryption_context = {
  #   "example1" : "test1",
  #   "example2" : "test2",
  # }

  tags = {
    Environment = "dev"
    Owner       = "hawaii"
    Project     = "hey"
  }
}
