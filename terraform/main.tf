provider "aws" {
  region = "us-east-1"

  default_tags {
    tags = {
      Project   = "FantasyInsight"
      ManagedBy = "Terraform"
      Owner     = "Brian"
    }
  }
}

resource "aws_s3_bucket" "csv_ingestion" {
  bucket = "csv-ingestion-data-fantasy-app-342026"
}

resource "aws_s3_bucket_public_access_block" "security_lock" {
  bucket = aws_s3_bucket.csv_ingestion.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_server_side_encryption_configuration" "encryption" {
  bucket = aws_s3_bucket.csv_ingestion.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_iam_role" "lambda_execution_role" {
  name = "fantasy-ingest-lambda-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = {
        Service = "lambda.amazonaws.com"
      }
    }]
  })
}

resource "aws_iam_role_policy" "lambda_s3_read" {
  name = "s3-read-permissions"
  role = aws_iam_role.lambda_execution_role.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["s3:GetObject"]
        Resource = "${aws_s3_bucket.csv_ingestion.arn}/*"
      },
      {
        Effect   = "Allow"
        Action   = [
          "logs:CreateLogGroup",
          "logs:CreateLogStream",
          "logs:PutLogEvents"
        ]
        Resource = "arn:aws:logs:*:*:*"
      }
    ]
  })
}

resource "aws_dynamodb_table" "terraform_locks" {
  name         = "terraform-state-locking"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "LockID"

  attribute {
    name = "LockID"
    type = "S"
  }
}

terraform {
  backend "s3" {
    bucket         = "csv-ingestion-data-fantasy-app-342026"
    key            = "state/terraform.tfstate" # This is the "folder/file" inside the bucket
    region         = "us-east-1"
    encrypt        = true
    use_lockfile   = true # The 2026 native S3 locking
  }
}

resource "aws_s3_bucket_versioning" "state_versioning" {
  bucket = "csv-ingestion-data-fantasy-app-342026"
  versioning_configuration {
    status = "Enabled"
  }
}