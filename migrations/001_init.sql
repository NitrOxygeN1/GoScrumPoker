-- Scrum Poker persistence (PostgreSQL).
-- rooms: one row per session
-- room_participants: join/leave (not in original two-table sketch but required by the app)
-- votes: one row per (room, user) vote; upsert on change

CREATE TABLE IF NOT EXISTS rooms (
    id UUID PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revealed BOOLEAN NOT NULL DEFAULT false
);

CREATE TABLE IF NOT EXISTS room_participants (
    room_id UUID NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    user_id TEXT NOT NULL,
    display_name TEXT NOT NULL,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (room_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_room_participants_room ON room_participants (room_id);

CREATE TABLE IF NOT EXISTS votes (
    id BIGSERIAL PRIMARY KEY,
    room_id UUID NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    user_id TEXT NOT NULL,
    value TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (room_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_votes_room ON votes (room_id);
