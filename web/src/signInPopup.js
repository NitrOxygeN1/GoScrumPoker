/**
 * Handles the Google OAuth popup window's end-of-flow handoff.
 *
 * Background: when the app runs inside the Google Meet add-on iframe, the
 * session cookie is a cross-site cookie. Modern Chrome partitions / blocks
 * such cookies aggressively, so neither the iframe nor (in some
 * partitioning configurations) the popup itself can rely on /api/me to read
 * the freshly created session — even when sign-in actually succeeded.
 *
 * To bypass cookies entirely the Go callback redirects back to
 * `/?login=ok&name=…&avatar=…&email=…`. The popup reads the profile straight
 * off the URL and posts it to its opener (the iframe SPA), then closes.
 * Falls back to a first-party /api/me fetch when the URL has no profile
 * (e.g. an older deploy that doesn't include it).
 *
 * On the parent side `useGoogleSignIn` listens for SIGNIN_MESSAGE_TYPE and
 * finishes the sign-in transition immediately.
 */

export const SIGNIN_MESSAGE_TYPE = "gsp-google-signin";

const POPUP_FALLBACK_TIMEOUT_MS = 1500;

function parseLoginResult(params) {
  const v = params.get("login");
  if (v === "ok" || v === "error") return v;
  return null;
}

function profileFromParams(params) {
  const displayName = String(params.get("name") || "").trim();
  const avatar = String(params.get("avatar") || "").trim();
  const email = String(params.get("email") || "").trim();
  if (!displayName && !avatar && !email) return null;
  return { signedIn: true, displayName, avatar, email };
}

function hasOpener() {
  try {
    return !!window.opener && window.opener !== window;
  } catch {
    return false;
  }
}

async function loadProfileFirstParty() {
  try {
    const res = await fetch("/api/me", {
      credentials: "include",
      headers: { Accept: "application/json" },
      cache: "no-store",
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
}

function postToOpener(message) {
  try {
    window.opener.postMessage(message, window.location.origin);
    return true;
  } catch {
    return false;
  }
}

function tryClose() {
  try {
    window.close();
  } catch {
    /* some browsers refuse window.close on windows they didn't open; ignore */
  }
}

/**
 * If this page is the OAuth popup, hand the result to the opener and close.
 *
 * Returns true if this tab IS the popup and the handoff has been initiated
 * (the caller should render the minimal "you can close this window" page
 * instead of mounting the full app — the popup is going away momentarily).
 */
export function handleSignInPopupHandoff() {
  if (typeof window === "undefined") return false;
  let params;
  try {
    params = new URLSearchParams(window.location.search);
  } catch {
    return false;
  }
  const result = parseLoginResult(params);
  if (!result) return false;
  if (!hasOpener()) return false;

  (async () => {
    let profile;
    if (result === "ok") {
      profile = profileFromParams(params) || (await loadProfileFirstParty());
    } else {
      profile = { signedIn: false };
    }
    postToOpener({ type: SIGNIN_MESSAGE_TYPE, result, profile });
    window.setTimeout(tryClose, 0);
    // Safety net: if window.close was refused, try again shortly so we don't
    // leave an empty "Sign-in complete" tab around forever.
    window.setTimeout(tryClose, POPUP_FALLBACK_TIMEOUT_MS);
  })();

  return true;
}
