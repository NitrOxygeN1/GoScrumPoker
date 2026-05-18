import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App.jsx";
import { initEmbedLayout } from "./embed.js";
import { initMeetAddon } from "./meetAddon.js";
import "./index.css";

initEmbedLayout();
// Fire-and-forget: Meet's host shell needs createAddonSession to resolve quickly,
// so kick the handshake off before React paints. Failure modes are logged and
// don't block standalone (non-Meet) use of the app.
initMeetAddon();

ReactDOM.createRoot(document.getElementById("root")).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
);
