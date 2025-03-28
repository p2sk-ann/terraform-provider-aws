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
  integration_name = "test0328"
  source_arn       = "arn:aws:dynamodb:us-west-2:490004623576:table/sample_table"
  target_arn       = "arn:aws:redshift:us-west-2:490004623576:namespace:5bae1772-241d-4928-96cb-63412625f943"
}
