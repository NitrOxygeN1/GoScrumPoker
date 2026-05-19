import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App.jsx";
import { initEmbedLayout } from "./embed.js";
import { initMeetAddon } from "./meetAddon.js";
import { handleSignInPopupHandoff } from "./signInPopup.js";
import "./index.css";

initEmbedLayout();

// If this tab is the OAuth popup returning from Google, post the result to
// the opener (the iframe SPA) and close. We render a minimal placeholder so
// the user sees something coherent in the half-second before window.close
// actually runs, and as a fallback if the browser refuses to close us.
const isSignInPopup = handleSignInPopupHandoff();

if (isSignInPopup) {
  ReactDOM.createRoot(document.getElementById("root")).render(
    <div className="popup-handoff" role="status" aria-live="polite">
      <h1>Signed in</h1>
      <p>You can close this window.</p>
    </div>
  );
} else {
  // Fire-and-forget: Meet's host shell needs createAddonSession to resolve
  // quickly, so kick the handshake off before React paints. Failure modes
  // are logged and don't block standalone (non-Meet) use of the app.
  initMeetAddon();

  ReactDOM.createRoot(document.getElementById("root")).render(
    <React.StrictMode>
      <App />
    </React.StrictMode>
  );
}
