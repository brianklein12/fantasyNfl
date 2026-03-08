# Fantasy NFL — Local Dev Notes

A fantasy football application built as a learning project for AWS Lambda, DynamoDB, Memcached, Go, Angular, Swagger, Terraform, GitHub Actions, and Ansible. The guiding principle is **local-first**: prove every piece of the system against docker-compose before wiring it to real AWS infrastructure.

---

## Project Goal

End-to-end ownership of a real production-style system without artificial complexity. The app lets users log in, search NFL players, build a fantasy team, and view player stats by year. Every technology choice is intentional and meant to be understood, not just cargo-culted.

---

## Technology Decisions

| Layer | Choice | Why |
|---|---|---|
| Backend | Go | Fast, explicit, great AWS SDK support, small binaries for Lambda |
| Database | DynamoDB | Learning goal; pairs naturally with Lambda; schema-flexible for evolving stat models |
| Cache | Memcached | Learning goal; simpler than Redis for the cache-aside pattern we need |
| Frontend | Angular | Learning goal; separate repo to keep backend concerns clean |
| API spec | Swagger / OpenAPI | Forces API-first thinking; auto-generates client code |
| IaC | Terraform | Industry standard; pairs with GitHub Actions for CI/CD |
| Config | Ansible | Covers any EC2-based config (e.g. Memcached if not using ElastiCache) |
| Auth | AWS Cognito | Managed user pool; integrates with API Gateway authorizers |

---

## Repository Structure

```
fantasyNfl/
├── cmd/
│   └── ingestor/
│       └── main.go          # CLI binary — runs the ETL locally
├── internal/
│   ├── models/
│   │   ├── player.go        # Player identity struct (PK/SK for single table)
│   │   └── stats.go         # Stat structs for all 4 CSV types
│   ├── parser/
│   │   ├── parser.go        # Shared CSV utilities
│   │   ├── offense.go       # Player offense CSV parser
│   │   ├── defense.go       # Player defense CSV parser
│   │   └── team.go          # Team offense & defense CSV parsers
│   └── storage/
│       ├── dynamodb.go      # DynamoDB client + BatchWriter
│       └── schema.go        # Single-table creation (idempotent)
├── api/
│   └── swagger.yaml         # OpenAPI spec (Phase 2)
├── terraform/
│   ├── main.tf              # Provider config (Phase 3)
│   ├── dynamodb.tf          # Table definitions (Phase 3)
│   └── lambda.tf            # Lambda functions (Phase 3)
├── data/
│   └── *.csv                # NFL stat CSVs from Kaggle (gitignored, ~220 MB)
├── docker-compose.yml       # Local DynamoDB + Memcached
├── Makefile
├── go.mod
└── go.sum
```

---

## Data Source

Eight CSV files downloaded from Kaggle (~220 MB total, gitignored):

| File | Rows (approx) | Description |
|---|---|---|
| `weekly_player_stats_offense.csv` | ~1M | Per-player, per-week passing/rushing/receiving |
| `weekly_player_stats_defense.csv` | ~500K | Per-player, per-week tackles/sacks/interceptions |
| `weekly_team_stats_offense.csv` | ~15K | Per-team, per-week offensive totals |
| `weekly_team_stats_defense.csv` | ~15K | Per-team, per-week defensive totals |
| `yearly_player_stats_offense.csv` | ~50K | Season-aggregate player offense |
| `yearly_player_stats_defense.csv` | ~25K | Season-aggregate player defense |
| `yearly_team_stats_offense.csv` | ~1K | Season-aggregate team offense |
| `yearly_team_stats_defense.csv` | ~1K | Season-aggregate team defense |

---

## DynamoDB Design: Single Table

All entity types — player bios, player stats (offense + defense, weekly + yearly), and team stats — live in one table: **`FantasyNFL`**.

### Why single-table?

The multi-table approach (separate Players, PlayerStats, TeamStats tables) is "SQL thinking" applied to DynamoDB. It forces multiple round-trips to fetch related data and pays the cost of multiple table reads. DynamoDB's real advantage is that all items sharing a partition key are **physically co-located** and returned in sort-key order in a single `Query` call. Single-table design exploits that property. The rule is: model your access patterns first, then design your keys to serve them.

### Table: `FantasyNFL`

| Key | Attribute name | Type |
|---|---|---|
| Partition key | `pk` | String |
| Sort key | `sk` | String |
| GSI1 partition key | `gsi1pk` | String (sparse) |
| GSI1 sort key | `gsi1sk` | String (sparse) |

Key names are intentionally generic (`pk`, `sk`). They hold values for multiple entity types — a concrete domain name like `player_id` would be confusing on an item that's actually a team stat.

### Key scheme by entity type

| Entity | `pk` | `sk` | `gsi1pk` | `gsi1sk` |
|---|---|---|---|---|
| Player bio | `PLAYER#<player_id>` | `#METADATA` | (not set) | (not set) |
| Player offense weekly | `PLAYER#<player_id>` | `STAT#offense_weekly#<season>#<week>` | `POSITION#<pos>` | `SEASON#<season>#WEEK#<week>` |
| Player offense yearly | `PLAYER#<player_id>` | `STAT#offense_yearly#<season>#<season_type>#0` | `POSITION#<pos>` | `SEASON#<season>#<season_type>#WEEK#0` |
| Player defense weekly | `PLAYER#<player_id>` | `STAT#defense_weekly#<season>#<week>` | `POSITION#<pos>` | `SEASON#<season>#WEEK#<week>` |
| Player defense yearly | `PLAYER#<player_id>` | `STAT#defense_yearly#<season>#<season_type>#0` | `POSITION#<pos>` | `SEASON#<season>#<season_type>#WEEK#0` |
| Team offense weekly | `TEAM#<team>` | `STAT#offense_weekly#<season>#<week>` | (not set) | (not set) |
| Team offense yearly | `TEAM#<team>` | `STAT#offense_yearly#<season>#<season_type>#0` | (not set) | (not set) |
| Team defense weekly | `TEAM#<team>` | `STAT#defense_weekly#<season>#<week>` | (not set) | (not set) |
| Team defense yearly | `TEAM#<team>` | `STAT#defense_yearly#<season>#<season_type>#0` | (not set) | (not set) |

### Key design decisions explained

**`PLAYER#` and `TEAM#` type prefixes on `pk`**

Prevents collisions between entity types that might share an ID string. Makes raw table dumps instantly readable — entity type is visible without a separate column. This is idiomatic single-table DynamoDB design.

**`#METADATA` as the sort key for player bio rows**

`#` has a lower ASCII value than any letter or digit. Because DynamoDB returns items within a partition sorted by `sk` ascending, `#METADATA` is always the first item returned for a player query. This means a single `Query` with no SK filter returns bio first, then all stat rows in chronological order — bio and stats in one call, naturally ordered.

**`STAT#<type>#<season>#[<season_type>#]<week>` — ordering within the sort key**

DynamoDB sort key range queries are **left-anchored prefix** operations only. The order of components must match how you'll actually query. Left-to-right for weekly is: type → season → week. For yearly it is: type → season → season_type → week. This supports:
- All offense weekly stats: `begins_with("STAT#offense_weekly")`
- All offense stats in 2023: `begins_with("STAT#offense_weekly#2023")`
- Specific week: exact match `"STAT#offense_weekly#2023#14"`
- All 2024 yearly rows (REG + POST): `begins_with("STAT#offense_yearly#2024")`
- 2024 regular season only: `begins_with("STAT#offense_yearly#2024#REG")`

`season_type` is only embedded in **yearly** SKs because the source data provides separate REG and POST aggregate rows for the same player+season. Without it, both rows would collide on the same DynamoDB item. Weekly SKs don't need it — regular season uses weeks 1–17 and postseason uses weeks 18–22, so the week number is already an implicit season_type discriminator.

If week came before season, all Week 1s across all years would be grouped together — almost never what you want.

**GSI1 — sparse index for position-based search**

`gsi1pk` and `gsi1sk` are only set on player stat rows. DynamoDB only indexes items that have both GSI key attributes present — everything else is invisible to the GSI. This is called a **sparse index**. Benefits:
- The index is smaller than the main table
- Cheaper write cost (fewer write capacity units spent maintaining the index)
- Queries return only meaningful items — no player bio or team rows mixed in

`gsi1pk = "POSITION#WR"` + `gsi1sk begins_with "SEASON#2023"` answers: "all WR stats in 2023" — the core roster-building query.

### Access patterns served

| Query | DynamoDB call |
|---|---|
| Player bio by ID | `GetItem` PK=`PLAYER#<id>` SK=`#METADATA` |
| All stats for a player | `Query` PK=`PLAYER#<id>` SK `begins_with("STAT#")` |
| Player offense stats for 2023 | `Query` PK=`PLAYER#<id>` SK `begins_with("STAT#offense_weekly#2023")` |
| Player stats for a specific week | `GetItem` PK=`PLAYER#<id>` SK=`STAT#offense_weekly#2023#14` |
| All WRs in 2023 | `Query` on GSI1, PK=`POSITION#WR`, SK `begins_with("SEASON#2023")` |
| Team stats for a season | `Query` PK=`TEAM#KC` SK `begins_with("STAT#offense_weekly#2023")` |

---

## Phase 1: ETL Ingestor (Complete)

### What it does

The ingestor binary reads a CSV file row-by-row and batch-writes all records (player bio + stats, or team stats) into the single `FantasyNFL` table. It handles all 8 CSV types via a `<csv-type>` argument. Because files are up to 100 MB, the parser streams one row at a time — no file is loaded into memory.

### Why batch writes?

DynamoDB's `BatchWriteItem` API accepts up to 25 items per request. Sending individual `PutItem` calls at scale would be ~25x slower and cost ~25x more in write capacity units. The `BatchWriter` in `internal/storage/dynamodb.go` accumulates items and auto-flushes at 25.

### Why separate player bio from stat rows?

Player bio data (height, weight, college, draft info) appears on every row for the same player in the CSV, but it doesn't change week to week. Emitting it as a distinct `#METADATA` item means:
- Bio is written once per player, not once per game week (millions fewer writes)
- Stat rows stay lean and uniform — they only carry the fields that change each week
- A future `GET /players/{id}` API fetches bio and stats in one `Query` call, getting the `#METADATA` item first and stat items after, all sorted chronologically

### Local vs. production mode

`NewClient()` in `internal/storage/dynamodb.go` reads the `DYNAMODB_ENDPOINT` environment variable. When set, it uses dummy static credentials and connects to the local docker-compose DynamoDB. When absent, it uses the standard AWS credential chain (env vars → `~/.aws` → IAM role). The same binary works in both environments with no code changes — this matters for the future Lambda handler, which will use the same packages.

### Parser design

Each parser (`offense.go`, `defense.go`, `team.go`) uses shared helpers from `parser.go`:

- `colIndex(headers)` — builds a `map[string]int` from the header row, so column lookups are by name, not fragile positional indices. The offense CSV has 393 columns; name-based access is the only sane approach.
- `field(row, idx, col)` — safe string read; returns `""` if the column is absent
- `num(row, idx, col)` — parses a float64; returns 0 on blank/error (common in sparse stat files)
- `newCSVReader(r)` — wraps `encoding/csv` with `ReuseRecord: true` (lower GC pressure on millions of rows) and `LazyQuotes: true` (tolerates messy real-world CSV data)

Each parser accepts callback functions (`onPlayer`, `onStat`) instead of returning slices. Records are handed off to the `BatchWriter` immediately — memory stays flat.

---

## Local Development

### Prerequisites

- Go 1.25+
- Docker + Docker Compose

### Start local services

```bash
make docker-up
```

Starts DynamoDB Local on `localhost:8000` and Memcached on `localhost:11211`.

### Create the table

```bash
make tables
```

Creates the `FantasyNFL` table in the local DynamoDB. Safe to re-run — existing tables are skipped.

### Ingest a CSV file

```bash
# Player offense (weekly)
make ingest TYPE=offense-player FILE=data/weekly_player_stats_offense.csv

# Player defense (weekly)
make ingest TYPE=defense-player FILE=data/weekly_player_stats_defense.csv

# Team offense (weekly)
make ingest TYPE=team-offense FILE=data/weekly_team_stats_offense.csv

# Team defense (weekly)
make ingest TYPE=team-defense FILE=data/weekly_team_stats_defense.csv

# Yearly variants
make ingest TYPE=offense-player-yr FILE=data/yearly_player_stats_offense.csv
make ingest TYPE=defense-player-yr FILE=data/yearly_player_stats_defense.csv
make ingest TYPE=team-offense-yr   FILE=data/yearly_team_stats_offense.csv
make ingest TYPE=team-defense-yr   FILE=data/yearly_team_stats_defense.csv
```

### Inspect data (no AWS CLI needed)

```bash
# Count all items
curl -s -X POST http://localhost:8000 \
  -H "Content-Type: application/x-amz-json-1.0" \
  -H "X-Amz-Target: DynamoDB_20120810.Scan" \
  -H "Authorization: AWS4-HMAC-SHA256 Credential=local/20260302/us-east-1/dynamodb/aws4_request, SignedHeaders=host;x-amz-date;x-amz-target, Signature=fake" \
  -H "X-Amz-Date: 20260302T000000Z" \
  -d '{"TableName":"FantasyNFL","Select":"COUNT"}'

# Peek at raw items (first 2)
curl -s -X POST http://localhost:8000 \
  -H "Content-Type: application/x-amz-json-1.0" \
  -H "X-Amz-Target: DynamoDB_20120810.Scan" \
  -H "Authorization: AWS4-HMAC-SHA256 Credential=local/20260302/us-east-1/dynamodb/aws4_request, SignedHeaders=host;x-amz-date;x-amz-target, Signature=fake" \
  -H "X-Amz-Date: 20260302T000000Z" \
  -d '{"TableName":"FantasyNFL","Limit":2}'
```

### Build only

```bash
make build       # outputs bin/ingestor
make test        # go test -v ./...
make fmt         # go fmt ./...
```

---

## Go Dependencies

```
github.com/aws/aws-sdk-go-v2                                   # AWS SDK core
github.com/aws/aws-sdk-go-v2/config                            # credential loading
github.com/aws/aws-sdk-go-v2/service/dynamodb                  # DynamoDB client
github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue   # struct marshaling
```

Deferred (added in later phases):
- `github.com/aws/aws-lambda-go` — Lambda handler wrapper (Phase 3)
- `github.com/bradfitz/gomemcache` — Memcached client (Phase 2)

---

## Roadmap

### Phase 2 — REST API
- HTTP server with versioned routes (`/api/v1/...`)
- `GET /api/v1/players` — search players by name/position/season (uses GSI1)
- `GET /api/v1/players/{id}` — player bio + stats in one Query call
- `GET /api/v1/players/{id}/stats` — player stats, filterable by season/week
- `GET /api/v1/teams/{team}/stats` — team stats
- Swagger spec in `api/swagger.yaml`
- Memcached cache-aside: reads check cache first, fall back to DynamoDB, populate cache on miss

### Phase 3 — AWS Infrastructure
- Terraform: S3 bucket (CSV landing zone), Lambda functions, DynamoDB `FantasyNFL` table, API Gateway, IAM roles
- S3 event trigger → Lambda → same internal parser/storage packages used by the local ingestor
- GitHub Actions: lint → test → build → `terraform apply`
- Ansible: config management for any EC2-hosted services

### Phase 4 — Frontend
- Angular app in a separate repository
- AWS Cognito for login/registration
- Player search UI (backed by GSI1 position query)
- Fantasy team builder
- Stats viewer (filter by year, week, position)

---

## Key Design Principles

1. **Local-first**: Every component works against docker-compose before any real AWS is involved.
2. **Access-pattern-driven schema**: DynamoDB table design starts with the queries the app needs, not with the data shape.
3. **Single table, all entities**: One `FantasyNFL` table holds players, stats, and teams. Co-location enables single-call fetches.
4. **Sparse GSI**: Only player stat rows carry GSI keys. Team and metadata rows are invisible to the position index — keeping it lean.
5. **Streaming over loading**: Files up to 100 MB are read one row at a time. Memory usage is flat regardless of file size.
6. **Code reuse across environments**: The same `internal/` packages power both the local CLI binary and future Lambda handlers.
7. **Fail loudly**: Errors are wrapped with context and surfaced immediately. No silent skips on bad data.
8. **Idempotent setup**: `CreateTables` and future Terraform runs are safe to re-run without side effects.
