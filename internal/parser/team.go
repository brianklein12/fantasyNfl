package parser

import (
	"fmt"
	"io"

	"github.com/brianklein12/fantasy-football/internal/models"
)

// ParseTeamOffense reads a weekly or yearly team offense CSV and calls onStat
// for each row.
//
// Team rows use PK = "TEAM#<team>" and do not set GSI1 fields, keeping them
// out of the position-based player index.
//
// isYearly controls the SK format:
//   - weekly: STAT#offense_weekly#<season>#<week>
//   - yearly: STAT#offense_yearly#<season>#<season_type>#0
//
// Including season_type in the yearly SK separates REG and POST totals, which
// the source data provides as distinct rows for the same team+season.
//
// Within-batch duplicate keys are resolved by the BatchWriter (last-write-wins).
func ParseTeamOffense(
	r io.Reader,
	isYearly bool,
	onStat func(models.TeamOffenseStat) error,
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

	for lineNum := 2; ; lineNum++ {
		row, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read row %d: %w", lineNum, err)
		}

		team := field(row, idx, "team")
		if team == "" {
			continue
		}

		season := field(row, idx, "season")
		week := field(row, idx, "week")
		seasonType := field(row, idx, "season_type")

		pk := "TEAM#" + team
		var sk string
		if isYearly {
			// season_type (REG/POST) is embedded so regular-season and postseason
			// totals for the same team+season land on separate DynamoDB items.
			week = "0"
			sk = fmt.Sprintf("STAT#%s#%s#%s#%s", statType, season, seasonType, week)
		} else {
			if week == "" {
				week = "0"
			}
			sk = fmt.Sprintf("STAT#%s#%s#%s", statType, season, week)
		}

		stat := models.TeamOffenseStat{
			PK:             pk,
			SK:             sk,
			EntityType:     "TEAM_STAT",
			Team:           team,
			GameID:         field(row, idx, "game_id"),
			Season:         season,
			Week:           week,
			SeasonType:     seasonType,
			TotalOffYards:  num(row, idx, "total_off_yards"),
			PassAttempts:   num(row, idx, "pass_attempts"),
			CompletePass:   num(row, idx, "complete_pass"),
			PassingYards:   num(row, idx, "passing_yards"),
			RushAttempts:   num(row, idx, "rush_attempts"),
			RushingYards:   num(row, idx, "rushing_yards"),
			RushTD:         num(row, idx, "rush_touchdown"),
			PassTD:         num(row, idx, "pass_touchdown"),
			Receptions:     num(row, idx, "receptions"),
			Targets:        num(row, idx, "targets"),
			ReceivingYards: num(row, idx, "receiving_yards"),
			ReceivingTD:    num(row, idx, "receiving_touchdown"),
			Interception:   num(row, idx, "interception"),
			Fumble:         num(row, idx, "fumble"),
			TotalOffPoints: num(row, idx, "total_off_points"),
			TotalDefPoints: num(row, idx, "total_def_points"),
			Win:            num(row, idx, "win"),
			Loss:           num(row, idx, "loss"),
			Tie:            num(row, idx, "tie"),
		}

		if err := onStat(stat); err != nil {
			return err
		}
	}
	return nil
}

// ParseTeamDefense reads a weekly or yearly team defense CSV and calls onStat
// for each row.
//
// isYearly controls the SK format:
//   - weekly: STAT#defense_weekly#<season>#<week>
//   - yearly: STAT#defense_yearly#<season>#<season_type>#0
func ParseTeamDefense(
	r io.Reader,
	isYearly bool,
	onStat func(models.TeamDefenseStat) error,
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

	for lineNum := 2; ; lineNum++ {
		row, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read row %d: %w", lineNum, err)
		}

		team := field(row, idx, "team")
		if team == "" {
			continue
		}

		season := field(row, idx, "season")
		week := field(row, idx, "week")
		seasonType := field(row, idx, "season_type")

		pk := "TEAM#" + team
		var sk string
		if isYearly {
			week = "0"
			sk = fmt.Sprintf("STAT#%s#%s#%s#%s", statType, season, seasonType, week)
		} else {
			if week == "" {
				week = "0"
			}
			sk = fmt.Sprintf("STAT#%s#%s#%s", statType, season, week)
		}

		stat := models.TeamDefenseStat{
			PK:               pk,
			SK:               sk,
			EntityType:       "TEAM_STAT",
			Team:             team,
			GameID:           field(row, idx, "game_id"),
			Season:           season,
			Week:             week,
			SeasonType:       seasonType,
			SoloTackle:       num(row, idx, "solo_tackle"),
			AssistTackle:     num(row, idx, "assist_tackle"),
			TackleWithAssist: num(row, idx, "tackle_with_assist"),
			Sack:             num(row, idx, "sack"),
			QBHit:            num(row, idx, "qb_hit"),
			Interception:     num(row, idx, "interception"),
			DefTouchdown:     num(row, idx, "def_touchdown"),
			FumbleForced:     num(row, idx, "fumble_forced"),
			Safety:           num(row, idx, "safety"),
			TotalOffPoints:   num(row, idx, "total_off_points"),
			TotalDefPoints:   num(row, idx, "total_def_points"),
			Win:              num(row, idx, "win"),
			Loss:             num(row, idx, "loss"),
			Tie:              num(row, idx, "tie"),
		}

		if err := onStat(stat); err != nil {
			return err
		}
	}
	return nil
}
