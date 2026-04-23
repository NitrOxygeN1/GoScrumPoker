import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useRoomSocket } from "./hooks/useRoomSocket.js";
import { readDisplayName, saveDisplayName } from "./displayNameStorage.js";

const CARDS = ["1", "2", "3", "5", "8", "13", "?", "coffee"];

const UUID_RE =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

/** When `/room-uuid` is open, the lobby is join-only (no create / manual id). */
function parsePathForLobby() {
  if (typeof window === "undefined") {
    return { fromRoomLink: false, roomId: "" };
  }
  const path = (window.location.pathname || "/").replace(/\/+/g, "/");
  const part = path.replace(/^\/+/, "").split("/").filter(Boolean);
  if (part.length === 1 && UUID_RE.test(part[0])) {
    return { fromRoomLink: true, roomId: part[0] };
  }
  return { fromRoomLink: false, roomId: "" };
}

function formatVote(v) {
  if (v == null || v === "") return "—";
  return v === "coffee" ? "☕" : v;
}

function initialRoomState() {
  return { id: "", revealed: false, users: [], votes: {} };
}

function IconClipboard() {
  return (
    <svg
      width="20"
      height="20"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
    >
      <rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
      <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h5a2 2 0 0 1 2 2v1" />
    </svg>
  );
}

function IconShareLink() {
  return (
    <svg
      width="20"
      height="20"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
    >
      <circle cx="18" cy="5" r="3" />
      <circle cx="6" cy="12" r="3" />
      <circle cx="18" cy="19" r="3" />
      <line x1="8.59" y1="13.51" x2="15.42" y2="17.49" />
      <line x1="15.41" y1="6.51" x2="8.59" y2="10.49" />
    </svg>
  );
}

function IconClose() {
  return (
    <svg
      width="20"
      height="20"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
    >
      <line x1="18" y1="6" x2="6" y2="18" />
      <line x1="6" y1="6" x2="18" y2="18" />
    </svg>
  );
}

export default function App() {
  const [phase, setPhase] = useState("lobby");
  const [displayName, setDisplayName] = useState(() => readDisplayName());
  const [roomIdInput, setRoomIdInput] = useState(
    () => parsePathForLobby().roomId
  );
  const [joinFromRoomLink, setJoinFromRoomLink] = useState(
    () => parsePathForLobby().fromRoomLink
  );
  const [activeRoomId, setActiveRoomId] = useState("");
  const [roomState, setRoomState] = useState(initialRoomState);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [selectedCard, setSelectedCard] = useState(null);
  const [copyToast, setCopyToast] = useState("");
  const [linkJoining, setLinkJoining] = useState(() => {
    if (typeof window === "undefined") return false;
    return (
      parsePathForLobby().fromRoomLink && readDisplayName().trim() !== ""
    );
  });
  const copyToastTimerRef = useRef(0);
  const prevRevealedRef = useRef(false);
  const canAutoJoinFromLinkRef = useRef(undefined);
  if (canAutoJoinFromLinkRef.current === undefined) {
    canAutoJoinFromLinkRef.current = readDisplayName().trim() !== "";
  }
  const displayNameForJoinRef = useRef(displayName);
  displayNameForJoinRef.current = displayName;
  const autoLinkJoinTried = useRef(false);

  const showCopyToast = useCallback((message) => {
    window.clearTimeout(copyToastTimerRef.current);
    setCopyToast(message);
    copyToastTimerRef.current = window.setTimeout(() => setCopyToast(""), 2000);
  }, []);

  useEffect(() => {
    const raw = window.location.pathname;
    const path = (raw || "/").replace(/\/+/g, "/");
    if (raw !== path) {
      window.history.replaceState(
        null,
        "",
        path + window.location.search + window.location.hash
      );
    }
    const part = path.replace(/^\/+/, "").split("/").filter(Boolean);
    if (part.length === 1 && UUID_RE.test(part[0])) {
      setRoomIdInput((prev) => (prev ? prev : part[0]));
      setJoinFromRoomLink(true);
    } else {
      setJoinFromRoomLink(false);
    }
  }, []);

  useEffect(() => {
    if (phase !== "room" || !activeRoomId) return;
    const want = `/${activeRoomId}`;
    const current = (window.location.pathname || "/").replace(/\/+/g, "/");
    if (current !== want) {
      window.history.replaceState(
        null,
        "",
        want + window.location.search + window.location.hash
      );
    }
  }, [phase, activeRoomId]);

  useEffect(() => {
    return () => window.clearTimeout(copyToastTimerRef.current);
  }, []);

  useEffect(() => {
    const isLinkManualName =
      joinFromRoomLink && phase === "lobby" && !linkJoining && !canAutoJoinFromLinkRef.current;
    if (isLinkManualName) {
      return;
    }
    const t = setTimeout(() => {
      if (displayName.trim()) {
        saveDisplayName(displayName.trim());
      } else {
        saveDisplayName("");
      }
    }, 400);
    return () => clearTimeout(t);
  }, [displayName, joinFromRoomLink, phase, linkJoining]);

  useEffect(() => {
    if (phase !== "room" || !activeRoomId) return;
    const n = displayName.trim();
    if (n) {
      // Refresh 7-day window on every visit to a room
      saveDisplayName(n);
    }
  }, [phase, activeRoomId, displayName]);

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

  const joinByRoomId = useCallback(async (id, { fromAuto = false } = {}) => {
    if (!id) return;
    setError("");
    if (fromAuto) {
      setLinkJoining(true);
    } else {
      setBusy(true);
    }
    try {
      const res = await fetch(`/rooms/${encodeURIComponent(id)}`);
      if (res.status === 404) {
        setError("Room not found.");
        return;
      }
      if (!res.ok) throw new Error("Could not load room");
      const n = (displayNameForJoinRef.current || "").trim();
      if (n) {
        saveDisplayName(n);
      }
      setActiveRoomId(id);
      prevRevealedRef.current = false;
      setPhase("room");
      setRoomState(initialRoomState());
      setSelectedCard(null);
    } catch (e) {
      setError(e.message || "Join failed");
    } finally {
      if (fromAuto) {
        setLinkJoining(false);
      } else {
        setBusy(false);
      }
    }
  }, []);

  useEffect(() => {
    if (phase !== "lobby" || !joinFromRoomLink) return;
    const id = roomIdInput.trim();
    if (!id) return;
    if (!canAutoJoinFromLinkRef.current) return;
    if (autoLinkJoinTried.current) return;
    autoLinkJoinTried.current = true;
    joinByRoomId(id, { fromAuto: true });
  }, [joinFromRoomLink, roomIdInput, phase, joinByRoomId]);

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

  function joinRoom() {
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
    joinByRoomId(id, { fromAuto: false });
  }

  function leaveRoom() {
    prevRevealedRef.current = false;
    setPhase("lobby");
    setActiveRoomId("");
    setRoomState(initialRoomState());
    setSelectedCard(null);
    setError("");
    setLinkJoining(false);
    setJoinFromRoomLink(false);
    setRoomIdInput("");
    window.history.replaceState(
      null,
      "",
      "/" + window.location.search + window.location.hash
    );
  }

  function goToMainLobby() {
    setError("");
    setLinkJoining(false);
    setJoinFromRoomLink(false);
    setRoomIdInput("");
    setDisplayName(readDisplayName());
    window.history.replaceState(
      null,
      "",
      "/" + window.location.search + window.location.hash
    );
  }

  const roomShareUrl = useMemo(() => {
    if (typeof window === "undefined" || !activeRoomId) return "";
    return `${window.location.origin}/${activeRoomId}`;
  }, [activeRoomId]);

  async function copyRoomId() {
    if (!activeRoomId) return;
    try {
      await navigator.clipboard.writeText(activeRoomId);
      showCopyToast("Room ID copied");
    } catch {
      showCopyToast("Copy failed");
    }
  }

  async function copyRoomUrl() {
    if (!roomShareUrl) return;
    try {
      await navigator.clipboard.writeText(roomShareUrl);
      showCopyToast("Link copied");
    } catch {
      showCopyToast("Copy failed");
    }
  }

  function pickCard(value) {
    setSelectedCard(value);
    vote(value);
  }

  return (
    <>
      {copyToast ? (
        <div className="copy-toast" role="status">
          {copyToast}
        </div>
      ) : null}
      {!(phase === "lobby" && joinFromRoomLink && linkJoining) && (
        <h1
          className={
            phase === "lobby" && joinFromRoomLink && !linkJoining
              ? "join-link-page-title"
              : undefined
          }
        >
          Scrum Poker
        </h1>
      )}
      {!(phase === "lobby" && joinFromRoomLink) && (
        <p className="sub">
          Planning poker — same origin as the API in production, or Vite on :5173 with a
          proxied API in dev.
        </p>
      )}

      {phase === "lobby" && joinFromRoomLink && linkJoining && (
        <p className="link-join-status" role="status" aria-live="polite">
          Joining room…
        </p>
      )}

      {phase === "lobby" &&
        joinFromRoomLink &&
        !linkJoining &&
        !canAutoJoinFromLinkRef.current &&
        !(displayName.trim() && error) && (
        <div className="panel join-link-panel">
          <div className="join-link-header">
            <p className="join-link-lead">Enter your name to join this room.</p>
            <button
              type="button"
              className="icon-btn join-link-close"
              onClick={goToMainLobby}
              title="Return to home page"
              aria-label="Return to home page"
            >
              <IconClose />
            </button>
          </div>
          <label htmlFor="name">Your name</label>
          <input
            id="name"
            type="text"
            placeholder="Jane"
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            autoComplete="nickname"
            autoFocus
          />
          <div
            className="row join-link-actions"
            style={{ marginTop: "0.75rem" }}
          >
            <button
              type="button"
              className="primary"
              disabled={busy}
              onClick={joinRoom}
            >
              Join room
            </button>
            <button type="button" className="ghost" onClick={goToMainLobby}>
              Return to home page
            </button>
          </div>
          {error && <p className="error">{error}</p>}
        </div>
      )}

      {phase === "lobby" && joinFromRoomLink && !linkJoining && displayName.trim() && error && (
        <div className="panel join-link-panel join-link-error-panel">
          <div className="join-link-header join-link-header--error">
            <p className="error" style={{ margin: 0 }}>
              {error}
            </p>
            <button
              type="button"
              className="icon-btn join-link-close"
              onClick={goToMainLobby}
              title="Return to home page"
              aria-label="Return to home page"
            >
              <IconClose />
            </button>
          </div>
          <div className="row join-link-actions" style={{ marginTop: "0.75rem" }}>
            <button type="button" className="ghost" onClick={goToMainLobby}>
              Return to home page
            </button>
          </div>
        </div>
      )}

      {phase === "lobby" && !joinFromRoomLink && (
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
            <div className="room-id-row">
              <div>
                <div className="muted">Room</div>
                <div
                  className="room-id-text"
                  style={{ wordBreak: "break-all", fontSize: "0.9rem" }}
                >
                  {activeRoomId}
                </div>
              </div>
              <div className="room-id-actions">
                <button
                  type="button"
                  className="icon-btn"
                  onClick={copyRoomId}
                  title="Copy room ID"
                  aria-label="Copy room ID"
                >
                  <IconClipboard />
                </button>
                <button
                  type="button"
                  className="icon-btn"
                  onClick={copyRoomUrl}
                  title="Copy room link"
                  aria-label="Copy room link"
                >
                  <IconShareLink />
                </button>
              </div>
            </div>
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
