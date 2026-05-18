package service

import (
	"context"
	"strings"

	"GoScrumPoker/internal/domain"
	"GoScrumPoker/internal/repository"
)

// RoomService contains room lifecycle use-cases.
type RoomService struct {
	rooms repository.RoomRepository
}

// NewRoomService constructs a RoomService.
func NewRoomService(rooms repository.RoomRepository) *RoomService {
	return &RoomService{rooms: rooms}
}

// CreateRoom allocates a new planning-poker room.
func (s *RoomService) CreateRoom(ctx context.Context) (*domain.Room, error) {
	return s.rooms.CreateRoom(ctx)
}

// GetOrCreateRoomForMeet returns the room ID bound to the given Google Meet
// meetingId, allocating a new room on first launch. The second return value
// is true when a room was created on this call.
func (s *RoomService) GetOrCreateRoomForMeet(ctx context.Context, meetingID string) (string, bool, error) {
	meetingID = strings.TrimSpace(meetingID)
	if meetingID == "" {
		return "", false, ErrInvalidInput
	}
	return s.rooms.GetOrCreateRoomByMeet(ctx, meetingID)
}

// RoomExists reports whether a room id is known.
func (s *RoomService) RoomExists(ctx context.Context, id string) (bool, error) {
	return s.rooms.Exists(ctx, id)
}

// GetSnapshot returns the public room view.
func (s *RoomService) GetSnapshot(ctx context.Context, id string) (domain.RoomState, bool, error) {
	return s.rooms.Snapshot(ctx, id)
}

// JoinRoom registers a participant after validating input.
func (s *RoomService) JoinRoom(ctx context.Context, roomID string, user domain.User) error {
	if strings.TrimSpace(roomID) == "" {
		return ErrInvalidInput
	}
	if strings.TrimSpace(user.ID) == "" || strings.TrimSpace(user.Name) == "" {
		return ErrInvalidInput
	}
	user.ID = strings.TrimSpace(user.ID)
	user.Name = strings.TrimSpace(user.Name)
	user.Avatar = strings.TrimSpace(user.Avatar)
	return s.rooms.Join(ctx, roomID, user)
}

// LeaveRoom removes a participant and their vote.
func (s *RoomService) LeaveRoom(ctx context.Context, roomID, userID string) error {
	if strings.TrimSpace(roomID) == "" || strings.TrimSpace(userID) == "" {
		return ErrInvalidInput
	}
	return s.rooms.Leave(ctx, roomID, strings.TrimSpace(userID))
}
