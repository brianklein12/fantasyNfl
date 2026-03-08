# ─── GitHub Actions OIDC Identity Provider ───────────────────────────────────
#
# Registers GitHub as a trusted OIDC identity provider in this AWS account.
# Done once per account — not per repo. Tells AWS: "I accept JWTs signed by
# token.actions.githubusercontent.com and will validate them against GitHub's
# public keys."
#
# AWS has managed thumbprint rotation for this provider automatically since
# 2023 — it validates against its own CA bundle, so this value never needs
# updating even as GitHub rotates certificates.
resource "aws_iam_openid_connect_provider" "github" {
  url             = "https://token.actions.githubusercontent.com"
  client_id_list  = ["sts.amazonaws.com"]
  thumbprint_list = ["6938fd4d98bab03faadb97b34396831e3780aea1"]
}

# ─── GitHub Actions CI/CD IAM Role ───────────────────────────────────────────
#
# The role GitHub Actions assumes via OIDC when the deploy workflow runs.
# Separate from the Lambda execution role — one is "what CI/CD can do in AWS",
# the other is "what the Lambda function can do at runtime."
#
# Trust policy is locked to main branch pushes only.
# PR workflows get sub = "repo:owner/repo:pull_request" which does NOT match,
# so PRs cannot assume this role at all. PRs run build/test only — no AWS
# access needed. This is intentional: terraform apply only runs post-merge.
resource "aws_iam_role" "github_actions" {
  name = "github-actions-fantasy-deploy"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Principal = {
          # The OIDC provider — not a specific account or IAM user.
          # Only callers bearing a valid JWT from GitHub can attempt this.
          Federated = aws_iam_openid_connect_provider.github.arn
        }
        Action = "sts:AssumeRoleWithWebIdentity"
        Condition = {
          StringEquals = {
            # aud: the token must have been requested specifically for AWS STS.
            # aws-actions/configure-aws-credentials hardcodes this value.
            "token.actions.githubusercontent.com:aud" = "sts.amazonaws.com"

            # sub: the security gate for a public repo.
            # GitHub sets this to the actual repo identity in the signed JWT —
            # workflow code cannot override it.
            #
            # A fork gets:     repo:theiruser/fantasyNfl:ref:refs/heads/main
            # A PR gets:       repo:brianklein12/fantasyNfl:pull_request
            # A main push gets: repo:brianklein12/fantasyNfl:ref:refs/heads/main
            #
            # Only a main branch push on your repo matches. Forks and PRs are
            # rejected at the AssumeRoleWithWebIdentity call.
            "token.actions.githubusercontent.com:sub" = "repo:brianklein12/fantasyNfl:ref:refs/heads/main"
          }
        }
      }
    ]
  })

  tags = {
    Project = "FantasyNFL"
  }
}

# ─── CI Role Permissions ─────────────────────────────────────────────────────
#
# Inline policy (embedded in the role, deleted with the role) rather than a
# managed policy attachment. These permissions are specific to this role and
# serve no purpose detached from it.
#
# Approach: service-level wildcards scoped to specific resource ARNs.
# Tighter than PowerUserAccess (which covers ALL AWS services) but avoids
# enumerating every individual Terraform provider API call, which becomes a
# maintenance burden every time a new .tf resource is added.
#
# The actual security boundary is the trust policy above — the sub claim
# ensures only your main branch can assume this role. The permission set
# is a least-privilege-by-service layer on top of that.
resource "aws_iam_role_policy" "github_actions_permissions" {
  name = "github-actions-deploy-permissions"
  role = aws_iam_role.github_actions.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      # ── S3 ────────────────────────────────────────────────────────────────
      # Terraform manages three S3 buckets: state, artifact, CSV ingestion.
      # All three need bucket-level and object-level access.
      {
        Effect = "Allow"
        Action = ["s3:*"]
        Resource = [
          "arn:aws:s3:::csv-ingestion-data-fantasy-app-342026",
          "arn:aws:s3:::csv-ingestion-data-fantasy-app-342026/*",
          "arn:aws:s3:::fantasy-nfl-lambda-artifacts-342026",
          "arn:aws:s3:::fantasy-nfl-lambda-artifacts-342026/*",
        ]
      },

      # ── Lambda ────────────────────────────────────────────────────────────
      # Covers create, update code, update config, add/remove permission, get.
      # Scoped to only this function — CI cannot touch any other Lambda.
      {
        Effect   = "Allow"
        Action   = ["lambda:*"]
        Resource = "arn:aws:lambda:us-east-1:*:function:fantasy-csv-ingestor"
      },

      # ── DynamoDB ──────────────────────────────────────────────────────────
      # Terraform manages two tables: the app table and the state lock table.
      {
        Effect = "Allow"
        Action = ["dynamodb:*"]
        Resource = [
          "arn:aws:dynamodb:us-east-1:*:table/FantasyNFL",
          "arn:aws:dynamodb:us-east-1:*:table/terraform-state-locking",
        ]
      },

      # ── IAM ───────────────────────────────────────────────────────────────
      # Terraform manages the Lambda execution role, its inline policies, the
      # GitHub OIDC provider, and this CI role itself. All scoped to specific
      # resource ARNs — CI cannot create or modify arbitrary IAM resources.
      # PassRole is required to assign the execution role to the Lambda function.
      {
        Effect = "Allow"
        Action = [
          "iam:GetRole",
          "iam:CreateRole",
          "iam:DeleteRole",
          "iam:UpdateRole",
          "iam:UpdateAssumeRolePolicy",
          "iam:TagRole",
          "iam:UntagRole",
          "iam:GetRolePolicy",
          "iam:PutRolePolicy",
          "iam:DeleteRolePolicy",
          "iam:ListRolePolicies",
          "iam:ListAttachedRolePolicies",
          "iam:PassRole",
          "iam:GetOpenIDConnectProvider",
          "iam:CreateOpenIDConnectProvider",
          "iam:DeleteOpenIDConnectProvider",
          "iam:TagOpenIDConnectProvider",
        ]
        Resource = [
          "arn:aws:iam::*:role/fantasy-ingest-lambda-role",
          "arn:aws:iam::*:role/github-actions-fantasy-deploy",
          "arn:aws:iam::*:oidc-provider/token.actions.githubusercontent.com",
        ]
      },

      # ── CloudWatch Logs ───────────────────────────────────────────────────
      # Terraform reads log group metadata during plan. Lambda creates the log
      # group itself on first invocation — CI only needs describe, not write.
      {
        Effect   = "Allow"
        Action   = ["logs:DescribeLogGroups"]
        Resource = "*"
      }
    ]
  })
}
