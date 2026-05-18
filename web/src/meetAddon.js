import { meet } from "@googleworkspace/meet-addons/meet.addons";
import { isEmbedded } from "./embed.js";

/**
 * Cloud project number sources, in priority order:
 *   1. <meta name="gsp-cloud-project-number" content="..."> injected by the Go server
 *      at startup from GOOGLE_CLOUD_PROJECT_NUMBER. Lets the same Docker image serve
 *      different deployments without rebuilding the frontend.
 *   2. import.meta.env.VITE_GOOGLE_CLOUD_PROJECT_NUMBER for local `npm run dev`.
 */
function readMetaContent(name) {
  if (typeof document === "undefined") return "";
  const el = document.querySelector(`meta[name="${name}"]`);
  return el ? (el.getAttribute("content") || "").trim() : "";
}

function readCloudProjectNumber() {
  const fromMeta = readMetaContent("gsp-cloud-project-number");
  if (fromMeta) return fromMeta;
  const fromEnv = (import.meta.env?.VITE_GOOGLE_CLOUD_PROJECT_NUMBER ?? "")
    .toString()
    .trim();
  return fromEnv;
}

let initPromise = null;

/** True when a cloud project number is available; init will at least attempt the SDK. */
export function isMeetAddonConfigured() {
  return !!readCloudProjectNumber();
}

/**
 * Result of the last (or in-flight) Meet add-on initialization.
 * Resolves to one of:
 *   { status: "standalone" }                              // not in an iframe
 *   { status: "unconfigured" }                            // in iframe but no project number
 *   { status: "ready", frameType, session, client }       // SDK handshake completed
 *   { status: "error", error, errorType? }                // SDK threw
 */
export function getMeetAddonInit() {
  return initPromise;
}

/**
 * Resolves to the Meet `MeetingInfo` object ({ meetingId, meetingCode }) for the
 * current call, or `null` when the app is not running inside Meet or the SDK
 * client failed to expose meeting info.
 *
 * Side-panel and main-stage clients both expose `getMeetingInfo()`.
 */
export async function getMeetMeetingInfo() {
  const init = await initMeetAddon();
  if (!init || init.status !== "ready" || !init.client) return null;
  try {
    const info = await init.client.getMeetingInfo();
    if (!info || typeof info !== "object") return null;
    const meetingId = String(info.meetingId || "").trim();
    const meetingCode = String(info.meetingCode || "").trim();
    if (!meetingId && !meetingCode) return null;
    return { meetingId, meetingCode };
  } catch (e) {
    console.warn("[meet-addon] getMeetingInfo failed:", e?.message || e);
    return null;
  }
}

/**
 * Initializes the Meet Web Add-ons SDK if the page is in an iframe and a cloud
 * project number is configured. Idempotent — safe to call multiple times.
 *
 * Meet's host shell waits for createAddonSession to complete before it considers
 * the add-on "launched". Call this as early as possible (before React renders)
 * to keep the handshake well under Meet's activation timeout.
 */
export function initMeetAddon() {
  if (initPromise) return initPromise;

  initPromise = (async () => {
    if (!isEmbedded()) {
      return { status: "standalone" };
    }

    const cloudProjectNumber = readCloudProjectNumber();
    if (!cloudProjectNumber) {
      console.warn(
        "[meet-addon] cloud project number not set; skipping Meet add-on session. " +
          "Set GOOGLE_CLOUD_PROJECT_NUMBER on the server (preferred) or " +
          "VITE_GOOGLE_CLOUD_PROJECT_NUMBER at build time."
      );
      return { status: "unconfigured" };
    }

    try {
      const frameType = meet.addon.getFrameType();
      const session = await meet.addon.createAddonSession({ cloudProjectNumber });
      const client =
        frameType === "MAIN_STAGE"
          ? await session.createMainStageClient()
          : await session.createSidePanelClient();
      console.info("[meet-addon] session ready", { frameType });
      return { status: "ready", frameType, session, client };
    } catch (e) {
      const message = e?.message || String(e);
      const errorType = e?.errorType;
      console.warn("[meet-addon] init failed:", message, errorType ? `(${errorType})` : "");
      return { status: "error", error: message, errorType };
    }
  })();

  return initPromise;
}
