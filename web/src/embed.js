/** True when the app runs inside a parent frame (e.g. Google Meet side panel). */
export function isEmbedded() {
  try {
    return window.self !== window.top;
  } catch {
    return true;
  }
}

/** Mark the document for compact iframe layout. */
export function initEmbedLayout() {
  if (isEmbedded()) {
    document.documentElement.classList.add("embedded");
  }
}
