# ─── Lambda artifact S3 bucket ──────────────────────────────────────────────
#
# Stores the zipped Lambda binary. Kept separate from the CSV ingestion bucket
# for two reasons:
#   1. No accidental trigger: the S3 notification on the CSV bucket fires on
#      any ".csv" upload; a zip upload there would hit an unknown prefix and
#      error. A dedicated bucket has no trigger at all.
#   2. Separate lifecycle: Lambda artifacts need versioning (for rollback);
#      ingested CSVs may be deleted after processing. Different buckets allow
#      different lifecycle policies.
resource "aws_s3_bucket" "lambda_artifacts" {
  bucket = "fantasy-nfl-lambda-artifacts-342026"
}

# Block all public access. Lambda artifacts contain compiled business logic —
# they should never be publicly readable.
resource "aws_s3_bucket_public_access_block" "lambda_artifacts" {
  bucket = aws_s3_bucket.lambda_artifacts.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# Versioning: every `terraform apply` replaces the zip object with the new
# build. Versioning retains all previous zips so you can roll back by pointing
# the Lambda at an older version or running `terraform apply` from an older
# commit.
resource "aws_s3_bucket_versioning" "lambda_artifacts" {
  bucket = aws_s3_bucket.lambda_artifacts.id

  versioning_configuration {
    status = "Enabled"
  }
}

# Explicit AES256 encryption. AWS enables SSE-S3 by default since 2023, but
# we configure it explicitly to match the pattern on the CSV ingestion bucket
# and to make the intent visible in code rather than relying on an implicit default.
resource "aws_s3_bucket_server_side_encryption_configuration" "lambda_artifacts" {
  bucket = aws_s3_bucket.lambda_artifacts.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

# ─── Lambda artifact zip object ─────────────────────────────────────────────
#
# Terraform uploads the local zip to the artifact bucket during `terraform apply`.
# This means `make deploy-lambda` (build zip + terraform apply) is a complete,
# self-contained deploy — no separate manual S3 upload step.
#
# etag: Terraform compares the local MD5 to what's in S3. If the file hasn't
# changed, the upload is skipped. If it has changed, Terraform uploads the new
# zip and updates the Lambda function.
resource "aws_s3_object" "lambda_zip" {
  bucket = aws_s3_bucket.lambda_artifacts.id
  key    = "function.zip"
  source = "${path.module}/../bin/function.zip"
  etag   = filemd5("${path.module}/../bin/function.zip")
}

# ─── Lambda function ─────────────────────────────────────────────────────────
resource "aws_lambda_function" "csv_ingestor" {
  function_name = "fantasy-csv-ingestor"

  # Where Lambda fetches the code from at deploy time. Lambda copies the zip
  # into its own internal storage — S3 is not read on every invocation.
  s3_bucket = aws_s3_bucket.lambda_artifacts.id
  s3_key    = aws_s3_object.lambda_zip.key

  # source_code_hash lets Terraform detect when the zip has changed.
  # Without it, Terraform sees no change to the s3_key and skips the Lambda
  # update even when the zip contents have changed.
  source_code_hash = filebase64sha256("${path.module}/../bin/function.zip")

  # provided.al2023: the current Go Lambda runtime. AWS deprecated go1.x.
  # "provided" means we supply the runtime ourselves — our binary IS the runtime.
  runtime = "provided.al2023"

  # For provided runtimes, AWS invokes whatever binary is named "bootstrap"
  # inside the zip. This is an AWS convention, not a Go thing. Our Makefile
  # builds the binary as bin/bootstrap and zips it as function.zip.
  handler = "bootstrap"

  # x86_64 matches the WSL / GitHub Actions runner architecture.
  # GOOS=linux GOARCH=amd64 in the build step compiles for this target.
  architectures = ["x86_64"]

  # 900 seconds = 15 minutes, the Lambda maximum. The weekly offense CSV has
  # ~1M rows streamed through the parser sequentially. 15 minutes gives ample
  # headroom even on a cold start with DynamoDB throttle retries.
  timeout = 400

  # 512 MB is generous for a streaming parser that holds one CSV row in memory
  # at a time. Go's GC pressure stays flat regardless of file size.
  # Lambda bills per GB-second; 512 MB keeps cost proportional to actual work.
  memory_size = 512

  # The IAM role Lambda assumes when it runs. This role has:
  #   - S3 GetObject on the CSV ingestion bucket (read the uploaded file)
  #   - DynamoDB BatchWriteItem + PutItem + UpdateItem on the FantasyNFL table
  #   - CloudWatch Logs CreateLogGroup + CreateLogStream + PutLogEvents
  # All three policies are attached to this single role.
  role = aws_iam_role.lambda_execution_role.arn

  environment {
    variables = {
      # TABLE_NAME lets the Lambda target a different DynamoDB table than the
      # local CLI default. Useful for running a dev Lambda against a test table
      # without recompiling.
      TABLE_NAME = aws_dynamodb_table.fantasy_nfl.name
    }
  }

  tags = {
    Project     = "FantasyNFL"
    Environment = "production"
  }
}

# ─── DynamoDB write policy ───────────────────────────────────────────────────
#
# Placed here (not main.tf) because it references the DynamoDB table defined
# in dynamodb.tf. Grouping it with Lambda resources keeps the "what Lambda is
# allowed to do" story in one file.
#
# Resource is scoped to the specific table ARN — not "*". Least privilege:
# if this Lambda's credentials were ever leaked, the blast radius is one table.
resource "aws_iam_role_policy" "lambda_dynamodb_write" {
  name  = "fantasy-lambda-dynamodb-write"
  role  = aws_iam_role.lambda_execution_role.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          # BatchWriteItem: the primary write path — 25 items per request.
          "dynamodb:BatchWriteItem",
          # PutItem: used by MergePlayerMetadata's fallback path (not currently
          # called, but safe to include for future use).
          "dynamodb:PutItem",
          # UpdateItem: used by MergePlayerMetadata for per-field if_not_exists
          # semantics — keeps player bio data additive across multiple CSV ingest runs.
          "dynamodb:UpdateItem",
        ]
        Resource = aws_dynamodb_table.fantasy_nfl.arn
      }
    ]
  })
}

# ─── S3 → Lambda event notification ─────────────────────────────────────────
#
# Tells S3 to invoke the Lambda whenever a ".csv" object is created in the
# ingestion bucket. The Lambda then reads the object, parses it, and writes
# to DynamoDB.
#
# depends_on: S3 validates that the Lambda permission (s3_invoke below) exists
# before it will accept the notification configuration. Without explicit
# depends_on, Terraform may create the notification and the permission in
# parallel, causing the S3 API call to fail with an "invalid Lambda function"
# error. Explicit depends_on serialises these two resources.
resource "aws_s3_bucket_notification" "csv_trigger" {
  bucket = aws_s3_bucket.csv_ingestion.id

  lambda_function {
    lambda_function_arn = aws_lambda_function.csv_ingestor.arn
    events              = ["s3:ObjectCreated:*"]
    filter_suffix       = ".csv"
  }

  depends_on = [aws_lambda_permission.s3_invoke]
}

# ─── Lambda resource-based permission ────────────────────────────────────────
#
# Lambda has a two-layer access model:
#   1. Execution role (aws_iam_role): what the Lambda CAN DO (call DynamoDB, S3, etc.)
#   2. Resource-based permission (this resource): who CAN INVOKE the Lambda
#
# This permission grants s3.amazonaws.com the right to invoke the function.
# source_arn scopes it to only our CSV ingestion bucket — if another S3 bucket
# somehow got our Lambda ARN, it could not invoke it. This is the confused-deputy
# protection: even if another principal tricks S3 into making the call, the
# permission only honours requests from this specific bucket.
resource "aws_lambda_permission" "s3_invoke" {
  statement_id  = "AllowS3Invoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.csv_ingestor.function_name
  principal     = "s3.amazonaws.com"

  # Scope to only the CSV ingestion bucket (confused-deputy protection).
  source_arn = aws_s3_bucket.csv_ingestion.arn
}
