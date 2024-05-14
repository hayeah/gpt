CREATE TABLE IF NOT EXISTS assistants (
    id INTEGER PRIMARY KEY,
    data TEXT NOT NULL,
    CONSTRAINT valid_json CHECK (json_valid(data))
) STRICT;

CREATE TABLE IF NOT EXISTS keys (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    CONSTRAINT valid_json CHECK (json_valid(value))
) STRICT;