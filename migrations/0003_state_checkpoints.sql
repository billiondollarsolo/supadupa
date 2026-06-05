CREATE TABLE control_state_checkpoints (
    id TEXT PRIMARY KEY,
    state BYTEA NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
