package parser

import (
	"fmt"
	"io"

	"github.com/brianklein12/fantasy-football/internal/models"
)

// ParsePlayerDefense reads a weekly or yearly player defense CSV and calls
// onPlayer for each unique player and onStat for each stat row.
//
// isYearly controls the SK format:
//   - weekly: STAT#defense_weekly#<season>#<week>
//   - yearly: STAT#defense_yearly#<season>#<season_type>#0
//
// Including season_type in the yearly SK is required because the source data
// contains both REG and POST rows for the same player+season, which would
// otherwise collide on a single DynamoDB item.
//
// Within-batch duplicate keys are resolved by the BatchWriter (last-write-wins).
func ParsePlayerDefense(
	r io.Reader,
	isYearly bool,
	onPlayer func(models.Player) error,
	onStat func(models.PlayerDefenseStat) error,
) error {
	cr := newCSVReader(r)

	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("read header: %w", err)
	}
	idx := colIndex(header)

	statType := "defense_weekly"
	if isYearly {
		statType = "defense_yearly"
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

		// Skip rows where the player has no recognized defensive position.
		// Offensive players who appear in the defense CSV (e.g. an RB who
		// made a tackle) carry position "N/A" — they are not fantasy-
		// relevant defensive players and should not be ingested here.
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

		stat := models.PlayerDefenseStat{
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

			SoloTackle:       num(row, idx, "solo_tackle"),
			AssistTackle:     num(row, idx, "assist_tackle"),
			TackleWithAssist: num(row, idx, "tackle_with_assist"),
			Sack:             num(row, idx, "sack"),
			QBHit:            num(row, idx, "qb_hit"),
			Interception:     num(row, idx, "interception"),
			DefTouchdown:     num(row, idx, "def_touchdown"),
			FumbleForced:     num(row, idx, "fumble_forced"),
			Safety:           num(row, idx, "safety"),

			FantasyPointsPPR: num(row, idx, "fantasy_points_ppr"),
			FantasyPointsStd: num(row, idx, "fantasy_points_standard"),
		}

		if err := onStat(stat); err != nil {
			return err
		}
	}
	return nil
}
