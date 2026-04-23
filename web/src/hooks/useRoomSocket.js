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

/**
 * WebSocket to Go server: join on connect, then exposes vote / reveal / reset.
 */
export function useRoomSocket({ roomId, displayName, enabled, onServerMessage }) {
  const wsRef = useRef(null);
  const userIdRef = useRef(getOrCreateUserId());
  const onMessageRef = useRef(onServerMessage);
  const nameForOpenRef = useRef(displayName);
  nameForOpenRef.current = displayName;
  onMessageRef.current = onServerMessage;

  useEffect(() => {
    if (!enabled || !roomId) return;
    const initialName = (nameForOpenRef.current || "").trim();
    if (!initialName) return;

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
            payload: { user_id: userIdRef.current, name: n },
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

  /** Updates display name in the room (reuses server join/upsert). */
  const rejoinWithName = useCallback(
    (name) => {
      const n = (name || "").trim();
      if (!n) return;
      send({
        type: "join",
        payload: { user_id: userIdRef.current, name: n },
      });
    },
    [send]
  );

  return {
    userId: userIdRef.current,
    vote: (value) => send({ type: "vote", payload: { value } }),
    reveal: () => send({ type: "reveal" }),
    reset: () => send({ type: "reset" }),
    rejoinWithName,
  };
}
