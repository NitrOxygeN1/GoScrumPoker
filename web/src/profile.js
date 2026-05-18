/**
 * Loads the currently signed-in Google profile from /api/me.
 *
 * The server exposes /api/me only when Google OAuth is configured AND the user
 * has a valid session cookie. Anything else (401, 404, 503, network error) is
 * treated as "no profile" so callers can fall back to anonymous defaults.
 *
 * Memoized: the result is cached for the lifetime of the page so navigation
 * within the SPA doesn't repeatedly hit the endpoint.
 *
 * Resolves to:
 *   { signedIn: true,  displayName: string, avatar: string, email: string }
 *   { signedIn: false }
 */
let cached = null;

export function fetchCurrentProfile() {
  if (cached) return cached;
  cached = (async () => {
    try {
      // credentials:"include" is critical: inside a Meet iframe, the cookie is
      // third-party and only sent when the request is explicitly cross-origin.
      const res = await fetch("/api/me", {
        credentials: "include",
        headers: { Accept: "application/json" },
      });
      if (!res.ok) return { signedIn: false };
      const data = await res.json();
      const displayName = String(data?.display_name || "").trim();
      const avatar = String(data?.avatar || "").trim();
      const email = String(data?.email || "").trim();
      if (!displayName && !avatar && !email) return { signedIn: false };
      return { signedIn: true, displayName, avatar, email };
    } catch {
      return { signedIn: false };
    }
  })();
  return cached;
}

/** Test/reset hook; not used in production code paths. */
export function _resetProfileCacheForTests() {
  cached = null;
}
