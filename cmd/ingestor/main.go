// Command ingestor reads NFL stats CSV files and writes them into the single
// FantasyNFL DynamoDB table.
//
// Usage:
//
//	ingestor [-create-tables] <csv-type> <file>
//
// CSV types:
//
//	offense-player     weekly player offense stats
//	defense-player     weekly player defense stats
//	team-offense       weekly team offense stats
//	team-defense       weekly team defense stats
//	offense-player-yr  yearly player offense stats
//	defense-player-yr  yearly player defense stats
//	team-offense-yr    yearly team offense stats
//	team-defense-yr    yearly team defense stats
//
// Environment variables:
//
//	DYNAMODB_ENDPOINT   local DynamoDB URL (e.g. http://localhost:8000).
//	                    Omit to use the standard AWS credential chain.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/brianklein12/fantasy-football/internal/models"
	"github.com/brianklein12/fantasy-football/internal/parser"
	"github.com/brianklein12/fantasy-football/internal/storage"
)

func main() {
	createTables := flag.Bool("create-tables", false, "Create the FantasyNFL table before ingesting")
	flag.Parse()

	args := flag.Args()
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: ingestor [-create-tables] <csv-type> <file>")
		fmt.Fprintln(os.Stderr, "csv-types: offense-player, defense-player, team-offense, team-defense,")
		fmt.Fprintln(os.Stderr, "           offense-player-yr, defense-player-yr, team-offense-yr, team-defense-yr")
		os.Exit(1)
	}
	csvType := args[0]
	csvFile := args[1]

	ctx := context.Background()

	endpoint := os.Getenv("DYNAMODB_ENDPOINT")
	client, err := storage.NewClient(ctx, endpoint)
	if err != nil {
		log.Fatalf("dynamodb client: %v", err)
	}

	if *createTables {
		if err := storage.CreateTables(ctx, client); err != nil {
			log.Fatalf("create tables: %v", err)
		}

		if csvFile == "/dev/null" {
			fmt.Println("Tables created successfully. Exiting.")
			os.Exit(0)
		}
	}

	f, err := os.Open(csvFile)
	if err != nil {
		log.Fatalf("open %s: %v", csvFile, err)
	}
	defer f.Close()

	// 4 MB read buffer amortizes syscall cost on large files.
	r := bufio.NewReaderSize(f, 4*1024*1024)

	start := time.Now()
	var rows int

	switch csvType {
	case "offense-player":
		rows, err = doPlayerOffense(ctx, client, r, false)
	case "offense-player-yr":
		rows, err = doPlayerOffense(ctx, client, r, true)
	case "defense-player":
		rows, err = doPlayerDefense(ctx, client, r, false)
	case "defense-player-yr":
		rows, err = doPlayerDefense(ctx, client, r, true)
	case "team-offense":
		rows, err = doTeamOffense(ctx, client, r, false)
	case "team-offense-yr":
		rows, err = doTeamOffense(ctx, client, r, true)
	case "team-defense":
		rows, err = doTeamDefense(ctx, client, r, false)
	case "team-defense-yr":
		rows, err = doTeamDefense(ctx, client, r, true)
	default:
		log.Fatalf("unknown csv-type %q", csvType)
	}

	if err != nil {
		log.Fatalf("ingest %s: %v", csvFile, err)
	}

	fmt.Printf("ingested %d rows from %s in %s\n", rows, csvFile, time.Since(start).Round(time.Millisecond))
}

// doPlayerOffense ingests a player offense CSV into the single FantasyNFL table.
// Player metadata and stat rows are written to the same table — the key scheme
// (PK = "PLAYER#<id>", SK = "#METADATA" vs "STAT#...") distinguishes them.
func doPlayerOffense(ctx context.Context, client *dynamodb.Client, r interface{ Read([]byte) (int, error) }, isYearly bool) (int, error) {
	w := storage.NewBatchWriter(client, storage.TableName)
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

// doPlayerDefense ingests a player defense CSV into the single FantasyNFL table.
func doPlayerDefense(ctx context.Context, client *dynamodb.Client, r interface{ Read([]byte) (int, error) }, isYearly bool) (int, error) {
	w := storage.NewBatchWriter(client, storage.TableName)
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

// doTeamOffense ingests a team offense CSV into the single FantasyNFL table.
func doTeamOffense(ctx context.Context, client *dynamodb.Client, r interface{ Read([]byte) (int, error) }, isYearly bool) (int, error) {
	w := storage.NewBatchWriter(client, storage.TableName)
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

// doTeamDefense ingests a team defense CSV into the single FantasyNFL table.
func doTeamDefense(ctx context.Context, client *dynamodb.Client, r interface{ Read([]byte) (int, error) }, isYearly bool) (int, error) {
	w := storage.NewBatchWriter(client, storage.TableName)
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
