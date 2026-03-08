# Fantasy NFL — Claude Working Preferences

## Communication Style

- Explain the *why* inline as code is written, not just what the code does.
- Discuss approach conversationally before generating code. Never silently generate a
  large block of code without first walking through the design decisions.
- Keep explanations grounded in the specific technology being used (Go, DynamoDB,
  Terraform, Lambda). Avoid generic advice.
- When a decision has tradeoffs, name them explicitly so the intent is clear.

## Workflow Preferences

- **Focused sessions**: One component per session. Don't sprawl across unrelated areas.
- **Local-first**: Prove every flow against docker-compose (local DynamoDB, Memcached)
  before wiring to real AWS infrastructure.
- **Read before writing**: Always read a file before editing it.
- **No over-engineering**: Only make changes that are directly requested or clearly
  necessary. Don't add features, refactor surrounding code, or create abstractions for
  one-time use.
- **Plan before implementing**: For multi-step work, use plan mode to outline the
  approach and get approval before writing code.

## Tech Stack Summary

| Layer         | Choice              | Notes                                          |
|---|---|---|
| Language      | Go 1.25             | Module: `github.com/brianklein12/fantasy-football` |
| Database      | DynamoDB            | Single-table design, table name: `FantasyNFL`  |
| Cache         | Memcached           | Cache-aside pattern (Phase 2)                  |
| Cloud compute | AWS Lambda          | `provided.al2023` runtime, `bootstrap` binary  |
| IaC           | Terraform           | State in S3 + DynamoDB lock                    |
| CI/CD         | GitHub Actions      | Phase 3                                        |
| Frontend      | Angular             | Separate repository, Cognito auth              |

## Project Phases

1. ✅ **ETL ingestor** — local CLI, CSV → single-table DynamoDB
2. 🔄 **Lambda + Terraform** — S3-triggered Lambda, same internal packages, cloud deploy
3. **REST API** — `net/http`, `/api/v1/` routes, Swagger, Memcached cache-aside
4. **AWS infrastructure** — API Gateway, Cognito, full Terraform, GitHub Actions CI/CD
5. **Angular frontend** — separate repo, player search, fantasy team builder

## DynamoDB Single-Table Design

Table: `FantasyNFL`, billing: PAY_PER_REQUEST

| Entity                | `pk`                | `sk`                                          |
|---|---|---|
| Player bio            | `PLAYER#<id>`       | `#METADATA`                                   |
| Player stat (weekly)  | `PLAYER#<id>`       | `STAT#offense_weekly#<season>#<week>`         |
| Player stat (yearly)  | `PLAYER#<id>`       | `STAT#offense_yearly#<season>#<season_type>#0`|
| Team stat             | `TEAM#<team>`       | `STAT#offense_weekly#<season>#<week>`         |

GSI1 (`gsi1pk` / `gsi1sk`) is sparse — set only on player stat rows.
`gsi1pk = "POSITION#WR"` + `gsi1sk begins_with "SEASON#2023"` → all WR stats in 2023.

## Lambda Conventions

- Runtime: `provided.al2023` (current Go runtime; `go1.x` is deprecated)
- Binary name: `bootstrap` (required by `provided` runtimes)
- Architecture: `x86_64` (matches WSL / GitHub Actions x86 runners)
- CSV type routing: S3 key prefix before the first `/` (e.g. `offense-player/file.csv`)
- Table name: `TABLE_NAME` env var, fallback to `storage.TableName` constant

## Key Internal Packages

- `internal/parser` — streaming CSV parsers, callback-based, one row at a time
- `internal/storage` — `BatchWriter` (25-item batches, last-write-wins dedup),
  `MergePlayerMetadata` (per-field `if_not_exists` via `UpdateItem`)
- `internal/models` — `Player`, `PlayerOffenseStat`, `PlayerDefenseStat`,
  `TeamOffenseStat`, `TeamDefenseStat`
