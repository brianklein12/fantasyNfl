// Command lambda is the AWS Lambda entry point for the Fantasy NFL CSV ingestor.
//
// It is triggered by S3 ObjectCreated events. Each event record carries a bucket
// name and object key. The key's prefix (the path component before the first "/")
// determines which CSV parser to invoke — the actual filename is irrelevant:
//
//	offense-player/weekly_player_stats_offense.csv  → ParsePlayerOffense
//	defense-player/weekly_player_stats_defense.csv  → ParsePlayerDefense
//	team-offense/weekly_team_stats_offense.csv       → ParseTeamOffense
//	team-defense/weekly_team_stats_defense.csv       → ParseTeamDefense
//	offense-player-yr/yearly_player_stats_offense.csv → ParsePlayerOffense (yearly)
//	defense-player-yr/yearly_player_stats_defense.csv → ParsePlayerDefense (yearly)
//	team-offense-yr/yearly_team_stats_offense.csv    → ParseTeamOffense (yearly)
//	team-defense-yr/yearly_team_stats_defense.csv    → ParseTeamDefense (yearly)
//
// Environment variables:
//
//	TABLE_NAME   DynamoDB table name (defaults to storage.TableName = "FantasyNFL")
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/brianklein12/fantasy-football/internal/models"
	"github.com/brianklein12/fantasy-football/internal/parser"
	"github.com/brianklein12/fantasy-football/internal/storage"
)

// Clients are constructed once at cold-start and reused across invocations.
// Lambda reuses the same process for multiple invocations when the execution
// environment is warm — initialising clients outside the handler avoids
// re-paying the SDK config-load cost on every event.
var (
	dynamoClient *dynamodb.Client
	s3Client     *s3.Client
	tableName    string
)

func init() {
	ctx := context.Background()

	// In Lambda, LoadDefaultConfig picks up the execution role credentials
	// automatically via the runtime's credential provider chain.
	// No endpoint override is needed — this binary is cloud-only.
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("load aws config: %v", err)
	}

	dynamoClient = dynamodb.NewFromConfig(cfg)
	s3Client = s3.NewFromConfig(cfg)

	// TABLE_NAME lets us point dev and prod Lambda functions at different tables
	// without recompiling. Falls back to the constant used by the local CLI.
	tableName = os.Getenv("TABLE_NAME")
	if tableName == "" {
		tableName = storage.TableName
	}
}

func main() {
	lambda.Start(handler)
}

// handler processes one S3 event, which may contain multiple records (e.g. if
// several CSVs were uploaded in quick succession). Records are processed
// sequentially — DynamoDB BatchWriteItem throughput is the bottleneck, not CPU.
func handler(ctx context.Context, event events.S3Event) error {
	for _, record := range event.Records {
		bucket := record.S3.Bucket.Name

		// S3 encodes object keys with URL percent-encoding (e.g. spaces become "+").
		// Decode before any string operations so prefix extraction works correctly.
		key, err := url.QueryUnescape(record.S3.Object.Key)
		if err != nil {
			return fmt.Errorf("unescape key %q: %w", record.S3.Object.Key, err)
		}

		if err := processFile(ctx, bucket, key); err != nil {
			// Return the error so Lambda marks this invocation as failed.
			// Lambda will retry (up to the event source's retry policy) and
			// the item will land in a dead-letter queue if configured.
			return fmt.Errorf("process s3://%s/%s: %w", bucket, key, err)
		}
	}
	return nil
}

// processFile downloads the object at s3://bucket/key, derives the CSV type
// from the key prefix, and routes it to the appropriate parser+storage pipeline.
func processFile(ctx context.Context, bucket, key string) error {
	// The prefix before the first "/" is the CSV type selector.
	// e.g. "offense-player/weekly_player_stats_offense.csv" → "offense-player"
	csvType := strings.SplitN(key, "/", 2)[0]

	resp, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	})
	if err != nil {
		return fmt.Errorf("get object: %w", err)
	}
	defer resp.Body.Close()

	// 4 MB read buffer amortises syscall cost on large files (same as CLI ingestor).
	r := bufio.NewReaderSize(resp.Body, 4*1024*1024)

	start := time.Now()
	var rows int

	switch csvType {
	case "offense-player":
		rows, err = doPlayerOffense(ctx, r, false)
	case "offense-player-yr":
		rows, err = doPlayerOffense(ctx, r, true)
	case "defense-player":
		rows, err = doPlayerDefense(ctx, r, false)
	case "defense-player-yr":
		rows, err = doPlayerDefense(ctx, r, true)
	case "team-offense":
		rows, err = doTeamOffense(ctx, r, false)
	case "team-offense-yr":
		rows, err = doTeamOffense(ctx, r, true)
	case "team-defense":
		rows, err = doTeamDefense(ctx, r, false)
	case "team-defense-yr":
		rows, err = doTeamDefense(ctx, r, true)
	default:
		return fmt.Errorf("unknown csv type from key prefix %q (key: %q)", csvType, key)
	}

	if err != nil {
		return err
	}

	log.Printf("ingested %d rows from s3://%s/%s in %s", rows, bucket, key, time.Since(start).Round(time.Millisecond))
	return nil
}

// The do* functions below mirror those in cmd/ingestor/main.go but accept
// the module-level tableName variable instead of the storage.TableName constant.
// This lets the Lambda function target a different table than the local CLI
// via the TABLE_NAME environment variable without touching internal packages.

func doPlayerOffense(ctx context.Context, r interface{ Read([]byte) (int, error) }, isYearly bool) (int, error) {
	w := storage.NewBatchWriter(dynamoClient, tableName)
	var n int

	err := parser.ParsePlayerOffense(r, isYearly,
		func(p models.Player) error { return w.MergePlayerMetadata(ctx, p) },
		func(s models.PlayerOffenseStat) error {
			n++
			return w.Put(ctx, s)
		},
	)
	if err != nil {
		return n, err
	}
	return n, w.Flush(ctx)
}

func doPlayerDefense(ctx context.Context, r interface{ Read([]byte) (int, error) }, isYearly bool) (int, error) {
	w := storage.NewBatchWriter(dynamoClient, tableName)
	var n int

	err := parser.ParsePlayerDefense(r, isYearly,
		func(p models.Player) error { return w.MergePlayerMetadata(ctx, p) },
		func(s models.PlayerDefenseStat) error {
			n++
			return w.Put(ctx, s)
		},
	)
	if err != nil {
		return n, err
	}
	return n, w.Flush(ctx)
}

func doTeamOffense(ctx context.Context, r interface{ Read([]byte) (int, error) }, isYearly bool) (int, error) {
	w := storage.NewBatchWriter(dynamoClient, tableName)
	var n int

	err := parser.ParseTeamOffense(r, isYearly, func(s models.TeamOffenseStat) error {
		n++
		return w.Put(ctx, s)
	})
	if err != nil {
		return n, err
	}
	return n, w.Flush(ctx)
}

func doTeamDefense(ctx context.Context, r interface{ Read([]byte) (int, error) }, isYearly bool) (int, error) {
	w := storage.NewBatchWriter(dynamoClient, tableName)
	var n int

	err := parser.ParseTeamDefense(r, isYearly, func(s models.TeamDefenseStat) error {
		n++
		return w.Put(ctx, s)
	})
	if err != nil {
		return n, err
	}
	return n, w.Flush(ctx)
}
