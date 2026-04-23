package repository

import "errors"

var (
	ErrRoomNotFound  = errors.New("room not found")
	ErrUserNotInRoom = errors.New("user not in room")
	ErrVotesRevealed = errors.New("votes are revealed; reset before voting again")
	ErrInvalidVote   = errors.New("invalid vote value")
)
