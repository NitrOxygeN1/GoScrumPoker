DROP INDEX IF EXISTS uq_rooms_meet_meeting_id;

CREATE UNIQUE INDEX IF NOT EXISTS uq_rooms_meet_meeting_id
    ON rooms (meet_meeting_id)
    WHERE meet_meeting_id IS NOT NULL;
