# Variables
BINARY_NAME=ingestor
DYNAMODB_ENDPOINT ?= http://localhost:8000

.PHONY: all build build-lambda deploy-lambda test fmt lint clean docker-up docker-down tables ingest

# The "CI" command - runs everything
ci: fmt lint test build

# Formatting (Go has this built-in)
fmt:
	go fmt ./...

# Linting (Requires golangci-lint)
lint:
	golangci-lint run

# Testing
test:
	go test -v ./...

# Build the ingestor binary
build:
	go build -o bin/$(BINARY_NAME) ./cmd/ingestor/main.go

# Clean up
clean:
	rm -rf bin/

# Start local DynamoDB and Memcached
docker-up:
	docker compose up -d

# Stop local services
docker-down:
	docker compose down

# Create DynamoDB tables in the local instance
tables: build
	DYNAMODB_ENDPOINT=$(DYNAMODB_ENDPOINT) ./bin/$(BINARY_NAME) -create-tables offense-player /dev/null

# Ingest a CSV file. Usage: make ingest TYPE=offense-player FILE=data/weekly_player_stats_offense.csv
ingest: build
	@if [ -z "$(FILE)" ]; then echo "Usage: make ingest TYPE=<csv-type> FILE=<path>"; exit 1; fi
	@if [ -z "$(TYPE)" ]; then echo "Usage: make ingest TYPE=<csv-type> FILE=<path>"; exit 1; fi
	DYNAMODB_ENDPOINT=$(DYNAMODB_ENDPOINT) ./bin/$(BINARY_NAME) $(TYPE) $(FILE)

# Build the Lambda binary and zip it for deployment.
#
# The binary must be named "bootstrap" — that is the entry point AWS expects
# for the provided.al2023 runtime. GOOS/GOARCH are technically redundant on
# WSL x86, but we set them explicitly so the same command works correctly in
# GitHub Actions CI runners without relying on the runner's default environment.
build-lambda:
	GOOS=linux GOARCH=amd64 go build -o bin/bootstrap ./cmd/lambda/main.go
	cd bin && zip -q function.zip bootstrap

# Build the Lambda zip and deploy to AWS via Terraform.
#
# Terraform's aws_s3_object resource uploads the zip to the artifact S3 bucket,
# and aws_lambda_function detects the changed source_code_hash and updates the
# function. One command does it all — no separate manual upload step.
deploy-lambda: build-lambda
	cd terraform && terraform apply

ingest-all: build
	@echo "Starting full data ingestion..."
	$(MAKE) ingest TYPE=offense-player FILE=data/weekly_player_stats_offense.csv
	$(MAKE) ingest TYPE=defense-player FILE=data/weekly_player_stats_defense.csv
	$(MAKE) ingest TYPE=team-offense    FILE=data/weekly_team_stats_offense.csv
	$(MAKE) ingest TYPE=team-defense    FILE=data/weekly_team_stats_defense.csv
	$(MAKE) ingest TYPE=offense-player-yr FILE=data/yearly_player_stats_offense.csv
	$(MAKE) ingest TYPE=defense-player-yr FILE=data/yearly_player_stats_defense.csv
	$(MAKE) ingest TYPE=team-offense-yr   FILE=data/yearly_team_stats_offense.csv
	$(MAKE) ingest TYPE=team-defense-yr   FILE=data/yearly_team_stats_defense.csv
	@echo "Full ingestion complete!"