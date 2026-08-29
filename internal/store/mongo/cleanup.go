package mongostore

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/nexryai/rosmarinus/internal/domain/cleanup"
)

type AccountCleanupRepository struct {
	db *mongo.Database
}

func NewAccountCleanupRepository(db *mongo.Database) *AccountCleanupRepository {
	return &AccountCleanupRepository{db: db}
}

func (r *AccountCleanupRepository) CleanupNote(ctx context.Context, noteID string) (cleanup.NoteResult, error) {
	if noteID == "" {
		return cleanup.NoteResult{}, fmt.Errorf("cleanup note id is required")
	}
	now := time.Now().UTC()
	result := cleanup.NoteResult{}
	updated, err := r.db.Collection("reactions").UpdateMany(ctx, bson.M{
		"noteId": noteID, "deletedAt": nil,
	}, bson.M{"$set": bson.M{"deletedAt": now}})
	if err != nil {
		return result, fmt.Errorf("cleanup reactions on note %s: %w", noteID, err)
	}
	result.Reactions = updated.ModifiedCount

	deleted, err := r.db.Collection("poll_votes").DeleteMany(ctx, bson.M{"noteId": noteID})
	if err != nil {
		return result, fmt.Errorf("cleanup poll votes on note %s: %w", noteID, err)
	}
	result.PollVotes = deleted.DeletedCount
	deleted, err = r.db.Collection("polls").DeleteMany(ctx, bson.M{"_id": noteID})
	if err != nil {
		return result, fmt.Errorf("cleanup poll on note %s: %w", noteID, err)
	}
	result.Polls = deleted.DeletedCount
	deleted, err = r.db.Collection("notifications").DeleteMany(ctx, bson.M{"noteId": noteID})
	if err != nil {
		return result, fmt.Errorf("cleanup notifications on note %s: %w", noteID, err)
	}
	result.Notifications = deleted.DeletedCount
	return result, nil
}

func (r *AccountCleanupRepository) CleanupActor(ctx context.Context, actorID string) (cleanup.Result, error) {
	if actorID == "" {
		return cleanup.Result{}, fmt.Errorf("cleanup actor id is required")
	}
	now := time.Now().UTC()
	result := cleanup.Result{}
	updates := []struct {
		collection string
		filter     bson.M
		count      *int64
	}{
		{"notes", bson.M{"authorId": actorID, "deletedAt": nil}, &result.Notes},
		{"reactions", bson.M{"actorId": actorID, "deletedAt": nil}, &result.Reactions},
		{"follows", bson.M{"$or": bson.A{bson.M{"followerId": actorID}, bson.M{"followeeId": actorID}}, "deletedAt": nil}, &result.Follows},
		{"blocks", bson.M{"$or": bson.A{bson.M{"blockerId": actorID}, bson.M{"blockeeId": actorID}}, "deletedAt": nil}, &result.Blocks},
	}
	for _, update := range updates {
		updated, err := r.db.Collection(update.collection).UpdateMany(ctx, update.filter, bson.M{"$set": bson.M{"deletedAt": now}})
		if err != nil {
			return result, fmt.Errorf("cleanup %s for actor %s: %w", update.collection, actorID, err)
		}
		*update.count = updated.ModifiedCount
	}
	if err := r.cleanupActorNoteDependencies(ctx, actorID, now, &result); err != nil {
		return result, err
	}
	deleted, err := r.db.Collection("polls").DeleteMany(ctx, bson.M{"authorId": actorID})
	if err != nil {
		return result, fmt.Errorf("cleanup polls for actor %s: %w", actorID, err)
	}
	result.Polls = deleted.DeletedCount
	if _, err := r.db.Collection("poll_votes").DeleteMany(ctx, bson.M{"actorId": actorID}); err != nil {
		return result, fmt.Errorf("cleanup poll votes for actor %s: %w", actorID, err)
	}
	deleted, err = r.db.Collection("notifications").DeleteMany(ctx, bson.M{"sourceActorId": actorID})
	if err != nil {
		return result, fmt.Errorf("cleanup notifications for actor %s: %w", actorID, err)
	}
	result.Notifications += deleted.DeletedCount
	return result, nil
}

func (r *AccountCleanupRepository) cleanupActorNoteDependencies(ctx context.Context, actorID string, now time.Time, result *cleanup.Result) error {
	cursor, err := r.db.Collection("notes").Find(ctx, bson.M{"authorId": actorID}, options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return fmt.Errorf("list notes for actor %s cleanup: %w", actorID, err)
	}
	defer cursor.Close(ctx)
	ids := make([]string, 0, 500)
	flush := func() error {
		if len(ids) == 0 {
			return nil
		}
		updated, err := r.db.Collection("reactions").UpdateMany(ctx, bson.M{
			"noteId": bson.M{"$in": ids}, "deletedAt": nil,
		}, bson.M{"$set": bson.M{"deletedAt": now}})
		if err != nil {
			return fmt.Errorf("cleanup reactions on notes by actor %s: %w", actorID, err)
		}
		result.Reactions += updated.ModifiedCount
		deleted, err := r.db.Collection("notifications").DeleteMany(ctx, bson.M{"noteId": bson.M{"$in": ids}})
		if err != nil {
			return fmt.Errorf("cleanup notifications on notes by actor %s: %w", actorID, err)
		}
		result.Notifications += deleted.DeletedCount
		if _, err := r.db.Collection("poll_votes").DeleteMany(ctx, bson.M{"noteId": bson.M{"$in": ids}}); err != nil {
			return fmt.Errorf("cleanup poll votes on notes by actor %s: %w", actorID, err)
		}
		ids = ids[:0]
		return nil
	}
	for cursor.Next(ctx) {
		var doc struct {
			ID string `bson:"_id"`
		}
		if err := cursor.Decode(&doc); err != nil {
			return fmt.Errorf("decode note for actor %s cleanup: %w", actorID, err)
		}
		if doc.ID != "" {
			ids = append(ids, doc.ID)
			if len(ids) == cap(ids) {
				if err := flush(); err != nil {
					return err
				}
			}
		}
	}
	if err := cursor.Err(); err != nil {
		return fmt.Errorf("iterate notes for actor %s cleanup: %w", actorID, err)
	}
	return flush()
}
