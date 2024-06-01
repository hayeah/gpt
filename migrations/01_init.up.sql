CREATE TABLE IF NOT EXISTS keys (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    CONSTRAINT valid_json CHECK (json_valid(value))
) STRICT;