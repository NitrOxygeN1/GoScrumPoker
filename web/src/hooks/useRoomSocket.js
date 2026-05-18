import { useCallback, useEffect, useRef } from "react";

const USER_KEY = "scrum_poker_user_id";

function getOrCreateUserId() {
  try {
    let id = sessionStorage.getItem(USER_KEY);
    if (!id) {
      id = crypto.randomUUID();
      sessionStorage.setItem(USER_KEY, id);
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

    return () => {
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

  // Push avatar updates that arrive after the initial join (e.g. /api/me
  // returns a profile picture moments after the WebSocket opened).
  useEffect(() => {
    if (!enabled || !roomId) return;
    const n = (nameForOpenRef.current || "").trim();
    if (!n) return;
    send({
      type: "join",
      payload: buildJoinPayload(userIdRef.current, n, avatarForOpenRef.current),
    });
  }, [avatar, enabled, roomId, send]);

  return {
    userId: userIdRef.current,
    vote: (value) => send({ type: "vote", payload: { value } }),
    reveal: () => send({ type: "reveal" }),
    reset: () => send({ type: "reset" }),
    rejoinWithName,
  };
}
