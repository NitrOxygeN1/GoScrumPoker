import { useCallback, useMemo, useRef, useState } from "react";
import { useRoomSocket } from "./hooks/useRoomSocket.js";

const CARDS = ["1", "2", "3", "5", "8", "13", "?", "coffee"];

function formatVote(v) {
  if (v == null || v === "") return "—";
  return v === "coffee" ? "☕" : v;
}

function initialRoomState() {
  return { id: "", revealed: false, users: [], votes: {} };
}

export default function App() {
  const [phase, setPhase] = useState("lobby");
  const [displayName, setDisplayName] = useState("");
  const [roomIdInput, setRoomIdInput] = useState("");
  const [activeRoomId, setActiveRoomId] = useState("");
  const [roomState, setRoomState] = useState(initialRoomState);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [selectedCard, setSelectedCard] = useState(null);
  const prevRevealedRef = useRef(false);

  const handleServerMessage = useCallback((msg) => {
    if (msg.type === "state" && msg.payload) {
      const revealed = !!msg.payload.revealed;
      if (prevRevealedRef.current && !revealed) {
        setSelectedCard(null);
      }
      prevRevealedRef.current = revealed;
      setRoomState({
        id: msg.payload.id ?? "",
        revealed,
        users: msg.payload.users ?? [],
        votes: msg.payload.votes ?? {},
      });
      setError("");
    } else if (msg.type === "error" && msg.payload?.message) {
      setError(msg.payload.message);
    }
  }, []);

  const { userId, vote, reveal, reset } = useRoomSocket({
    roomId: activeRoomId,
    displayName,
    enabled: phase === "room",
    onServerMessage: handleServerMessage,
  });

  const me = useMemo(
    () => roomState.users.find((u) => u.id === userId),
    [roomState.users, userId]
  );

  async function createRoom() {
    setError("");
    if (!displayName.trim()) {
      setError("Enter your name first.");
      return;
    }
    setBusy(true);
    try {
      const res = await fetch("/rooms", { method: "POST" });
      if (!res.ok) throw new Error("Could not create room");
      const data = await res.json();
      setActiveRoomId(data.id);
      prevRevealedRef.current = false;
      setPhase("room");
      setRoomState(initialRoomState());
      setSelectedCard(null);
    } catch (e) {
      setError(e.message || "Create failed");
    } finally {
      setBusy(false);
    }
  }

  async function joinRoom() {
    setError("");
    const id = roomIdInput.trim();
    if (!displayName.trim()) {
      setError("Enter your name first.");
      return;
    }
    if (!id) {
      setError("Enter a room ID.");
      return;
    }
    setBusy(true);
    try {
      const res = await fetch(`/rooms/${encodeURIComponent(id)}`);
      if (res.status === 404) {
        setError("Room not found.");
        return;
      }
      if (!res.ok) throw new Error("Could not load room");
      setActiveRoomId(id);
      prevRevealedRef.current = false;
      setPhase("room");
      setRoomState(initialRoomState());
      setSelectedCard(null);
    } catch (e) {
      setError(e.message || "Join failed");
    } finally {
      setBusy(false);
    }
  }

  function leaveRoom() {
    prevRevealedRef.current = false;
    setPhase("lobby");
    setActiveRoomId("");
    setRoomState(initialRoomState());
    setSelectedCard(null);
    setError("");
  }

  function pickCard(value) {
    setSelectedCard(value);
    vote(value);
  }

  return (
    <>
      <h1>Scrum Poker</h1>
      <p className="sub">Planning poker — same origin as the API in production, or Vite on :5173 with a proxied API in dev.</p>

      {phase === "lobby" && (
        <div className="panel">
          <label htmlFor="name">Your name</label>
          <input
            id="name"
            type="text"
            placeholder="Jane"
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            autoComplete="nickname"
          />

          <div className="row" style={{ marginTop: "1rem" }}>
            <button type="button" className="primary" disabled={busy} onClick={createRoom}>
              Create room
            </button>
          </div>

          <p className="muted" style={{ marginTop: "1.25rem" }}>
            Or join an existing room
          </p>
          <label htmlFor="room">Room ID</label>
          <input
            id="room"
            type="text"
            placeholder="uuid from host"
            value={roomIdInput}
            onChange={(e) => setRoomIdInput(e.target.value)}
          />
          <div className="row">
            <button type="button" className="primary" disabled={busy} onClick={joinRoom}>
              Join room
            </button>
          </div>
          {error && <p className="error">{error}</p>}
        </div>
      )}

      {phase === "room" && (
        <>
          <button type="button" className="back ghost" onClick={leaveRoom}>
            ← Leave room
          </button>

          <div className="panel">
            <div className="muted">Room</div>
            <div style={{ wordBreak: "break-all", fontSize: "0.9rem" }}>{activeRoomId}</div>
            {error && <p className="error">{error}</p>}
          </div>

          <div className="panel">
            <div className="muted">You</div>
            <div>
              {displayName.trim()} {me?.voted ? <span className="badge voted">voted</span> : <span className="badge">not voted</span>}
            </div>
            {!roomState.revealed && (
              <>
                <p className="muted" style={{ marginTop: "0.75rem" }}>
                  Pick a card (tap again to change)
                </p>
                <div className="cards">
                  {CARDS.map((c) => (
                    <button
                      key={c}
                      type="button"
                      className={`card-btn ${selectedCard === c ? "selected" : ""}`}
                      onClick={() => pickCard(c)}
                    >
                      {c === "coffee" ? "☕" : c}
                    </button>
                  ))}
                </div>
              </>
            )}
            {roomState.revealed && me && (
              <p style={{ marginTop: "0.75rem" }}>
                Your card:{" "}
                <strong>{formatVote(roomState.votes[userId])}</strong>
              </p>
            )}
          </div>

          <div className="panel">
            <div className="muted">Participants ({roomState.users.length})</div>
            <ul className="participants">
              {roomState.users.length === 0 && (
                <li className="muted">Connecting…</li>
              )}
              {roomState.users.map((u) => (
                <li key={u.id}>
                  <span>{u.name}</span>
                  <span>
                    {roomState.revealed ? (
                      <span className="badge revealed">{formatVote(roomState.votes[u.id])}</span>
                    ) : u.voted ? (
                      <span className="badge voted">voted</span>
                    ) : (
                      <span className="badge">waiting</span>
                    )}
                  </span>
                </li>
              ))}
            </ul>
          </div>

          <div className="panel actions">
            <button type="button" className="primary" onClick={() => reveal()}>
              Reveal votes
            </button>
            <button type="button" onClick={() => reset()}>
              Reset round
            </button>
          </div>
        </>
      )}
    </>
  );
}
