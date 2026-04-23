package service

import (
	"context"
	"strings"

	"GoScrumPoker/internal/domain"
	"GoScrumPoker/internal/repository"
)

// VoteService contains voting / reveal / reset use-cases.
type VoteService struct {
	votes repository.VoteRepository
}

// NewVoteService constructs a VoteService.
func NewVoteService(votes repository.VoteRepository) *VoteService {
	return &VoteService{votes: votes}
}

// PlaceVote records or replaces a user's vote; allowed before or after reveal.
func (s *VoteService) PlaceVote(ctx context.Context, roomID, userID, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return repository.ErrInvalidVote
	}
	userID = strings.TrimSpace(userID)
	roomID = strings.TrimSpace(roomID)
	return s.votes.Apply(ctx, roomID, func(r *domain.Room) error {
		if _, ok := r.Users[userID]; !ok {
			return repository.ErrUserNotInRoom
		}
		r.Votes[userID] = value
		return nil
	})
}

// RevealVotes exposes all votes for the current round.
func (s *VoteService) RevealVotes(ctx context.Context, roomID string) error {
	roomID = strings.TrimSpace(roomID)
	return s.votes.Apply(ctx, roomID, func(r *domain.Room) error {
		r.Revealed = true
		return nil
	})
}

// ResetRound clears votes and starts a new hidden round.
func (s *VoteService) ResetRound(ctx context.Context, roomID string) error {
	roomID = strings.TrimSpace(roomID)
	return s.votes.Apply(ctx, roomID, func(r *domain.Room) error {
		r.Revealed = false
		r.Votes = make(map[string]string)
		return nil
	})
}
