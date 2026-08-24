package mongostore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/nexryai/rosmarinus/internal/domain/polls"
)

type PollRepository struct {
	collection *mongo.Collection
}

type pollDocument struct {
	NoteID     string     `bson:"_id"`
	AuthorID   string     `bson:"authorId"`
	AuthorHost *string    `bson:"authorHost"`
	Choices    []string   `bson:"choices"`
	Votes      []int      `bson:"votes"`
	Multiple   bool       `bson:"multiple"`
	ExpiresAt  *time.Time `bson:"expiresAt,omitempty"`
	CreatedAt  time.Time  `bson:"createdAt"`
	UpdatedAt  time.Time  `bson:"updatedAt"`
}

func NewPollRepository(db *mongo.Database) *PollRepository {
	return &PollRepository{collection: db.Collection("polls")}
}

func (r *PollRepository) FindByNoteID(ctx context.Context, noteID string) (*polls.Poll, error) {
	var doc pollDocument
	if err := r.collection.FindOne(ctx, bson.M{"_id": noteID}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return toPoll(doc), nil
}

func (r *PollRepository) UpsertRemote(ctx context.Context, poll polls.Poll) (*polls.Poll, error) {
	if poll.NoteID == "" || poll.AuthorID == "" || len(poll.Choices) == 0 || len(poll.Choices) != len(poll.Votes) {
		return nil, fmt.Errorf("poll note, author, choices, and votes are required")
	}
	now := time.Now().UTC()
	if poll.CreatedAt.IsZero() {
		poll.CreatedAt = now
	}
	if poll.UpdatedAt.IsZero() {
		poll.UpdatedAt = now
	}
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": poll.NoteID}, bson.M{
		"$setOnInsert": fromPoll(poll),
	}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return nil, err
	}
	return r.FindByNoteID(ctx, poll.NoteID)
}

func (r *PollRepository) UpdateRemoteVotes(ctx context.Context, noteID, authorID string, votes []int) (*polls.Poll, error) {
	if noteID == "" || authorID == "" {
		return nil, fmt.Errorf("poll note and author are required")
	}
	existing, err := r.FindByNoteID(ctx, noteID)
	if err != nil || existing == nil {
		return existing, err
	}
	if existing.AuthorID != authorID {
		return nil, nil
	}
	if len(votes) != len(existing.Choices) {
		return nil, fmt.Errorf("poll vote count length does not match choices")
	}
	for _, vote := range votes {
		if vote < 0 {
			return nil, fmt.Errorf("poll votes must not be negative")
		}
	}
	result, err := r.collection.UpdateOne(ctx, bson.M{"_id": noteID, "authorId": authorID}, bson.M{
		"$set": bson.M{"votes": votes, "updatedAt": time.Now().UTC()},
	})
	if err != nil {
		return nil, err
	}
	if result.MatchedCount == 0 {
		return nil, nil
	}
	return r.FindByNoteID(ctx, noteID)
}

func fromPoll(poll polls.Poll) pollDocument {
	return pollDocument{
		NoteID: poll.NoteID, AuthorID: poll.AuthorID, AuthorHost: poll.AuthorHost,
		Choices: poll.Choices, Votes: poll.Votes, Multiple: poll.Multiple,
		ExpiresAt: poll.ExpiresAt, CreatedAt: poll.CreatedAt, UpdatedAt: poll.UpdatedAt,
	}
}

func toPoll(doc pollDocument) *polls.Poll {
	return &polls.Poll{
		NoteID: doc.NoteID, AuthorID: doc.AuthorID, AuthorHost: doc.AuthorHost,
		Choices: doc.Choices, Votes: doc.Votes, Multiple: doc.Multiple,
		ExpiresAt: doc.ExpiresAt, CreatedAt: doc.CreatedAt, UpdatedAt: doc.UpdatedAt,
	}
}
