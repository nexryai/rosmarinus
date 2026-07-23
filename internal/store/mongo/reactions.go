package mongostore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/nexryai/rosmarinus/internal/domain/reactions"
)

type ReactionRepository struct {
	collection *mongo.Collection
}

type reactionDocument struct {
	ID                   string     `bson:"_id,omitempty"`
	NoteID               string     `bson:"noteId"`
	NoteURI              string     `bson:"noteUri"`
	ActorID              string     `bson:"actorId"`
	ActorURI             string     `bson:"actorUri"`
	ActorHost            *string    `bson:"actorHost"`
	Reaction             string     `bson:"reaction"`
	RemoteActivityID     string     `bson:"remoteActivityId,omitempty"`
	RemoteUndoActivityID string     `bson:"remoteUndoActivityId,omitempty"`
	CreatedAt            time.Time  `bson:"createdAt"`
	DeletedAt            *time.Time `bson:"deletedAt,omitempty"`
}

func NewReactionRepository(db *mongo.Database) *ReactionRepository {
	return &ReactionRepository{collection: db.Collection("reactions")}
}

func (r *ReactionRepository) Find(ctx context.Context, noteID, actorID string) (*reactions.Reaction, error) {
	return r.findOne(ctx, bson.M{
		"noteId":    noteID,
		"actorId":   actorID,
		"deletedAt": nil,
	})
}

func (r *ReactionRepository) FindByID(ctx context.Context, id string) (*reactions.Reaction, error) {
	return r.findOne(ctx, bson.M{
		"_id":       id,
		"deletedAt": nil,
	})
}

func (r *ReactionRepository) Upsert(ctx context.Context, reaction reactions.Reaction) (*reactions.Reaction, error) {
	if reaction.NoteID == "" || reaction.ActorID == "" {
		return nil, fmt.Errorf("noteId and actorId are required")
	}
	if reaction.ID == "" {
		reaction.ID = reactionID(reaction.NoteID, reaction.ActorID)
	}
	if reaction.CreatedAt.IsZero() {
		reaction.CreatedAt = time.Now().UTC()
	}
	doc := fromReaction(reaction)
	_, err := r.collection.UpdateOne(ctx, bson.M{
		"noteId":  doc.NoteID,
		"actorId": doc.ActorID,
	}, bson.M{
		"$set": bson.M{
			"noteUri":              doc.NoteURI,
			"actorUri":             doc.ActorURI,
			"actorHost":            doc.ActorHost,
			"reaction":             doc.Reaction,
			"remoteActivityId":     doc.RemoteActivityID,
			"remoteUndoActivityId": "",
			"deletedAt":            nil,
		},
		"$setOnInsert": bson.M{
			"_id":       doc.ID,
			"noteId":    doc.NoteID,
			"actorId":   doc.ActorID,
			"createdAt": doc.CreatedAt,
		},
	}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return nil, err
	}
	return r.Find(ctx, reaction.NoteID, reaction.ActorID)
}

func (r *ReactionRepository) Delete(ctx context.Context, noteID, actorID, remoteUndoActivityID string) error {
	now := time.Now().UTC()
	update := bson.M{
		"deletedAt": now,
	}
	if remoteUndoActivityID != "" {
		update["remoteUndoActivityId"] = remoteUndoActivityID
	}
	_, err := r.collection.UpdateOne(ctx, bson.M{
		"noteId":    noteID,
		"actorId":   actorID,
		"deletedAt": nil,
	}, bson.M{"$set": update})
	return err
}

func (r *ReactionRepository) findOne(ctx context.Context, filter bson.M) (*reactions.Reaction, error) {
	var doc reactionDocument
	if err := r.collection.FindOne(ctx, filter).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return toReaction(doc), nil
}

func fromReaction(reaction reactions.Reaction) reactionDocument {
	return reactionDocument{
		ID:                   reaction.ID,
		NoteID:               reaction.NoteID,
		NoteURI:              reaction.NoteURI,
		ActorID:              reaction.ActorID,
		ActorURI:             reaction.ActorURI,
		ActorHost:            reaction.ActorHost,
		Reaction:             reaction.Reaction,
		RemoteActivityID:     reaction.RemoteActivityID,
		RemoteUndoActivityID: reaction.RemoteUndoActivityID,
		CreatedAt:            reaction.CreatedAt,
	}
}

func toReaction(doc reactionDocument) *reactions.Reaction {
	return &reactions.Reaction{
		ID:                   doc.ID,
		NoteID:               doc.NoteID,
		NoteURI:              doc.NoteURI,
		ActorID:              doc.ActorID,
		ActorURI:             doc.ActorURI,
		ActorHost:            doc.ActorHost,
		Reaction:             doc.Reaction,
		RemoteActivityID:     doc.RemoteActivityID,
		RemoteUndoActivityID: doc.RemoteUndoActivityID,
		CreatedAt:            doc.CreatedAt,
	}
}

func reactionID(noteID, actorID string) string {
	sum := sha256.Sum256([]byte(noteID + "\x00" + actorID))
	return "reaction_" + hex.EncodeToString(sum[:])[:24]
}
