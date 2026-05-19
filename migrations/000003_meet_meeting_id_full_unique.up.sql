-- Migration 000002 created a *partial* unique index on rooms.meet_meeting_id
-- (WHERE meet_meeting_id IS NOT NULL). That index correctly enforces "at most
-- one room per Meet meeting", BUT Postgres's INSERT ... ON CONFLICT (column)
-- clause cannot be inferred from a partial index — it raises 42P10:
--   "there is no unique or exclusion constraint matching the ON CONFLICT
--   specification"
-- Replace it with a non-partial unique index. A plain UNIQUE index on a
-- nullable column in Postgres still allows multiple NULLs (NULLS DISTINCT is
-- the default), so semantics are preserved for rooms with no Meet binding.

DROP INDEX IF EXISTS uq_rooms_meet_meeting_id;

CREATE UNIQUE INDEX IF NOT EXISTS uq_rooms_meet_meeting_id
    ON rooms (meet_meeting_id);
