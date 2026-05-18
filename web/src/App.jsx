import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useRoomSocket } from "./hooks/useRoomSocket.js";
import { readDisplayName, saveDisplayName } from "./displayNameStorage.js";
import { readLastRoomId, saveLastRoomId } from "./lastRoomStorage.js";
import { computeVoteRecommendation } from "./voteRecommendation.js";
import { getMeetMeetingInfo, isMeetAddonConfigured } from "./meetAddon.js";
import { fetchCurrentProfile } from "./profile.js";
import { isEmbedded } from "./embed.js";

/** Initial guess that the app is loading inside a Google Meet add-on. Suppresses
 *  the standalone lobby while the Meet SDK handshake decides where to send us. */
function probablyInMeet() {
  if (typeof window === "undefined") return false;
  return isEmbedded() && isMeetAddonConfigured();
}

const CARDS = ["1", "2", "3", "5", "8", "13", "?", "coffee"];
const STORY_NUMS = [1, 2, 3, 5, 8, 13];

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

/** @returns {null | "path"} — invalid URL; `null` means `/` or a single valid room UUID. */
function getPathNotFoundKind() {
  if (typeof window === "undefined") return null;
  const path = (window.location.pathname || "/").replace(/\/+/g, "/");
  const part = path.replace(/^\/+/, "").split("/").filter(Boolean);
  if (part.length === 0) return null;
  if (part.length > 1) return "path";
  return UUID_RE.test(part[0]) ? null : "path";
}

function formatVote(v) {
  if (v == null || v === "") return "—";
  return v === "coffee" ? "☕" : v;
}

function initialRoomState() {
  return { id: "", revealed: false, users: [], votes: {} };
}

const nameCollator = new Intl.Collator(undefined, { sensitivity: "base" });

function compareByName(a, b) {
  return nameCollator.compare(a.name || "", b.name || "");
}

/**
 * Revealed order: no vote, then "?", "coffee", then numbers ascending. Tie: name.
 * Hidden order: not voted first, then voted; tie: name.
 */
function sortUsersForDisplay(users, votes, revealed) {
  const list = [...users];
  if (!revealed) {
    list.sort((a, b) => {
      if (a.voted !== b.voted) {
        return a.voted ? 1 : -1;
      }
      return compareByName(a, b);
    });
    return list;
  }
  function tier(v) {
    if (v == null || v === "") return 0;
    if (v === "?") return 1;
    if (v === "coffee") return 2;
    const n = parseFloat(String(v));
    if (!Number.isNaN(n)) return 3;
    return 4;
  }
  list.sort((a, b) => {
    const va = votes[a.id];
    const vb = votes[b.id];
    const ta = tier(va);
    const tb = tier(vb);
    if (ta !== tb) {
      return ta - tb;
    }
    if (ta === 3) {
      const na = parseFloat(String(va));
      const nb = parseFloat(String(vb));
      if (na !== nb) {
        return na - nb;
      }
    }
    return compareByName(a, b);
  });
  return list;
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

function IconEdit() {
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
      <path d="M12 20h9" />
      <path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4L16.5 3.5z" />
    </svg>
  );
}

function initialsFromName(name) {
  const trimmed = String(name || "").trim();
  if (!trimmed) return "?";
  const parts = trimmed.split(/\s+/).slice(0, 2);
  return parts.map((p) => p[0]).join("").toUpperCase();
}

function Avatar({ name, src, size = 28 }) {
  const [broken, setBroken] = useState(false);
  const initials = initialsFromName(name);
  const style = { width: size, height: size };
  if (src && !broken) {
    return (
      <img
        className="avatar avatar--img"
        style={style}
        src={src}
        alt=""
        aria-hidden
        referrerPolicy="no-referrer"
        onError={() => setBroken(true)}
      />
    );
  }
  return (
    <span className="avatar avatar--fallback" style={style} aria-hidden>
      {initials}
    </span>
  );
}

function IconCheck() {
  return (
    <svg
      width="20"
      height="20"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
    >
      <polyline points="4 12 9 17 20 6" />
    </svg>
  );
}

export default function App() {
  const [phase, setPhase] = useState("lobby");
  const [displayName, setDisplayName] = useState(() => readDisplayName());
  const [userAvatar, setUserAvatar] = useState("");
  // Hide the lobby until Meet auto-join settles, so users in a Meet add-on
  // never see (and accidentally click) the standalone create/join form.
  const [meetJoining, setMeetJoining] = useState(
    () => probablyInMeet() && !parsePathForLobby().fromRoomLink
  );
  const [roomIdInput, setRoomIdInput] = useState(
    () => parsePathForLobby().roomId
  );
  const [joinFromRoomLink, setJoinFromRoomLink] = useState(
    () => parsePathForLobby().fromRoomLink
  );
  const [activeRoomId, setActiveRoomId] = useState("");
  const [roomState, setRoomState] = useState(initialRoomState);
  const [busy, setBusy] = useState(false);
  const [selectedCard, setSelectedCard] = useState(null);
  const [toast, setToast] = useState(null);
  const [editingYouName, setEditingYouName] = useState(false);
  const [editNameDraft, setEditNameDraft] = useState("");
  const [linkJoinFailed, setLinkJoinFailed] = useState(false);
  const [linkJoining, setLinkJoining] = useState(() => {
    if (typeof window === "undefined") return false;
    return (
      parsePathForLobby().fromRoomLink && readDisplayName().trim() !== ""
    );
  });
  /** 404: invalid app URL, or room link with no such room. */
  const [notFound, setNotFound] = useState(() => getPathNotFoundKind());
  const toastTimerRef = useRef(0);
  const prevRevealedRef = useRef(false);
  const userIdForStateRef = useRef("");
  const canAutoJoinFromLinkRef = useRef(undefined);
  if (canAutoJoinFromLinkRef.current === undefined) {
    canAutoJoinFromLinkRef.current = readDisplayName().trim() !== "";
  }
  const displayNameForJoinRef = useRef(displayName);
  displayNameForJoinRef.current = displayName;
  const autoLinkJoinTried = useRef(false);

  const showToast = useCallback((message, kind = "default") => {
    window.clearTimeout(toastTimerRef.current);
    setToast({ message, kind });
    toastTimerRef.current = window.setTimeout(() => setToast(null), 2000);
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

  // Apply the Google profile (name + avatar) for both standalone and Meet runs.
  // We respect a non-empty display name already in storage (the user may have
  // renamed themselves) and only fill in defaults when there is none.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      const prof = await fetchCurrentProfile();
      if (cancelled || !prof || !prof.signedIn) return;
      if (prof.avatar) setUserAvatar((prev) => prev || prof.avatar);
      if (prof.displayName) {
        setDisplayName((prev) => {
          const existing = (prev || "").trim();
          return existing ? prev : prof.displayName;
        });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  // When the app is launched as a Meet add-on, bind to a Scrum Poker room that
  // tracks the current meeting and jump straight into it. Manual lobby / room
  // links still win: a path like `/<uuid>` keeps its existing auto-join flow.
  const meetAutoJoinTried = useRef(false);
  useEffect(() => {
    if (meetAutoJoinTried.current) return;
    if (joinFromRoomLink) return; // explicit room link beats meeting auto-binding
    meetAutoJoinTried.current = true;

    let cancelled = false;
    (async () => {
      const info = await getMeetMeetingInfo();
      if (cancelled) return;
      if (!info) {
        // Not in Meet (or SDK failed) — release the standalone lobby.
        setMeetJoining(false);
        return;
      }

      setMeetJoining(true);
      try {
        const profPromise = fetchCurrentProfile();
        const res = await fetch("/rooms/by-meet", {
          method: "POST",
          headers: { "Content-Type": "application/json", Accept: "application/json" },
          credentials: "include",
          body: JSON.stringify({
            meeting_id: info.meetingId || "",
            meeting_code: info.meetingCode || "",
          }),
        });
        if (!res.ok) {
          console.warn("[meet] /rooms/by-meet failed", res.status);
          return;
        }
        const data = await res.json();
        if (cancelled || !data?.id) return;

        const prof = await profPromise;
        const nameFromProfile = prof?.displayName ? String(prof.displayName) : "";
        const avatarFromProfile = prof?.avatar ? String(prof.avatar) : "";
        const stored = readDisplayName().trim();
        const finalName = stored || nameFromProfile;

        if (!finalName) {
          // No identity available — fall back to the standard lobby so the user
          // can type a name (their next visit will then auto-join in one click).
          // Stash the room id so the lobby's "Join room" still goes to it.
          setRoomIdInput(data.id);
          saveLastRoomId(data.id);
          return;
        }
        setDisplayName(finalName);
        saveDisplayName(finalName);
        if (avatarFromProfile) setUserAvatar(avatarFromProfile);

        saveLastRoomId(data.id);
        setActiveRoomId(data.id);
        prevRevealedRef.current = false;
        setRoomState(initialRoomState());
        setSelectedCard(null);
        setPhase("room");
      } catch (e) {
        console.warn("[meet] auto-join failed", e?.message || e);
      } finally {
        if (!cancelled) setMeetJoining(false);
      }
    })();

    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
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
    return () => window.clearTimeout(toastTimerRef.current);
  }, []);

  const handleServerMessage = useCallback((msg) => {
    if (msg.type === "state" && msg.payload) {
      const revealed = !!msg.payload.revealed;
      if (prevRevealedRef.current && !revealed) {
        setSelectedCard(null);
      }
      prevRevealedRef.current = revealed;
      const votes = msg.payload.votes ?? {};
      setRoomState({
        id: msg.payload.id ?? "",
        revealed,
        users: msg.payload.users ?? [],
        votes,
      });
      if (revealed) {
        const uid = userIdForStateRef.current;
        if (uid) {
          const mine = votes[uid];
          if (mine != null && String(mine).trim() !== "") {
            setSelectedCard(mine);
          } else {
            setSelectedCard(null);
          }
        }
      }
    } else if (msg.type === "error" && msg.payload?.message) {
      showToast(msg.payload.message, "error");
    }
  }, [showToast]);

  const { userId, vote, reveal, reset, rejoinWithName } = useRoomSocket({
    roomId: activeRoomId,
    displayName,
    avatar: userAvatar,
    enabled: phase === "room",
    onServerMessage: handleServerMessage,
  });

  userIdForStateRef.current = userId;

  const trySaveYouName = useCallback(() => {
    const n = editNameDraft.trim();
    if (!n) {
      showToast("Enter your name first.", "error");
      return;
    }
    setDisplayName(n);
    saveDisplayName(n);
    rejoinWithName(n);
    setEditingYouName(false);
  }, [editNameDraft, showToast, rejoinWithName]);

  const me = useMemo(
    () => roomState.users.find((u) => u.id === userId),
    [roomState.users, userId]
  );

  const sortedParticipants = useMemo(
    () =>
      sortUsersForDisplay(
        roomState.users,
        roomState.votes,
        roomState.revealed
      ),
    [roomState.users, roomState.votes, roomState.revealed]
  );

  const voteRecommendation = useMemo(
    () =>
      computeVoteRecommendation(roomState.votes, roomState.revealed, {
        format: formatVote,
        numericOptions: STORY_NUMS,
      }),
    [roomState.votes, roomState.revealed]
  );

  const joinByRoomId = useCallback(async (id, { fromAuto = false } = {}) => {
    if (!id) return;
    if (fromAuto) {
      setLinkJoining(true);
    } else {
      setBusy(true);
    }
    try {
      const res = await fetch(`/rooms/${encodeURIComponent(id)}`);
      if (res.status === 404) {
        if (joinFromRoomLink) {
          setNotFound("room");
        } else {
          showToast("Room not found.", "error");
        }
        return;
      }
      if (!res.ok) throw new Error("Could not load room");
      const n = (displayNameForJoinRef.current || "").trim();
      if (n) {
        saveDisplayName(n);
      }
      saveLastRoomId(id);
      setActiveRoomId(id);
      prevRevealedRef.current = false;
      setPhase("room");
      setRoomState(initialRoomState());
      setSelectedCard(null);
    } catch (e) {
      showToast(e.message || "Join failed", "error");
      if (fromAuto) setLinkJoinFailed(true);
    } finally {
      if (fromAuto) {
        setLinkJoining(false);
      } else {
        setBusy(false);
      }
    }
  }, [showToast, joinFromRoomLink]);

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
    if (!displayName.trim()) {
      showToast("Enter your name first.", "error");
      return;
    }
    setBusy(true);
    try {
      const res = await fetch("/rooms", { method: "POST" });
      if (!res.ok) throw new Error("Could not create room");
      const data = await res.json();
      saveDisplayName(displayName.trim());
      saveLastRoomId(data.id);
      setActiveRoomId(data.id);
      prevRevealedRef.current = false;
      setPhase("room");
      setRoomState(initialRoomState());
      setSelectedCard(null);
    } catch (e) {
      showToast(e.message || "Create failed", "error");
    } finally {
      setBusy(false);
    }
  }

  function rejoinLastRoom() {
    const id = readLastRoomId();
    if (!id) {
      showToast("No last room on this device.", "error");
      return;
    }
    if (!displayName.trim()) {
      showToast("Enter your name first.", "error");
      return;
    }
    setRoomIdInput(id);
    joinByRoomId(id, { fromAuto: false });
  }

  function joinRoom() {
    const id = roomIdInput.trim();
    if (!displayName.trim()) {
      showToast("Enter your name first.", "error");
      return;
    }
    if (!id) {
      showToast("Enter a room ID.", "error");
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
    setLinkJoinFailed(false);
    setEditingYouName(false);
    setLinkJoining(false);
    setJoinFromRoomLink(false);
    setRoomIdInput("");
    window.history.replaceState(
      null,
      "",
      "/" + window.location.search + window.location.hash
    );
  }

  const goToMainLobby = useCallback(() => {
    setNotFound(null);
    setLinkJoinFailed(false);
    setLinkJoining(false);
    setJoinFromRoomLink(false);
    setRoomIdInput("");
    setDisplayName(readDisplayName());
    window.history.replaceState(
      null,
      "",
      "/" + window.location.search + window.location.hash
    );
  }, []);

  function goHome() {
    if (notFound) {
      goToMainLobby();
      return;
    }
    if (phase === "room") {
      leaveRoom();
      return;
    }
    if (joinFromRoomLink) {
      goToMainLobby();
      return;
    }
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
      showToast("Room ID copied");
    } catch {
      showToast("Copy failed", "error");
    }
  }

  async function copyRoomUrl() {
    if (!roomShareUrl) return;
    try {
      await navigator.clipboard.writeText(roomShareUrl);
      showToast("Link copied");
    } catch {
      showToast("Copy failed", "error");
    }
  }

  function pickCard(value) {
    setSelectedCard(value);
    vote(value);
  }

  const showLinkNameForm =
    phase === "lobby" &&
    joinFromRoomLink &&
    !linkJoining &&
    !canAutoJoinFromLinkRef.current;
  const showLinkErrorPanel =
    phase === "lobby" &&
    joinFromRoomLink &&
    !linkJoining &&
    canAutoJoinFromLinkRef.current &&
    displayName.trim() &&
    linkJoinFailed;

  useEffect(() => {
    if (!showLinkNameForm && !showLinkErrorPanel) return;
    const onKey = (e) => {
      if (e.key === "Escape") {
        e.preventDefault();
        goToMainLobby();
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [showLinkNameForm, showLinkErrorPanel, goToMainLobby]);

  useEffect(() => {
    if (!notFound) return;
    const onKey = (e) => {
      if (e.key === "Escape") {
        e.preventDefault();
        goToMainLobby();
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [notFound, goToMainLobby]);

  return (
    <>
      {toast ? (
        <div
          className={
            toast.kind === "error" ? "app-toast app-toast--error" : "app-toast"
          }
          role={toast.kind === "error" ? "alert" : "status"}
          aria-live={toast.kind === "error" ? "assertive" : "polite"}
        >
          {toast.message}
        </div>
      ) : null}
      {notFound ? (
        <>
          <button type="button" className="app-title" onClick={goHome} title="Home">
            Scrum Poker
          </button>
          <div className="panel not-found" role="status" aria-live="polite">
            <h2 className="not-found-title">Not found (404)</h2>
            <p className="muted" style={{ marginTop: "0.5rem" }}>
              {notFound === "path"
                ? "This page does not exist, or the address is misspelled or not supported by this app."
                : "We could not find a room for this link. It may have been removed, or the ID is wrong or expired."}
            </p>
            <div className="row" style={{ marginTop: "1rem" }}>
              <button type="button" className="primary" onClick={goToMainLobby}>
                Back to home
              </button>
            </div>
          </div>
        </>
      ) : (
        <>
          {!(phase === "lobby" && joinFromRoomLink && linkJoining) && (
            <button
              type="button"
              className={[
                "app-title",
                phase === "lobby" && joinFromRoomLink && !linkJoining
                  ? "app-title--join-link"
                  : null,
              ]
                .filter(Boolean)
                .join(" ")}
              onClick={goHome}
              title="Home"
            >
              Scrum Poker
            </button>
          )}
          {phase === "lobby" && joinFromRoomLink && linkJoining && (
        <p className="link-join-status" role="status" aria-live="polite">
          Joining room…
        </p>
      )}
      {phase === "lobby" && !joinFromRoomLink && meetJoining && (
        <p className="link-join-status" role="status" aria-live="polite">
          Connecting to this meeting…
        </p>
      )}

      {showLinkNameForm && (
        <div className="panel join-link-panel">
          <div className="join-link-lead-section">
            <button
              type="button"
              className="icon-btn join-link-close join-link-close--corner"
              onClick={goToMainLobby}
              title="Return to home page (Esc)"
              aria-label="Return to home page"
            >
              <IconClose />
            </button>
            <p className="join-link-lead">Enter your name to join this room.</p>
          </div>
          <form
            className="join-link-name-form"
            onSubmit={(e) => {
              e.preventDefault();
              joinRoom();
            }}
          >
            <label htmlFor="name-join-link">Your name</label>
            <input
              id="name-join-link"
              name="displayName"
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
              <button type="submit" className="primary" disabled={busy}>
                Join room
              </button>
              <button type="button" className="ghost" onClick={goToMainLobby}>
                Return to home page
              </button>
            </div>
          </form>
        </div>
      )}

      {showLinkErrorPanel && (
        <div className="panel join-link-panel join-link-error-panel">
          <div
            className="join-link-lead-section"
            style={{ minHeight: "0", paddingBottom: 0 }}
          >
            <button
              type="button"
              className="icon-btn join-link-close join-link-close--corner"
              onClick={goToMainLobby}
              title="Return to home page (Esc)"
              aria-label="Return to home page"
            >
              <IconClose />
            </button>
            <span className="visually-hidden">
              The room could not be opened. Details were shown in a message.
            </span>
          </div>
          <div className="row join-link-actions" style={{ marginTop: "0.75rem" }}>
            <button type="button" className="ghost" onClick={goToMainLobby}>
              Return to home page
            </button>
          </div>
        </div>
      )}

      {phase === "lobby" && !joinFromRoomLink && !meetJoining && (
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
            {readLastRoomId() ? (
              <button
                type="button"
                className="ghost"
                disabled={busy}
                onClick={rejoinLastRoom}
                title="Open the room you used last (this device)"
              >
                Last room
              </button>
            ) : null}
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
                <div className="muted">Room ID</div>
                <button
                  type="button"
                  className="room-id-text"
                  onClick={copyRoomId}
                  title="Copy room ID"
                  aria-label="Copy room ID to clipboard"
                >
                  {activeRoomId}
                </button>
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
          </div>

          <div className="panel you-panel">
            <div className="muted">You</div>
            {editingYouName ? (
              <div className="you-line you-line--edit">
                <input
                  className="you-name-input"
                  type="text"
                  value={editNameDraft}
                  onChange={(e) => setEditNameDraft(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      e.preventDefault();
                      trySaveYouName();
                    }
                    if (e.key === "Escape") {
                      e.preventDefault();
                      setEditNameDraft((displayName || "").trim());
                      setEditingYouName(false);
                    }
                  }}
                  autoFocus
                  maxLength={80}
                  aria-label="Your display name"
                />
                <div className="you-line-actions">
                  <button
                    type="button"
                    className="icon-btn you-edit-ctl"
                    title="Discard (Esc)"
                    onClick={() => {
                      setEditNameDraft((displayName || "").trim());
                      setEditingYouName(false);
                    }}
                    aria-label="Cancel edit"
                  >
                    <IconClose />
                  </button>
                  <button
                    type="button"
                    className="icon-btn you-edit-ctl you-edit-ctl--apply"
                    title="Save"
                    onClick={() => trySaveYouName()}
                    aria-label="Save name"
                  >
                    <IconCheck />
                  </button>
                </div>
              </div>
            ) : (
              <div className="you-line you-line--view">
                <span className="you-identity">
                  <Avatar name={displayName} src={userAvatar} size={32} />
                  <span className="you-line-name">{displayName.trim()}</span>
                </span>
                <div className="you-line-actions">
                  <button
                    type="button"
                    className="icon-btn you-edit-ctl"
                    title="Edit your name"
                    onClick={() => {
                      setEditNameDraft((displayName || "").trim());
                      setEditingYouName(true);
                    }}
                    aria-label="Edit your name"
                  >
                    <IconEdit />
                  </button>
                </div>
              </div>
            )}
            {me && (
              <>
                <p className="muted" style={{ marginTop: "0.75rem" }}>
                  {roomState.revealed
                    ? "Change your card anytime (taps update votes for everyone)."
                    : "Pick a card (tap again to change)."}
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
          </div>

          <div className="panel">
            <div className="muted">Participants ({roomState.users.length})</div>
            <ul className="participants">
              {roomState.users.length === 0 && (
                <li className="muted">Connecting…</li>
              )}
              {sortedParticipants.map((u) => (
                <li key={u.id}>
                  <span className="participant-identity">
                    <Avatar name={u.name} src={u.avatar} size={28} />
                    <span className="participant-name">{u.name}</span>
                  </span>
                  <span>
                    {roomState.revealed ? (
                      <span className="badge revealed">
                        {formatVote(roomState.votes[u.id])}
                      </span>
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

          <div className="panel actions actions-with-rec">
            <div className="actions-btns">
              <button type="button" className="primary" onClick={() => reveal()}>
                Reveal votes
              </button>
              <button type="button" onClick={() => reset()}>
                Reset round
              </button>
            </div>
            {roomState.revealed && voteRecommendation && (
              <div className="actions-recommend" role="status">
                {voteRecommendation.line}
              </div>
            )}
          </div>
        </>
      )}
        </>
      )}
    </>
  );
}
