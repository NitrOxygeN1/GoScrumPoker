import { useCallback, useEffect, useRef } from "react";

const USER_KEY = "scrum_poker_user_id";

// Anonymous user_id is persisted in localStorage so it survives tab closures
// and server restarts. Earlier builds kept it in sessionStorage, which is
// wiped when the tab closes — that meant a Render idle cold-shutdown while
// the user closed their tab would re-join them as a brand-new participant
// and leave their previous row behind as a duplicate ghost. We migrate any
// legacy sessionStorage value once so existing rooms don't fragment.
function getOrCreateUserId() {
  try {
    let id = localStorage.getItem(USER_KEY);
    if (!id) {
      const legacy = sessionStorage.getItem(USER_KEY);
      if (legacy) {
        id = legacy;
        localStorage.setItem(USER_KEY, id);
        sessionStorage.removeItem(USER_KEY);
      }
    }
    if (!id) {
      id = crypto.randomUUID();
      localStorage.setItem(USER_KEY, id);
    }
    return id;
  } catch {
    return crypto.randomUUID();
  }
}

function buildJoinPayload(userId, name, avatar) {
  const payload = { user_id: userId, name };
  const a = (avatar || "").trim();
  if (a) payload.avatar = a;
  return payload;
}

/**
 * WebSocket to Go server: join on connect, then exposes vote / reveal / reset.
 *
 * `avatar` is optional; when set (e.g. Google profile picture URL) it is sent in
 * the join payload and broadcast back via the room snapshot.
 */
export function useRoomSocket({
  roomId,
  displayName,
  avatar,
  enabled,
  onServerMessage,
}) {
  const wsRef = useRef(null);
  const userIdRef = useRef(getOrCreateUserId());
  const onMessageRef = useRef(onServerMessage);
  const nameForOpenRef = useRef(displayName);
  const avatarForOpenRef = useRef(avatar || "");
  nameForOpenRef.current = displayName;
  avatarForOpenRef.current = avatar || "";
  onMessageRef.current = onServerMessage;

  useEffect(() => {
    if (!enabled || !roomId) return;
    const initialName = (nameForOpenRef.current || "").trim();
    if (!initialName) return;

    // Same host as the page (including inside a Google Meet iframe); wss on HTTPS deploys.
    const scheme = window.location.protocol === "https:" ? "wss" : "ws";
    const ws = new WebSocket(
      `${scheme}://${window.location.host}/ws/${encodeURIComponent(roomId)}`
    );
    wsRef.current = ws;

    ws.onopen = () => {
      const n = (nameForOpenRef.current || "").trim();
      if (n) {
        ws.send(
          JSON.stringify({
            type: "join",
            payload: buildJoinPayload(
              userIdRef.current,
              n,
              avatarForOpenRef.current
            ),
          })
        );
      }
    };

    ws.onmessage = (ev) => {
      try {
        const msg = JSON.parse(ev.data);
        onMessageRef.current?.(msg);
      } catch {
        /* ignore */
      }
    };

    // Close the socket explicitly when the page/iframe is being torn down.
    // The React effect cleanup only fires on component unmount — it does NOT
    // run when Google Meet destroys the side-panel iframe, when the tab
    // closes, when the mobile OS evicts the page, or when the document
    // enters bfcache. Without this, the server has to wait for the TCP FIN
    // (or its 30s ping/pong timeout) before evicting the participant, and a
    // dropped-off user keeps appearing in everyone else's list.
    //
    // `pagehide` is the modern, reliable signal for all of those transitions
    // (including bfcache). `beforeunload` is a fallback for older browsers
    // where pagehide doesn't fire inside iframes; both are idempotent
    // because the WebSocket close is.
    const closeForUnload = () => {
      try {
        if (
          ws.readyState === WebSocket.OPEN ||
          ws.readyState === WebSocket.CONNECTING
        ) {
          ws.close(1001, "page hidden");
        }
      } catch {
        /* ignore */
      }
    };
    window.addEventListener("pagehide", closeForUnload);
    window.addEventListener("beforeunload", closeForUnload);

    return () => {
      window.removeEventListener("pagehide", closeForUnload);
      window.removeEventListener("beforeunload", closeForUnload);
      ws.close();
      if (wsRef.current === ws) wsRef.current = null;
    };
  }, [roomId, enabled]);

  const send = useCallback((obj) => {
    const ws = wsRef.current;
    if (ws?.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify(obj));
    }
  }, []);

  /** Re-sends the join payload so server-side row reflects current name/avatar. */
  const rejoinWithName = useCallback(
    (name) => {
      const n = (name || "").trim();
      if (!n) return;
      send({
        type: "join",
        payload: buildJoinPayload(
          userIdRef.current,
          n,
          avatarForOpenRef.current
        ),
      });
    },
    [send]
  );

  // Push identity updates that arrive after the initial join (e.g. /api/me
  // returns a profile picture moments after the WebSocket opened, or the user
  // switches to a different Google account mid-room). `send` no-ops when the
  // socket isn't open yet, so the very first render is harmless.
  useEffect(() => {
    if (!enabled || !roomId) return;
    const n = (displayName || "").trim();
    if (!n) return;
    send({
      type: "join",
      payload: buildJoinPayload(userIdRef.current, n, avatar || ""),
    });
  }, [displayName, avatar, enabled, roomId, send]);

  return {
    userId: userIdRef.current,
    vote: (value) => send({ type: "vote", payload: { value } }),
    reveal: () => send({ type: "reveal" }),
    reset: () => send({ type: "reset" }),
    rejoinWithName,
  };
}
