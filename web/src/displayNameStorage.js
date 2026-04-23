const KEY = "scrum_poker_display_name_v1";
const WEEK_MS = 7 * 24 * 60 * 60 * 1000;

/**
 * @returns {string} last saved name, or "" if missing / expired / invalid
 */
export function readDisplayName() {
  if (typeof window === "undefined") return "";
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return "";
    const o = JSON.parse(raw);
    if (typeof o?.n !== "string" || typeof o?.e !== "number") {
      localStorage.removeItem(KEY);
      return "";
    }
    if (o.e < Date.now()) {
      localStorage.removeItem(KEY);
      return "";
    }
    return o.n;
  } catch {
    return "";
  }
}

/**
 * Persists a trimmed name with expiry now + 7 days (refreshed on every save / room visit).
 * Empty string removes storage.
 */
export function saveDisplayName(name) {
  if (typeof window === "undefined") return;
  const t = (name && String(name).trim()) || "";
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
      JSON.stringify({ n: t, e: Date.now() + WEEK_MS })
    );
  } catch {
    /* quota */
  }
}
