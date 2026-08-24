package mongostore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
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

type pollVoteDocument struct {
	ID        string    `bson:"_id"`
	NoteID    string    `bson:"noteId"`
	ActorID   string    `bson:"actorId"`
	Choice    int       `bson:"choice"`
	CreatedAt time.Time `bson:"createdAt"`
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

func (r *PollRepository) UpsertLocal(ctx context.Context, poll polls.Poll) (*polls.Poll, error) {
	poll.AuthorHost = nil
	return r.upsert(ctx, poll)
}

func (r *PollRepository) UpsertRemote(ctx context.Context, poll polls.Poll) (*polls.Poll, error) {
	if poll.AuthorHost == nil || *poll.AuthorHost == "" {
		return nil, fmt.Errorf("remote poll author host is required")
	}
	return r.upsert(ctx, poll)
}

func (r *PollRepository) upsert(ctx context.Context, poll polls.Poll) (*polls.Poll, error) {
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

func (r *PollRepository) RecordVote(ctx context.Context, noteID, actorID string, choice int, createdAt time.Time) (*polls.Vote, *polls.Poll, error) {
	poll, err := r.FindByNoteID(ctx, noteID)
	if err != nil {
		return nil, nil, err
	}
	if poll == nil {
		return nil, nil, polls.ErrNotFound
	}
	if choice < 0 || choice >= len(poll.Choices) {
		return nil, poll, polls.ErrInvalidChoice
	}
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	if poll.ExpiresAt != nil && !time.Now().UTC().Before(*poll.ExpiresAt) {
		return nil, poll, polls.ErrExpired
	}
	voteID := pollVoteID(noteID, actorID, choice, poll.Multiple)
	doc := pollVoteDocument{ID: voteID, NoteID: noteID, ActorID: actorID, Choice: choice, CreatedAt: createdAt}
	votesCollection := r.collection.Database().Collection("poll_votes")
	_, err = votesCollection.InsertOne(ctx, doc)
	if mongo.IsDuplicateKeyError(err) {
		if findErr := votesCollection.FindOne(ctx, bson.M{"_id": voteID}).Decode(&doc); findErr != nil {
			return nil, poll, findErr
		}
		if repairErr := r.repairVoteCount(ctx, noteID, doc.Choice); repairErr != nil {
			return nil, poll, repairErr
		}
		poll, findErr := r.FindByNoteID(ctx, noteID)
		if findErr != nil {
			return nil, nil, findErr
		}
		return &polls.Vote{ID: doc.ID, NoteID: noteID, ActorID: actorID, Choice: doc.Choice, CreatedAt: doc.CreatedAt}, poll, polls.ErrAlreadyVoted
	}
	if err != nil {
		return nil, poll, err
	}
	path := "votes." + strconv.Itoa(choice)
	if _, err := r.collection.UpdateOne(ctx, bson.M{"_id": noteID}, bson.M{
		"$inc": bson.M{path: 1}, "$set": bson.M{"updatedAt": time.Now().UTC()},
	}); err != nil {
		return nil, poll, err
	}
	poll, err = r.FindByNoteID(ctx, noteID)
	if err != nil {
		return nil, nil, err
	}
	return &polls.Vote{ID: voteID, NoteID: noteID, ActorID: actorID, Choice: choice, CreatedAt: createdAt}, poll, nil
}

func (r *PollRepository) repairVoteCount(ctx context.Context, noteID string, choice int) error {
	count, err := r.collection.Database().Collection("poll_votes").CountDocuments(ctx, bson.M{"noteId": noteID, "choice": choice})
	if err != nil {
		return err
	}
	path := "votes." + strconv.Itoa(choice)
	_, err = r.collection.UpdateOne(ctx, bson.M{"_id": noteID}, bson.M{
		"$max": bson.M{path: count}, "$set": bson.M{"updatedAt": time.Now().UTC()},
	})
	return err
}

func (r *PollRepository) ListVoterActorIDs(ctx context.Context, noteID string) ([]string, error) {
	cursor, err := r.collection.Database().Collection("poll_votes").Find(ctx, bson.M{"noteId": noteID}, options.Find().SetProjection(bson.M{"actorId": 1}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	seen := map[string]struct{}{}
	actorIDs := make([]string, 0)
	for cursor.Next(ctx) {
		var doc struct {
			ActorID string `bson:"actorId"`
		}
		if err := cursor.Decode(&doc); err != nil {
			return nil, err
		}
		if doc.ActorID == "" {
			continue
		}
		if _, exists := seen[doc.ActorID]; exists {
			continue
		}
		seen[doc.ActorID] = struct{}{}
		actorIDs = append(actorIDs, doc.ActorID)
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}
	return actorIDs, nil
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

func pollVoteID(noteID, actorID string, choice int, multiple bool) string {
	value := noteID + "\x00" + actorID
	if multiple {
		value += "\x00" + strconv.Itoa(choice)
	}
	sum := sha256.Sum256([]byte(value))
	return "poll_vote_" + hex.EncodeToString(sum[:])[:24]
}
