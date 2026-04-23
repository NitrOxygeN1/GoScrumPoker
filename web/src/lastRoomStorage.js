const KEY = "scrum_poker_last_room_id_v1";
const WEEK_MS = 7 * 24 * 60 * 60 * 1000;

/**
 * @returns {string} last saved room id, or "" if missing / expired / invalid
 */
export function readLastRoomId() {
  if (typeof window === "undefined") return "";
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return "";
    const o = JSON.parse(raw);
    if (typeof o?.id !== "string" || typeof o?.e !== "number") {
      localStorage.removeItem(KEY);
      return "";
    }
    if (o.e < Date.now()) {
      localStorage.removeItem(KEY);
      return "";
    }
    return o.id.trim() || "";
  } catch {
    return "";
  }
}

/**
 * Persists a room id; expiry is refreshed on every save. Empty string removes storage.
 */
export function saveLastRoomId(roomId) {
  if (typeof window === "undefined") return;
  const t = (roomId && String(roomId).trim()) || "";
  if (!t) {
    try {
      localStorage.removeItem(KEY);
    } catch {
      /* ignore */
    }
    return;
  }
  try {
    localStorage.setItem(
      KEY,
      JSON.stringify({ id: t, e: Date.now() + WEEK_MS })
    );
  } catch {
    /* ignore */
  }
}
