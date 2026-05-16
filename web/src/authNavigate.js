const LOGIN_PATH = "/auth/google/login";

/**
 * Start Google sign-in with a full-page redirect (never a popup).
 * In an iframe, navigates the top window so OAuth is not blocked inside the frame.
 */
export function navigateToGoogleLogin() {
  const url = new URL(LOGIN_PATH, window.location.origin);
  if (isEmbedded()) {
    url.searchParams.set("embedded", "1");
    window.top.location.assign(url.toString());
    return;
  }
  window.location.assign(url.toString());
}

function isEmbedded() {
  try {
    return window.self !== window.top;
  } catch {
    return true;
  }
}
