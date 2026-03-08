package models

// Player represents a unique NFL player's identity and biographical info.
// Stored in the single FantasyNFL table as a metadata item:
//
//	pk = "PLAYER#<player_id>"
//	sk = "#METADATA"
//
// The "#" prefix on the sort key sorts before any letter in ASCII, so this
// item is always the first returned when querying a player's item collection.
// That means one Query call can fetch bio + stats together in order.
type Player struct {
	// Composite key fields — these become the DynamoDB PK and SK.
	PK string `dynamodbav:"pk"`
	SK string `dynamodbav:"sk"`

	// EntityType is a discriminator stored on every item so that a raw scan
	// of the table immediately tells you what each item represents.
	EntityType string `dynamodbav:"entity_type"`

	// PlayerID is stored as a regular attribute (not the key) so that it
	// is directly readable without parsing the PK prefix.
	PlayerID   string `dynamodbav:"player_id"`
	Name       string `dynamodbav:"player_name"`
	Position   string `dynamodbav:"position"`
	BirthYear  string `dynamodbav:"birth_year,omitempty"`
	Height     string `dynamodbav:"height,omitempty"`
	Weight     string `dynamodbav:"weight,omitempty"`
	College    string `dynamodbav:"college,omitempty"`
	DraftYear  string `dynamodbav:"draft_year,omitempty"`
	DraftRound string `dynamodbav:"draft_round,omitempty"`
	DraftPick  string `dynamodbav:"draft_pick,omitempty"`
	DraftOvr   string `dynamodbav:"draft_ovr,omitempty"`
}
