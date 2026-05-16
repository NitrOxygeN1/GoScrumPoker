import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App.jsx";
import { initEmbedLayout } from "./embed.js";
import "./index.css";

initEmbedLayout();

ReactDOM.createRoot(document.getElementById("root")).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
);
