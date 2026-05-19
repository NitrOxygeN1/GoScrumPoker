import { useCallback, useEffect, useRef, useState } from "react";
import { refetchProfile, signOut as signOutProfile } from "../profile.js";

const POLL_INTERVAL_MS = 1500;
const SIGN_IN_TIMEOUT_MS = 90_000;
const POPUP_FEATURES = "popup,width=480,height=640,noopener=no";
const LOGIN_URL_BASE = "/auth/google/login";

/**
 * Drives the "Sign in with Google" flow from inside the Meet add-on iframe.
 *
 *  • Opens /auth/google/login in a popup window (top-level, breaks out of the
 *    Meet iframe). The server's writeTopLevelRedirect is still used as a
 *    safety net when the popup itself happens to load inside an iframe.
 *  • Polls /api/me every ~1.5s for up to 90s.
 *  • On success: closes the popup, sets {signedIn, displayName, avatar, email}.
 *  • On popup blocker / opener=null: falls back to navigating the top-level
 *    window to /auth/google/login (the user will return to the meeting and
 *    re-open the add-on; auto-join picks up where it left off).
 *
 * The hook never throws; failures surface via the `error` field.
 */
export function useGoogleSignIn({ initialProfile } = {}) {
  const [profile, setProfile] = useState(
    initialProfile || { signedIn: false }
  );
  const [signingIn, setSigningIn] = useState(false);
  const [error, setError] = useState("");

  const popupRef = useRef(null);
  const intervalRef = useRef(0);
  const timeoutRef = useRef(0);
  const cancelledRef = useRef(false);

  const stopPolling = useCallback(() => {
    if (intervalRef.current) {
      window.clearInterval(intervalRef.current);
      intervalRef.current = 0;
    }
    if (timeoutRef.current) {
      window.clearTimeout(timeoutRef.current);
      timeoutRef.current = 0;
    }
  }, []);

  const finishSuccess = useCallback(
    (next) => {
      stopPolling();
      setProfile(next);
      setSigningIn(false);
      setError("");
      const popup = popupRef.current;
      popupRef.current = null;
      try {
        popup?.close();
      } catch {
        /* cross-origin close is OK to ignore */
      }
    },
    [stopPolling]
  );

  const finishFailure = useCallback(
    (msg) => {
      stopPolling();
      setSigningIn(false);
      setError(msg || "");
      popupRef.current = null;
    },
    [stopPolling]
  );

  useEffect(() => () => {
    cancelledRef.current = true;
    stopPolling();
    try {
      popupRef.current?.close();
    } catch {
      /* ignore */
    }
    popupRef.current = null;
  }, [stopPolling]);

  const beginPolling = useCallback(() => {
    if (intervalRef.current) return;
    const tick = async () => {
      if (cancelledRef.current) return;
      const popup = popupRef.current;
      // User dismissed the popup manually — give up gracefully.
      if (popup && popup.closed) {
        finishFailure("Sign-in window was closed before completing.");
        return;
      }
      try {
        const next = await refetchProfile();
        if (cancelledRef.current) return;
        if (next?.signedIn) {
          finishSuccess(next);
        }
      } catch {
        /* network error mid-poll; try again on the next tick */
      }
    };
    intervalRef.current = window.setInterval(tick, POLL_INTERVAL_MS);
    timeoutRef.current = window.setTimeout(() => {
      finishFailure("Sign-in timed out. Please try again.");
    }, SIGN_IN_TIMEOUT_MS);
    // Fire one immediate poll so an already-signed-in session (e.g. cookie just
    // arrived) flips the UI before the first 1.5s interval elapses.
    tick();
  }, [finishFailure, finishSuccess]);

  const signIn = useCallback(
    (opts) => {
      if (signingIn) return;
      const url = opts?.switchAccount
        ? `${LOGIN_URL_BASE}?switch=1`
        : LOGIN_URL_BASE;

      setError("");
      setSigningIn(true);

      let popup = null;
      try {
        popup = window.open(url, "gsp-google-signin", POPUP_FEATURES);
      } catch {
        popup = null;
      }

      if (!popup || popup.closed || typeof popup.closed === "undefined") {
        // Popup blocked: fall back to a top-level navigation. The server's
        // writeTopLevelRedirect on /auth/google/login handles the case where
        // the request was made from inside an iframe.
        try {
          if (window.top && window.top !== window.self) {
            window.top.location.href = url;
          } else {
            window.location.href = url;
          }
        } catch {
          window.location.href = url;
        }
        // We won't get a chance to poll — the page is navigating away — but
        // record the attempt so the button shows a spinner until unload.
        return;
      }

      popupRef.current = popup;
      try {
        popup.focus();
      } catch {
        /* cross-origin focus restrictions: ignore */
      }
      beginPolling();
    },
    [beginPolling, signingIn]
  );

  const signOut = useCallback(async () => {
    stopPolling();
    setError("");
    try {
      popupRef.current?.close();
    } catch {
      /* ignore */
    }
    popupRef.current = null;
    setSigningIn(false);

    const next = await signOutProfile();
    setProfile(next);
    return next;
  }, [stopPolling]);

  const cancel = useCallback(() => {
    finishFailure("");
    try {
      popupRef.current?.close();
    } catch {
      /* ignore */
    }
    popupRef.current = null;
  }, [finishFailure]);

  return { profile, signIn, signOut, cancel, signingIn, error, setProfile };
}
