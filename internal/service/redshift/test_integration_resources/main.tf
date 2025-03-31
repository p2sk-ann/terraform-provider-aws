variable "aws_region" {
  description = "AWS region to deploy resources"
  type        = string
  default     = "us-west-2"
}

provider "aws" {
  region = "us-west-2"
}

data "aws_caller_identity" "current" {}

# resource "aws_redshift_integration" "sample-dynamodb" {
#   integration_name = "hogehoge223"
#   description      = "fugafuga223"
#   source_arn       = "arn:aws:dynamodb:us-west-2:490004623576:table/sample_table"
#   # source_arn = "arn:aws:dynamodb:us-west-2:490004623576:table/sample_table"
#   target_arn = "arn:aws:redshift:us-west-2:490004623576:namespace:661deecf-6ec2-4389-b04e-27eb3a1cc189"
#   # target_arn = "arn:aws:redshift-serverless:us-west-2:490004623576:namespace/c3465720-ce4e-4d0a-9e5b-aa2ce48d683f"

#   # kms_key_id = "arn:aws:kms:us-west-2:490004623576:key/13d6f1e4-0c19-46bb-bf92-215b413be14e"
#   # additional_encryption_context = {
#   #   "example1" : "test1",
#   #   "example2" : "test2",
#   # }

#   tags = {
#     Environment = "dev"
#     Owner       = "hawaii"
#     Project     = "hey"
#   }
# }

resource "aws_redshift_integration" "sample-s3" {
  integration_name = "s3-integration-hoge"
  description      = "s3-integration-fuga"
  source_arn       = "arn:aws:s3:::sample-s3-redshift-integration"
  # source_arn = "arn:aws:s3:::sample-s3-redshift-integration2"
  target_arn = "arn:aws:redshift:us-west-2:490004623576:namespace:661deecf-6ec2-4389-b04e-27eb3a1cc189"
  # target_arn = "arn:aws:redshift-serverless:us-west-2:490004623576:namespace/c2f4bc36-309b-48f2-9f7a-2092bf391df7"

  tags = {
    Environment = "dev"
    City        = "tokyo"
    Project     = "hi"
  }
}
