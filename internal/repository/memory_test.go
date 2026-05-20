package repository

import (
	"context"
	"testing"

	"GoScrumPoker/internal/domain"
)

// Re-joining with the same display name but a fresh user_id (the symptom of
// a Render cold-shutdown + tab close that wipes sessionStorage) must collapse
// the previous "ghost" participant instead of producing a duplicate row.
func TestMemory_JoinEvictsSameNameGhost(t *testing.T) {
	mem := NewMemory()
	t.Cleanup(func() { _ = mem.Close() })

	ctx := context.Background()
	room, err := mem.CreateRoom(ctx)
	if err != nil {
		t.Fatalf("create room: %v", err)
	}

	if err := mem.Join(ctx, room.ID, domain.User{ID: "old-uid", Name: "Alex QA"}); err != nil {
		t.Fatalf("join old: %v", err)
	}
	if err := mem.Apply(ctx, room.ID, func(r *domain.Room) error {
		r.Votes["old-uid"] = "5"
		return nil
	}); err != nil {
		t.Fatalf("seed vote: %v", err)
	}

	if err := mem.Join(ctx, room.ID, domain.User{ID: "new-uid", Name: "alex qa"}); err != nil {
		t.Fatalf("join new: %v", err)
	}

	snap, ok, err := mem.Snapshot(ctx, room.ID)
	if err != nil || !ok {
		t.Fatalf("snapshot: ok=%v err=%v", ok, err)
	}
	if len(snap.Users) != 1 {
		t.Fatalf("expected ghost evicted, got %d users: %+v", len(snap.Users), snap.Users)
	}
	if snap.Users[0].ID != "new-uid" {
		t.Fatalf("expected fresh user_id to win, got %q", snap.Users[0].ID)
	}
	if snap.Users[0].Voted {
		t.Fatalf("ghost's vote should have been dropped along with its row")
	}
}

// Distinct names must not be evicted — only true duplicates collapse.
func TestMemory_JoinKeepsDistinctNames(t *testing.T) {
	mem := NewMemory()
	t.Cleanup(func() { _ = mem.Close() })

	ctx := context.Background()
	room, err := mem.CreateRoom(ctx)
	if err != nil {
		t.Fatalf("create room: %v", err)
	}

	for _, u := range []domain.User{
		{ID: "u1", Name: "Alex QA"},
		{ID: "u2", Name: "MC Alex"},
		{ID: "u3", Name: "Mikhail"},
		{ID: "u4", Name: "Nikita Timofeev"},
	} {
		if err := mem.Join(ctx, room.ID, u); err != nil {
			t.Fatalf("join %s: %v", u.ID, err)
		}
	}

	snap, ok, err := mem.Snapshot(ctx, room.ID)
	if err != nil || !ok {
		t.Fatalf("snapshot: ok=%v err=%v", ok, err)
	}
	if len(snap.Users) != 4 {
		t.Fatalf("expected 4 distinct participants, got %d: %+v", len(snap.Users), snap.Users)
	}
}

// Whitespace-only / empty names must never trigger eviction.
func TestMemory_JoinEmptyNameDoesNotEvict(t *testing.T) {
	mem := NewMemory()
	t.Cleanup(func() { _ = mem.Close() })

	ctx := context.Background()
	room, err := mem.CreateRoom(ctx)
	if err != nil {
		t.Fatalf("create room: %v", err)
	}

	if err := mem.Join(ctx, room.ID, domain.User{ID: "u1", Name: "   "}); err != nil {
		t.Fatalf("join u1: %v", err)
	}
	if err := mem.Join(ctx, room.ID, domain.User{ID: "u2", Name: ""}); err != nil {
		t.Fatalf("join u2: %v", err)
	}

	snap, ok, err := mem.Snapshot(ctx, room.ID)
	if err != nil || !ok {
		t.Fatalf("snapshot: ok=%v err=%v", ok, err)
	}
	if len(snap.Users) != 2 {
		t.Fatalf("blank names must not collapse; got %d users", len(snap.Users))
	}
}
