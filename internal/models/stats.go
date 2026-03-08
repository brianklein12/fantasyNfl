package models

// PlayerOffenseStat is one row of offensive stats for a player in the single table.
//
//	pk      = "PLAYER#<player_id>"
//	sk      = "STAT#offense_weekly#<season>#<week>"              (weekly)
//	        = "STAT#offense_yearly#<season>#<season_type>#0"      (yearly)
//	gsi1pk  = "POSITION#<position>"  — enables "all WRs in season X" queries
//	gsi1sk  = "SEASON#<season>#WEEK#<week>"                      (weekly)
//	        = "SEASON#<season>#<season_type>#WEEK#0"              (yearly)
//
// SK component order (type → season → season_type → week) allows prefix queries:
//
//	begins_with("STAT#offense_yearly#2024")        → all 2024 yearly rows (REG + POST)
//	begins_with("STAT#offense_yearly#2024#REG")    → 2024 regular season only
//
// season_type is included in the yearly SK because the source data provides both
// REG and POST totals for the same player+season; without it they would collide.
type PlayerOffenseStat struct {
	PK         string `dynamodbav:"pk"`
	SK         string `dynamodbav:"sk"`
	EntityType string `dynamodbav:"entity_type"`

	// GSI1 fields — sparse: only player stat rows set these.
	// Team rows and player metadata rows omit them, keeping the GSI lean.
	GSI1PK string `dynamodbav:"gsi1pk"`
	GSI1SK string `dynamodbav:"gsi1sk"`

	// Domain fields stored as regular attributes.
	PlayerID   string `dynamodbav:"player_id"`
	Season     string `dynamodbav:"season"`
	Week       string `dynamodbav:"week"`
	SeasonType string `dynamodbav:"season_type"`
	Team       string `dynamodbav:"team"`
	Position   string `dynamodbav:"position"`

	// Passing
	PassAttempts float64 `dynamodbav:"pass_attempts,omitempty"`
	CompletePass float64 `dynamodbav:"complete_pass,omitempty"`
	PassingYards float64 `dynamodbav:"passing_yards,omitempty"`
	PassTD       float64 `dynamodbav:"pass_touchdown,omitempty"`
	PasserRating float64 `dynamodbav:"passer_rating,omitempty"`
	Interception float64 `dynamodbav:"interception,omitempty"`

	// Rushing
	RushAttempts float64 `dynamodbav:"rush_attempts,omitempty"`
	RushingYards float64 `dynamodbav:"rushing_yards,omitempty"`
	RushTD       float64 `dynamodbav:"rush_touchdown,omitempty"`

	// Receiving
	Receptions     float64 `dynamodbav:"receptions,omitempty"`
	Targets        float64 `dynamodbav:"targets,omitempty"`
	ReceivingYards float64 `dynamodbav:"receiving_yards,omitempty"`
	ReceivingTD    float64 `dynamodbav:"receiving_touchdown,omitempty"`

	// Misc
	Fumble           float64 `dynamodbav:"fumble,omitempty"`
	FumbleLost       float64 `dynamodbav:"fumble_lost,omitempty"`
	FantasyPointsPPR float64 `dynamodbav:"fantasy_points_ppr,omitempty"`
	FantasyPointsStd float64 `dynamodbav:"fantasy_points_standard,omitempty"`
}

// PlayerDefenseStat is one row of defensive stats for a player in the single table.
//
//	pk      = "PLAYER#<player_id>"
//	sk      = "STAT#defense_weekly#<season>#<week>"             (weekly)
//	        = "STAT#defense_yearly#<season>#<season_type>#0"     (yearly)
//	gsi1pk  = "POSITION#<position>"
//	gsi1sk  = "SEASON#<season>#WEEK#<week>"                     (weekly)
//	        = "SEASON#<season>#<season_type>#WEEK#0"             (yearly)
type PlayerDefenseStat struct {
	PK         string `dynamodbav:"pk"`
	SK         string `dynamodbav:"sk"`
	EntityType string `dynamodbav:"entity_type"`

	GSI1PK string `dynamodbav:"gsi1pk"`
	GSI1SK string `dynamodbav:"gsi1sk"`

	PlayerID   string `dynamodbav:"player_id"`
	Season     string `dynamodbav:"season"`
	Week       string `dynamodbav:"week"`
	SeasonType string `dynamodbav:"season_type"`
	Team       string `dynamodbav:"team"`
	Position   string `dynamodbav:"position"`

	SoloTackle       float64 `dynamodbav:"solo_tackle,omitempty"`
	AssistTackle     float64 `dynamodbav:"assist_tackle,omitempty"`
	TackleWithAssist float64 `dynamodbav:"tackle_with_assist,omitempty"`
	Sack             float64 `dynamodbav:"sack,omitempty"`
	QBHit            float64 `dynamodbav:"qb_hit,omitempty"`
	Interception     float64 `dynamodbav:"interception,omitempty"`
	DefTouchdown     float64 `dynamodbav:"def_touchdown,omitempty"`
	FumbleForced     float64 `dynamodbav:"fumble_forced,omitempty"`
	Safety           float64 `dynamodbav:"safety,omitempty"`

	FantasyPointsPPR float64 `dynamodbav:"fantasy_points_ppr,omitempty"`
	FantasyPointsStd float64 `dynamodbav:"fantasy_points_standard,omitempty"`
}

// TeamOffenseStat is one row of team offensive stats in the single table.
//
//	pk  = "TEAM#<team>"
//	sk  = "STAT#offense_weekly#<season>#<week>"             (weekly)
//	    = "STAT#offense_yearly#<season>#<season_type>#0"     (yearly)
//
// No GSI fields — team rows do not participate in the position-based index.
// This is what makes the index "sparse": only items with gsi1pk/gsi1sk set are indexed.
type TeamOffenseStat struct {
	PK         string `dynamodbav:"pk"`
	SK         string `dynamodbav:"sk"`
	EntityType string `dynamodbav:"entity_type"`

	Team       string `dynamodbav:"team"`
	GameID     string `dynamodbav:"game_id,omitempty"`
	Season     string `dynamodbav:"season"`
	Week       string `dynamodbav:"week"`
	SeasonType string `dynamodbav:"season_type"`

	TotalOffYards  float64 `dynamodbav:"total_off_yards,omitempty"`
	PassAttempts   float64 `dynamodbav:"pass_attempts,omitempty"`
	CompletePass   float64 `dynamodbav:"complete_pass,omitempty"`
	PassingYards   float64 `dynamodbav:"passing_yards,omitempty"`
	RushAttempts   float64 `dynamodbav:"rush_attempts,omitempty"`
	RushingYards   float64 `dynamodbav:"rushing_yards,omitempty"`
	RushTD         float64 `dynamodbav:"rush_touchdown,omitempty"`
	PassTD         float64 `dynamodbav:"pass_touchdown,omitempty"`
	Receptions     float64 `dynamodbav:"receptions,omitempty"`
	Targets        float64 `dynamodbav:"targets,omitempty"`
	ReceivingYards float64 `dynamodbav:"receiving_yards,omitempty"`
	ReceivingTD    float64 `dynamodbav:"receiving_touchdown,omitempty"`
	Interception   float64 `dynamodbav:"interception,omitempty"`
	Fumble         float64 `dynamodbav:"fumble,omitempty"`
	TotalOffPoints float64 `dynamodbav:"total_off_points,omitempty"`
	TotalDefPoints float64 `dynamodbav:"total_def_points,omitempty"`
	Win            float64 `dynamodbav:"win,omitempty"`
	Loss           float64 `dynamodbav:"loss,omitempty"`
	Tie            float64 `dynamodbav:"tie,omitempty"`
}

// TeamDefenseStat is one row of team defensive stats in the single table.
//
//	pk  = "TEAM#<team>"
//	sk  = "STAT#defense_weekly#<season>#<week>"             (weekly)
//	    = "STAT#defense_yearly#<season>#<season_type>#0"     (yearly)
type TeamDefenseStat struct {
	PK         string `dynamodbav:"pk"`
	SK         string `dynamodbav:"sk"`
	EntityType string `dynamodbav:"entity_type"`

	Team       string `dynamodbav:"team"`
	GameID     string `dynamodbav:"game_id,omitempty"`
	Season     string `dynamodbav:"season"`
	Week       string `dynamodbav:"week"`
	SeasonType string `dynamodbav:"season_type"`

	SoloTackle       float64 `dynamodbav:"solo_tackle,omitempty"`
	AssistTackle     float64 `dynamodbav:"assist_tackle,omitempty"`
	TackleWithAssist float64 `dynamodbav:"tackle_with_assist,omitempty"`
	Sack             float64 `dynamodbav:"sack,omitempty"`
	QBHit            float64 `dynamodbav:"qb_hit,omitempty"`
	Interception     float64 `dynamodbav:"interception,omitempty"`
	DefTouchdown     float64 `dynamodbav:"def_touchdown,omitempty"`
	FumbleForced     float64 `dynamodbav:"fumble_forced,omitempty"`
	Safety           float64 `dynamodbav:"safety,omitempty"`
	TotalOffPoints   float64 `dynamodbav:"total_off_points,omitempty"`
	TotalDefPoints   float64 `dynamodbav:"total_def_points,omitempty"`
	Win              float64 `dynamodbav:"win,omitempty"`
	Loss             float64 `dynamodbav:"loss,omitempty"`
	Tie              float64 `dynamodbav:"tie,omitempty"`
}
