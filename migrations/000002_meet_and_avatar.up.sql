-- Bind a Google Meet meetingId to at most one room (idempotent room-per-call).
-- Nullable so legacy rooms created without a Meet context stay valid.
-- Partial unique index allows many NULLs but enforces a single room per meetingId.
ALTER TABLE rooms
    ADD COLUMN IF NOT EXISTS meet_meeting_id TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS uq_rooms_meet_meeting_id
    ON rooms (meet_meeting_id)
    WHERE meet_meeting_id IS NOT NULL;

-- Optional Google profile avatar URL persisted with the participant row so the
-- side panel can render avatars even on snapshot reloads.
ALTER TABLE room_participants
    ADD COLUMN IF NOT EXISTS avatar_url TEXT NOT NULL DEFAULT '';
