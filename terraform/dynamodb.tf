# The single FantasyNFL application table.
#
# All entity types — player bios, player stats (offense + defense, weekly + yearly),
# and team stats — live in this one table. Single-table design keeps related data
# co-located on the same DynamoDB partition, enabling a player's bio + all their
# stats to come back in one Query call rather than multiple round-trips.
#
# PAY_PER_REQUEST means we pay per read/write, not for reserved capacity.
# Correct for a project with bursty, infrequent ingest runs and low read volume.
resource "aws_dynamodb_table" "fantasy_nfl" {
  name         = "FantasyNFL"
  billing_mode = "PAY_PER_REQUEST"

  # Generic key names (pk / sk) are intentional — a single table stores multiple
  # entity types, and a concrete name like "player_id" would be misleading on a
  # team stat row. The entity type is encoded in the key value instead.
  #
  # key_schema blocks replace the deprecated top-level hash_key / range_key
  # attributes. HASH = partition key, RANGE = sort key — same semantics, newer syntax.
  hash_key  = "pk"
  range_key = "sk"

  attribute {
    name = "pk"
    type = "S"
  }

  attribute {
    name = "sk"
    type = "S"
  }

  # GSI1 supports position-based queries: "all WR stats in 2023".
  # gsi1pk = "POSITION#WR", gsi1sk begins_with "SEASON#2023"
  #
  # Only player stat rows carry gsi1pk / gsi1sk. Team stats and player bio
  # rows do NOT set these attributes, so DynamoDB silently excludes them from
  # the index. This is called a sparse index — it stays small and cheap because
  # it only contains the rows the position query actually cares about.
  attribute {
    name = "gsi1pk"
    type = "S"
  }

  attribute {
    name = "gsi1sk"
    type = "S"
  }

  global_secondary_index {
    name            = "GSI1"
    projection_type = "ALL" # return all attributes — no need for a second fetch

    key_schema {
      attribute_name = "gsi1pk"
      key_type       = "HASH"
    }

    key_schema {
      attribute_name = "gsi1sk"
      key_type       = "RANGE"
    }
  }

  lifecycle {
    prevent_destroy = true
  }

  tags = {
    Project     = "FantasyNFL"
    Environment = "production"
  }
}
