package storage

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// TableName is the single DynamoDB table that holds all entity types.
const TableName = "FantasyNFL"

// CreateTables creates the single FantasyNFL table if it does not already exist.
// Safe to call multiple times — existing tables are skipped without error.
//
// Table design:
//
//	PK  (partition key) — prefixed entity key, e.g. "PLAYER#abc123" or "TEAM#KC"
//	SK  (sort key)      — item discriminator, e.g. "#METADATA" or "STAT#offense_weekly#2023#14"
//
// GSI1 (gsi1pk / gsi1sk):
//
//	Sparse index — only player stat rows set these attributes.
//	Enables queries like "all WR stats in 2023" without scanning the full table.
//	gsi1pk = "POSITION#WR"
//	gsi1sk = "SEASON#2023#WEEK#14"
func CreateTables(ctx context.Context, client *dynamodb.Client) error {
	return ensureTable(ctx, client, &dynamodb.CreateTableInput{
		TableName: aws.String(TableName),

		// Only attributes used as key components are declared here.
		// All other attributes (player_name, passing_yards, etc.) are schemaless
		// and do not appear in this definition — that is intentional DynamoDB design.
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("pk"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("sk"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("gsi1pk"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("gsi1sk"), AttributeType: types.ScalarAttributeTypeS},
		},

		// Primary key: pk (hash) + sk (range).
		// Items sharing a pk form an "item collection" — physically co-located
		// in DynamoDB and sorted by sk. This makes range queries over a player's
		// full history a single, fast call.
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String("pk"), KeyType: types.KeyTypeHash},
			{AttributeName: aws.String("sk"), KeyType: types.KeyTypeRange},
		},

		GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{
			{
				IndexName: aws.String("gsi1"),
				KeySchema: []types.KeySchemaElement{
					{AttributeName: aws.String("gsi1pk"), KeyType: types.KeyTypeHash},
					{AttributeName: aws.String("gsi1sk"), KeyType: types.KeyTypeRange},
				},
				// ProjectionTypeAll copies every attribute into the GSI.
				// For a read-heavy fantasy app this trades storage cost for
				// avoiding extra fetches back to the main table.
				Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll},
			},
		},

		// PayPerRequest: no capacity planning required. Right for dev and early prod.
		// Switch to PROVISIONED once you have stable, high-volume traffic patterns.
		BillingMode: types.BillingModePayPerRequest,
	})
}

// ensureTable creates the table and ignores ResourceInUseException (already exists).
func ensureTable(ctx context.Context, client *dynamodb.Client, input *dynamodb.CreateTableInput) error {
	_, err := client.CreateTable(ctx, input)
	if err != nil {
		var resEx *types.ResourceInUseException
		if isError(err, &resEx) {
			fmt.Printf("table %s already exists, skipping\n", *input.TableName)
			return nil
		}
		return fmt.Errorf("create table %s: %w", *input.TableName, err)
	}
	fmt.Printf("created table %s\n", *input.TableName)
	return nil
}

// isError checks whether err (or anything it wraps) is of type T.
func isError[T error](err error, target *T) bool {
	if err == nil {
		return false
	}
	type unwrapper interface{ Unwrap() error }
	for {
		if e, ok := err.(T); ok {
			*target = e
			return true
		}
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
}
