/**
 * Handles the Google OAuth popup window's end-of-flow handoff.
 *
 * Background: when the app runs inside the Google Meet add-on iframe, the
 * session cookie is third-party. Modern Chrome frequently blocks third-party
 * cookies (or has them off by user choice), so the iframe's polling of
 * /api/me may never see the new session even though sign-in actually
 * succeeded. The popup itself, however, is a top-level window on the app's
 * own origin and can read the cookie reliably.
 *
 * This module runs in that popup right after Google redirects back to
 * `/?login=ok` (or `?login=error`). When it detects an opener it:
 *
 *   1. Reads the profile from /api/me (first-party fetch — works).
 *   2. postMessage's it to the opener (the iframe SPA) targeting our own
 *      origin so an unrelated parent on a different origin cannot intercept.
 *   3. Closes itself.
 *
 * On the parent side `useGoogleSignIn` listens for SIGNIN_MESSAGE_TYPE and
 * finishes the sign-in transition immediately, with no /api/me round-trip
 * required from inside the iframe.
 */

export const SIGNIN_MESSAGE_TYPE = "gsp-google-signin";

const POPUP_FALLBACK_TIMEOUT_MS = 1500;

function parseLoginResult() {
  try {
    const params = new URLSearchParams(window.location.search);
    const v = params.get("login");
    if (v === "ok" || v === "error") return v;
  } catch {
    /* ignore */
  }
  return null;
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
  const result = parseLoginResult();
  if (!result) return false;
  if (!hasOpener()) return false;

  (async () => {
    const profile =
      result === "ok" ? await loadProfileFirstParty() : { signedIn: false };
    postToOpener({ type: SIGNIN_MESSAGE_TYPE, result, profile });
    // Give the message a tick to flush before tearing the window down.
    window.setTimeout(tryClose, 0);
    // Safety net: if window.close was refused, try again shortly so we don't
    // leave an empty "Sign-in complete" tab around forever.
    window.setTimeout(tryClose, POPUP_FALLBACK_TIMEOUT_MS);
  })();

  return true;
}
