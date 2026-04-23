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
  onMessageRef.current = onServerMessage;

  useEffect(() => {
    if (!enabled || !roomId || !displayName?.trim()) return;

    const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
    const ws = new WebSocket(`${proto}//${window.location.host}/ws/${roomId}`);
    wsRef.current = ws;

    ws.onopen = () => {
      ws.send(
        JSON.stringify({
          type: "join",
          payload: { user_id: userIdRef.current, name: displayName.trim() },
        })
      );
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
  }, [roomId, displayName, enabled]);

  const send = useCallback((obj) => {
    const ws = wsRef.current;
    if (ws?.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify(obj));
    }
  }, []);

  return {
    userId: userIdRef.current,
    vote: (value) => send({ type: "vote", payload: { value } }),
    reveal: () => send({ type: "reveal" }),
    reset: () => send({ type: "reset" }),
  };
}
