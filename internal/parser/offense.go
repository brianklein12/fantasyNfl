package parser

import (
	"fmt"
	"io"

	"github.com/brianklein12/fantasy-football/internal/models"
)

// ParsePlayerOffense reads a weekly or yearly player offense CSV and calls
// onPlayer for each unique player encountered and onStat for each stat row.
// It deduplicates players by player_id — onPlayer is only called the first
// time a given player_id is seen.
//
// isYearly controls the SK format:
//   - weekly: STAT#offense_weekly#<season>#<week>
//   - yearly: STAT#offense_yearly#<season>#<season_type>#0
//
// Including season_type in the yearly SK is required because the source data
// contains both REG and POST rows for the same player+season, which would
// otherwise collide on a single DynamoDB item.
//
// Within-batch duplicate keys (e.g. rolling-snapshot rows in the weekly CSV)
// are resolved by the BatchWriter (last-write-wins), so no parser-level
// seenItems tracking is needed here.
func ParsePlayerOffense(
	r io.Reader,
	isYearly bool,
	onPlayer func(models.Player) error,
	onStat func(models.PlayerOffenseStat) error,
) error {
	cr := newCSVReader(r)

	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("read header: %w", err)
	}
	idx := colIndex(header)

	statType := "offense_weekly"
	if isYearly {
		statType = "offense_yearly"
	}

	seenPlayers := make(map[string]bool)

	for lineNum := 2; ; lineNum++ {
		row, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read row %d: %w", lineNum, err)
		}

		playerID := field(row, idx, "player_id")
		if playerID == "" {
			continue
		}

		// Skip rows where the player has no recognized offensive position.
		// Defensive players who appear in the offense CSV (e.g. a DB who
		// returned a fumble) carry position "N/A" — they are not fantasy-
		// relevant offensive players and should not be ingested here.
		position := field(row, idx, "position")
		if position == "" || position == "N/A" {
			continue
		}

		pk := "PLAYER#" + playerID

		// 1. Emit player metadata once per player_id.
		if !seenPlayers[playerID] {
			seenPlayers[playerID] = true
			if err := onPlayer(models.Player{
				PK:         pk,
				SK:         "#METADATA",
				EntityType: "PLAYER",
				PlayerID:   playerID,
				Name:       field(row, idx, "player_name"),
				Position:   field(row, idx, "position"),
				BirthYear:  field(row, idx, "birth_year"),
				Height:     field(row, idx, "height"),
				Weight:     field(row, idx, "weight"),
				College:    field(row, idx, "college"),
				DraftYear:  field(row, idx, "draft_year"),
				DraftRound: field(row, idx, "draft_round"),
				DraftPick:  field(row, idx, "draft_pick"),
				DraftOvr:   field(row, idx, "draft_ovr"),
			}); err != nil {
				return err
			}
		}

		// 2. Build stat sort key.
		season := field(row, idx, "season")
		week := field(row, idx, "week")
		seasonType := field(row, idx, "season_type")

		var statSK, gsi1SK string
		if isYearly {
			// Yearly rows have no meaningful week number; "0" is the sentinel.
			// season_type (REG/POST) is embedded so regular-season and
			// postseason totals land on separate DynamoDB items.
			week = "0"
			statSK = fmt.Sprintf("STAT#%s#%s#%s#%s", statType, season, seasonType, week)
			gsi1SK = fmt.Sprintf("SEASON#%s#%s#WEEK#%s", season, seasonType, week)
		} else {
			// Weekly rows: POST uses weeks 18–22, REG uses 1–17, so season_type
			// is implicit in the week number — no collision, no need to embed it.
			if week == "" {
				week = "0"
			}
			statSK = fmt.Sprintf("STAT#%s#%s#%s", statType, season, week)
			gsi1SK = fmt.Sprintf("SEASON#%s#WEEK#%s", season, week)
		}

		stat := models.PlayerOffenseStat{
			PK:         pk,
			SK:         statSK,
			EntityType: "PLAYER_STAT",
			GSI1PK:     "POSITION#" + position,
			GSI1SK:     gsi1SK,

			PlayerID:   playerID,
			Season:     season,
			Week:       week,
			SeasonType: seasonType,
			Team:       field(row, idx, "team"),
			Position:   position,

			PassAttempts: num(row, idx, "pass_attempts"),
			CompletePass: num(row, idx, "complete_pass"),
			PassingYards: num(row, idx, "passing_yards"),
			PassTD:       num(row, idx, "pass_touchdown"),
			PasserRating: num(row, idx, "passer_rating"),
			Interception: num(row, idx, "interception"),

			RushAttempts: num(row, idx, "rush_attempts"),
			RushingYards: num(row, idx, "rushing_yards"),
			RushTD:       num(row, idx, "rush_touchdown"),

			Receptions:     num(row, idx, "receptions"),
			Targets:        num(row, idx, "targets"),
			ReceivingYards: num(row, idx, "receiving_yards"),
			ReceivingTD:    num(row, idx, "receiving_touchdown"),

			Fumble:           num(row, idx, "fumble"),
			FumbleLost:       num(row, idx, "fumble_lost"),
			FantasyPointsPPR: num(row, idx, "fantasy_points_ppr"),
			FantasyPointsStd: num(row, idx, "fantasy_points_standard"),
		}

		if err := onStat(stat); err != nil {
			return err
		}
	}

	return nil
}
