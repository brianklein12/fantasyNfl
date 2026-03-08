package storage

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/brianklein12/fantasy-football/internal/models"
)

const (
	batchSize = 25 // DynamoDB BatchWriteItem hard maximum

	// Retry / backoff constants for UnprocessedItems handling.
	// DynamoDB returns HTTP 200 even for throttled items; they appear in
	// UnprocessedItems rather than as an error. We retry those items with
	// exponential backoff so the service has time to recover capacity.
	maxRetryAttempts = 8                    // up to ~25 s of cumulative wait
	baseRetryDelay   = 100 * time.Millisecond
	maxRetryDelay    = 30 * time.Second
)

// NewClient creates a DynamoDB client. When DYNAMODB_ENDPOINT is set in the
// environment (e.g. http://localhost:8000) it connects to a local instance,
// otherwise it uses the default AWS credential chain for production.
func NewClient(ctx context.Context, endpoint string) (*dynamodb.Client, error) {
	if endpoint != "" {
		// Local / docker-compose mode: override endpoint and use dummy credentials.
		cfg, err := config.LoadDefaultConfig(ctx,
			config.WithRegion("us-east-1"),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("local", "local", "")),
		)
		if err != nil {
			return nil, fmt.Errorf("load local aws config: %w", err)
		}
		client := dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
			o.BaseEndpoint = aws.String(endpoint)
		})
		return client, nil
	}

	// Production: use the standard AWS credential chain (env vars, ~/.aws, IAM role).
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	return dynamodb.NewFromConfig(cfg), nil
}

// BatchWriter accumulates items and flushes to DynamoDB in batches of 25.
type BatchWriter struct {
	client    *dynamodb.Client
	tableName string
	buf       []types.WriteRequest
	// itemIndex maps "pk\x00sk" → buf index so that Put can overwrite an
	// earlier entry when the same composite key appears again in the same
	// batch (last-write-wins). DynamoDB's BatchWriteItem returns a
	// ValidationException if a batch contains two PutRequests with the same
	// key, so deduplication here keeps the storage layer transparent to
	// callers that may produce duplicate rows (e.g. rolling-snapshot CSVs).
	itemIndex map[string]int
}

// NewBatchWriter returns a BatchWriter targeting the given table.
func NewBatchWriter(client *dynamodb.Client, tableName string) *BatchWriter {
	return &BatchWriter{
		client:    client,
		tableName: tableName,
		itemIndex: make(map[string]int),
	}
}

// Put marshals item into a DynamoDB AttributeValue map and adds it to the
// buffer. When the buffer reaches 25 items it is flushed automatically.
//
// If the buffer already holds an item with the same pk+sk, the new item
// replaces it (last-write-wins). This prevents the ValidationException that
// DynamoDB raises when a batch contains duplicate keys, and ensures the final
// snapshot row from rolling-update source data is the one written.
func (b *BatchWriter) Put(ctx context.Context, item any) error {
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("marshal item: %w", err)
	}

	// Build a dedup key from the composite primary key.
	// "\x00" is a separator that cannot appear in DynamoDB string values.
	key := attrStr(av, "pk") + "\x00" + attrStr(av, "sk")
	req := types.WriteRequest{PutRequest: &types.PutRequest{Item: av}}

	if idx, exists := b.itemIndex[key]; exists {
		// Same pk+sk seen before in this batch — replace in place so the
		// buffer length (and therefore batch size) stays the same.
		b.buf[idx] = req
	} else {
		b.itemIndex[key] = len(b.buf)
		b.buf = append(b.buf, req)
	}

	if len(b.buf) >= batchSize {
		return b.Flush(ctx)
	}
	return nil
}

// MergePlayerMetadata writes a player's #METADATA item using per-field
// if_not_exists semantics via UpdateItem:
//   - If the item doesn't exist yet, it is created with every non-zero field set.
//   - If the item already exists, only fields that are currently absent in
//     DynamoDB are filled in — existing values are never overwritten.
//
// This lets multiple CSV files contribute to a player's bio without any one
// file clobbering data that another file already wrote. For example, the weekly
// CSV might carry height/weight while the yearly CSV might not; running both
// in either order will produce the richest possible merged result.
//
// Fields that carry sentinel "missing data" values ("", "0", "0.0", "N/A")
// are silently skipped — they are not written to DynamoDB.
//
// Unlike Put, this method bypasses the batch buffer and issues an individual
// UpdateItem call. BatchWriteItem does not support condition expressions, so
// per-field if_not_exists is not possible through batch writes.
func (b *BatchWriter) MergePlayerMetadata(ctx context.Context, p models.Player) error {
	type bioField struct {
		attr  string
		value string
	}
	fields := []bioField{
		{"entity_type", p.EntityType},
		{"player_id", p.PlayerID},
		{"player_name", p.Name},
		{"position", p.Position},
		{"birth_year", p.BirthYear},
		{"height", p.Height},
		{"weight", p.Weight},
		{"college", p.College},
		{"draft_year", p.DraftYear},
		{"draft_round", p.DraftRound},
		{"draft_pick", p.DraftPick},
		{"draft_ovr", p.DraftOvr},
	}

	exprNames := map[string]string{}
	exprValues := map[string]types.AttributeValue{}
	var setParts []string

	for i, f := range fields {
		if !meaningfulBioValue(f.value) {
			continue
		}
		n := fmt.Sprintf("#n%d", i)
		v := fmt.Sprintf(":v%d", i)
		exprNames[n] = f.attr
		exprValues[v] = &types.AttributeValueMemberS{Value: f.value}
		// if_not_exists(field, :val) returns the existing value when the field
		// is already set, and :val when it is absent — so SET field = if_not_exists(...)
		// only writes the field on the first meaningful value seen across all ingests.
		setParts = append(setParts, fmt.Sprintf("%s = if_not_exists(%s, %s)", n, n, v))
	}

	if len(setParts) == 0 {
		return nil // nothing meaningful to write
	}

	_, err := b.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(b.tableName),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: p.PK},
			"sk": &types.AttributeValueMemberS{Value: p.SK},
		},
		UpdateExpression:          aws.String("SET " + strings.Join(setParts, ", ")),
		ExpressionAttributeNames:  exprNames,
		ExpressionAttributeValues: exprValues,
	})
	if err != nil {
		return fmt.Errorf("merge player metadata %s: %w", p.PK, err)
	}
	return nil
}

// meaningfulBioValue reports whether s is a non-empty, non-zero bio value worth
// writing to DynamoDB. Source CSVs use "", "0", "0.0", and "N/A" as sentinels
// for unknown/missing biographical data.
func meaningfulBioValue(s string) bool {
	switch s {
	case "", "0", "0.0", "N/A":
		return false
	}
	return true
}

// attrStr extracts a string value from a DynamoDB AttributeValue map.
// Returns "" when the key is absent or the value is not a String type.
func attrStr(av map[string]types.AttributeValue, key string) string {
	v, ok := av[key]
	if !ok {
		return ""
	}
	s, ok := v.(*types.AttributeValueMemberS)
	if !ok {
		return ""
	}
	return s.Value
}

// Flush sends all buffered items to DynamoDB and clears the buffer.
// Call this after processing the last row of a CSV file.
//
// DynamoDB's BatchWriteItem never returns an HTTP error for throttling.
// Instead it returns HTTP 200 with UnprocessedItems containing the items
// that were rejected due to insufficient capacity. This method retries
// those items with exponential backoff + jitter until all are written
// or maxRetryAttempts is reached.
//
// Backoff schedule (base * 2^attempt, capped at maxRetryDelay, ±25% jitter):
//
//	attempt 1 → ~200 ms   attempt 5 → ~3.2 s
//	attempt 2 → ~400 ms   attempt 6 → ~6.4 s
//	attempt 3 → ~800 ms   attempt 7 → ~12.8 s
//	attempt 4 → ~1.6 s    attempt 8 → error
//
// Jitter prevents thundering herd: if two ingestor processes throttle at
// the same time, randomised retry windows stop them retrying in lockstep.
func (b *BatchWriter) Flush(ctx context.Context) error {
	if len(b.buf) == 0 {
		return nil
	}

	// Move items out of the buffer immediately so the buffer is always empty
	// after Flush returns, even if we fail partway through retries.
	// Reset itemIndex at the same time so the next batch starts fresh.
	pending := b.buf
	b.buf = b.buf[:0]
	b.itemIndex = make(map[string]int)

	for attempt := 0; len(pending) > 0; attempt++ {
		if attempt >= maxRetryAttempts {
			return fmt.Errorf("batch write to %s: %d items still unprocessed after %d attempts (persistent throttle)",
				b.tableName, len(pending), maxRetryAttempts)
		}

		// Wait before every retry (not before the first attempt).
		if attempt > 0 {
			// Base delay doubles each attempt: 200ms, 400ms, 800ms, …
			base := min(baseRetryDelay*(1<<attempt), maxRetryDelay)
			// ±25% jitter: shift the delay by a random fraction of itself.
			// This staggers retries from concurrent processes so they don't
			// all slam DynamoDB again at the same instant.
			jitter := time.Duration(rand.Int63n(int64(base) / 2))
			if rand.Intn(2) == 0 {
				jitter = -jitter
			}
			delay := base + jitter

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		resp, err := b.client.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{
			RequestItems: map[string][]types.WriteRequest{
				b.tableName: pending,
			},
		})
		if err != nil {
			return fmt.Errorf("batch write to %s: %w", b.tableName, err)
		}

		// UnprocessedItems contains only items DynamoDB could not write due to
		// throttling or temporary capacity limits. An empty map means success.
		pending = resp.UnprocessedItems[b.tableName]
	}

	return nil
}
