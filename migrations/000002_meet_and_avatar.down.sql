ALTER TABLE room_participants DROP COLUMN IF EXISTS avatar_url;
DROP INDEX IF EXISTS uq_rooms_meet_meeting_id;
ALTER TABLE rooms DROP COLUMN IF EXISTS meet_meeting_id;
