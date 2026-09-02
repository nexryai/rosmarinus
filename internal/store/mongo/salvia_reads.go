package mongostore

import (
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/emojis"
	"github.com/nexryai/rosmarinus/internal/domain/follows"
	"github.com/nexryai/rosmarinus/internal/domain/notes"
	"github.com/nexryai/rosmarinus/internal/domain/notifications"
	"github.com/nexryai/rosmarinus/internal/readmodel"
)

type SalviaReader struct {
	db *mongo.Database
}

func NewSalviaReader(db *mongo.Database) *SalviaReader {
	return &SalviaReader{db: db}
}

func (r *SalviaReader) ListPublicTimeline(ctx context.Context, viewerActorID string, after readmodel.Cursor, limit int) ([]readmodel.Note, error) {
	blocked, err := r.blockedActorIDs(ctx, viewerActorID)
	if err != nil {
		return nil, err
	}
	filter := bson.M{"deletedAt": nil, "visibility": string(notes.VisibilityPublic)}
	filter = withExcludedAuthors(filter, blocked)
	filter = withCreatedCursor(filter, after)
	return r.listNotes(ctx, viewerActorID, filter, limit, -1)
}

func (r *SalviaReader) ListHomeTimeline(ctx context.Context, viewerActorID string, after readmodel.Cursor, limit int) ([]readmodel.Note, error) {
	viewer, err := r.findActor(ctx, viewerActorID)
	if err != nil || viewer == nil {
		return nil, err
	}
	following, err := r.followingActorIDs(ctx, viewerActorID)
	if err != nil {
		return nil, err
	}
	blocked, err := r.blockedActorIDs(ctx, viewerActorID)
	if err != nil {
		return nil, err
	}
	feed := bson.A{
		bson.M{"authorId": viewerActorID},
		bson.M{"authorId": bson.M{"$in": following}, "visibility": bson.M{"$in": bson.A{
			string(notes.VisibilityPublic), string(notes.VisibilityHome), string(notes.VisibilityFollowers),
		}}},
		bson.M{"visibility": string(notes.VisibilitySpecified), "visibleUserUris": viewer.URI},
	}
	filter := bson.M{"deletedAt": nil, "$or": feed}
	filter = withExcludedAuthors(filter, blocked)
	filter = withCreatedCursor(filter, after)
	return r.listNotes(ctx, viewerActorID, filter, limit, -1)
}

func (r *SalviaReader) FindVisibleNote(ctx context.Context, viewerActorID, noteID string) (*readmodel.Note, error) {
	visibility, err := r.visibleNoteFilter(ctx, viewerActorID)
	if err != nil {
		return nil, err
	}
	var doc noteDocument
	filter := bson.M{"$and": bson.A{bson.M{"_id": noteID, "deletedAt": nil}, visibility}}
	if err := r.db.Collection("notes").FindOne(ctx, filter).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	items, err := r.enrichNotes(ctx, viewerActorID, []noteDocument{doc})
	if err != nil || len(items) == 0 {
		return nil, err
	}
	return &items[0], nil
}

func (r *SalviaReader) ListVisibleThread(ctx context.Context, viewerActorID, noteID string, after readmodel.Cursor, limit int) ([]readmodel.Note, error) {
	if root, err := r.FindVisibleNote(ctx, viewerActorID, noteID); err != nil || root == nil {
		return nil, err
	}
	visibility, err := r.visibleNoteFilter(ctx, viewerActorID)
	if err != nil {
		return nil, err
	}
	filter := bson.M{"$and": bson.A{bson.M{"replyId": noteID, "deletedAt": nil}, visibility}}
	filter = withCreatedCursorDirection(filter, after, 1)
	docs, err := r.listNoteDocuments(ctx, filter, limit, 1)
	if err != nil {
		return nil, err
	}
	return r.enrichNotes(ctx, viewerActorID, docs)
}

func (r *SalviaReader) ListConnections(ctx context.Context, viewerActorID, actorID, kind, afterID string, limit int) ([]readmodel.Connection, error) {
	filter := bson.M{"deletedAt": nil}
	actorField := "followerId"
	switch kind {
	case "followers":
		filter["followeeId"] = actorID
		filter["status"] = string(follows.StatusAccepted)
	case "following":
		filter["followerId"] = actorID
		filter["status"] = string(follows.StatusAccepted)
		actorField = "followeeId"
	case "requests":
		filter["followeeId"] = actorID
		filter["status"] = string(follows.StatusPending)
	default:
		return nil, fmt.Errorf("unknown connection kind %q", kind)
	}
	blocked, err := r.blockedActorIDs(ctx, viewerActorID)
	if err != nil {
		return nil, err
	}
	if len(blocked) > 0 {
		filter[actorField] = bson.M{"$nin": blocked}
	}
	if afterID != "" {
		filter["_id"] = bson.M{"$gt": afterID}
	}
	cursor, err := r.db.Collection("follows").Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []followDocument
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	result := make([]readmodel.Connection, 0, len(docs))
	for _, doc := range docs {
		actor, err := r.findActor(ctx, actorIDFromFollow(doc, actorField))
		if err != nil {
			return nil, err
		}
		if actor == nil {
			continue
		}
		result = append(result, readmodel.Connection{Follow: *toFollow(doc), Actor: actor})
	}
	return result, nil
}

func (r *SalviaReader) ListNotifications(ctx context.Context, accountID, actorID string, after readmodel.Cursor, limit int, unread *bool) ([]readmodel.Notification, error) {
	filter := bson.M{"recipientAccountId": accountID}
	if actorID != "" {
		filter["recipientActorId"] = actorID
	}
	if unread != nil {
		if *unread {
			filter["isRead"] = bson.M{"$ne": true}
		} else {
			filter["isRead"] = true
		}
	}
	filter = withCreatedCursor(filter, after)
	cursor, err := r.db.Collection("notifications").Find(ctx, filter, options.Find().SetSort(newestSort()).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []notificationDocument
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	result := make([]readmodel.Notification, 0, len(docs))
	for _, doc := range docs {
		item := readmodel.Notification{Notification: notifications.Notification{
			ID: doc.ID, RecipientAccountID: doc.RecipientAccountID, RecipientActorID: doc.RecipientActorID,
			Kind: doc.Kind, SourceActorID: doc.SourceActorID, NoteID: doc.NoteID,
			CreatedAt: doc.CreatedAt, IsRead: doc.IsRead, ReadAt: doc.ReadAt,
		}}
		if doc.SourceActorID != "" && actorID != "" {
			blocked, blockErr := r.actorsBlockEachOther(ctx, actorID, doc.SourceActorID)
			if blockErr != nil {
				return nil, blockErr
			}
			if !blocked {
				item.Source, err = r.findActor(ctx, doc.SourceActorID)
				if err != nil {
					return nil, err
				}
			}
		}
		if doc.NoteID != "" && actorID != "" {
			item.Note, err = r.FindVisibleNote(ctx, actorID, doc.NoteID)
			if err != nil {
				return nil, err
			}
		}
		result = append(result, item)
	}
	return result, nil
}

func (r *SalviaReader) FindProfile(ctx context.Context, viewerActorID, actorID string) (*readmodel.Profile, error) {
	blocked, err := r.actorsBlockEachOther(ctx, viewerActorID, actorID)
	if err != nil || blocked {
		return nil, err
	}
	actor, err := r.findActor(ctx, actorID)
	if err != nil || actor == nil {
		return nil, err
	}
	followers, err := r.db.Collection("follows").CountDocuments(ctx, bson.M{
		"followeeId": actorID, "status": string(follows.StatusAccepted), "deletedAt": nil,
	})
	if err != nil {
		return nil, err
	}
	following, err := r.db.Collection("follows").CountDocuments(ctx, bson.M{
		"followerId": actorID, "status": string(follows.StatusAccepted), "deletedAt": nil,
	})
	if err != nil {
		return nil, err
	}
	return &readmodel.Profile{Actor: actor, FollowersCount: int(followers), FollowingCount: int(following)}, nil
}

func (r *SalviaReader) ListLocalEmojis(ctx context.Context, afterName string, limit int) ([]emojis.Emoji, error) {
	filter := bson.M{"host": ""}
	if afterName != "" {
		filter["name"] = bson.M{"$gt": afterName}
	}
	cursor, err := r.db.Collection("emojis").Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "name", Value: 1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []emojiRecord
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	result := make([]emojis.Emoji, 0, len(docs))
	for _, doc := range docs {
		result = append(result, *toEmoji(doc))
	}
	return result, nil
}

func (r *SalviaReader) listNotes(ctx context.Context, viewerActorID string, filter bson.M, limit, direction int) ([]readmodel.Note, error) {
	docs, err := r.listNoteDocuments(ctx, filter, limit, direction)
	if err != nil {
		return nil, err
	}
	return r.enrichNotes(ctx, viewerActorID, docs)
}

func (r *SalviaReader) listNoteDocuments(ctx context.Context, filter bson.M, limit, direction int) ([]noteDocument, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: filter}},
		{{Key: "$sort", Value: bson.D{{Key: "createdAt", Value: direction}, {Key: "_id", Value: direction}}}},
		{{Key: "$lookup", Value: bson.M{
			"from": "actors", "let": bson.M{"authorId": "$authorId"},
			"pipeline": bson.A{
				bson.M{"$match": bson.M{"$expr": bson.M{"$eq": bson.A{"$_id", "$$authorId"}}}},
				bson.M{"$match": bson.M{"deletedAt": nil, "isSuspended": bson.M{"$ne": true}}},
				bson.M{"$project": bson.M{"_id": 1}},
			},
			"as": "activeAuthor",
		}}},
		{{Key: "$match", Value: bson.M{"activeAuthor.0": bson.M{"$exists": true}}}},
		{{Key: "$limit", Value: int64(limit)}},
		{{Key: "$unset", Value: "activeAuthor"}},
	}
	cursor, err := r.db.Collection("notes").Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []noteDocument
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (r *SalviaReader) enrichNotes(ctx context.Context, viewerActorID string, docs []noteDocument) ([]readmodel.Note, error) {
	result := make([]readmodel.Note, 0, len(docs))
	blocked, err := r.blockedActorIDs(ctx, viewerActorID)
	if err != nil {
		return nil, err
	}
	visibility, err := r.visibleNoteFilter(ctx, viewerActorID)
	if err != nil {
		return nil, err
	}
	for _, doc := range docs {
		item := readmodel.Note{Note: *toNote(doc)}
		var err error
		item.Author, err = r.findActor(ctx, doc.AuthorID)
		if err != nil {
			return nil, err
		}
		if item.Author == nil {
			continue
		}
		item.Poll, err = NewPollRepository(r.db).FindByNoteID(ctx, doc.ID)
		if err != nil {
			return nil, err
		}
		item.Reactions, err = r.reactionSummary(ctx, doc.ID, viewerActorID, blocked)
		if err != nil {
			return nil, err
		}
		if item.Poll != nil {
			item.MyVotes, err = r.myPollVotes(ctx, doc.ID, viewerActorID)
			if err != nil {
				return nil, err
			}
		}
		item.Reply, err = r.findNoteReference(ctx, doc.ReplyID, visibility)
		if err != nil {
			return nil, err
		}
		item.Quote, err = r.findNoteReference(ctx, doc.QuoteID, visibility)
		if err != nil {
			return nil, err
		}
		item.Renote, err = r.findNoteReference(ctx, doc.RenoteID, visibility)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func (r *SalviaReader) findNoteReference(ctx context.Context, noteID string, visibility bson.M) (*readmodel.NoteReference, error) {
	if noteID == "" {
		return nil, nil
	}
	var doc noteDocument
	filter := bson.M{"$and": bson.A{bson.M{"_id": noteID, "deletedAt": nil}, visibility}}
	if err := r.db.Collection("notes").FindOne(ctx, filter).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	author, err := r.findActor(ctx, doc.AuthorID)
	if err != nil || author == nil {
		return nil, err
	}
	return &readmodel.NoteReference{Note: *toNote(doc), Author: author}, nil
}

func (r *SalviaReader) visibleNoteFilter(ctx context.Context, viewerActorID string) (bson.M, error) {
	viewer, err := r.findActor(ctx, viewerActorID)
	if err != nil || viewer == nil {
		return bson.M{"_id": bson.M{"$exists": false}}, err
	}
	following, err := r.followingActorIDs(ctx, viewerActorID)
	if err != nil {
		return nil, err
	}
	blocked, err := r.blockedActorIDs(ctx, viewerActorID)
	if err != nil {
		return nil, err
	}
	visibility := bson.A{
		bson.M{"authorId": viewerActorID},
		bson.M{"visibility": bson.M{"$in": bson.A{string(notes.VisibilityPublic), string(notes.VisibilityHome), ""}}},
		bson.M{"visibility": string(notes.VisibilityFollowers), "authorId": bson.M{"$in": following}},
		bson.M{"visibility": string(notes.VisibilitySpecified), "visibleUserUris": viewer.URI},
	}
	filter := bson.M{"$or": visibility}
	return withExcludedAuthors(filter, blocked), nil
}

func (r *SalviaReader) findActor(ctx context.Context, actorID string) (*actors.Actor, error) {
	if actorID == "" {
		return nil, nil
	}
	var doc actorDocument
	err := r.db.Collection("actors").FindOne(ctx, bson.M{"_id": actorID, "deletedAt": nil, "isSuspended": bson.M{"$ne": true}}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	actor := actorFromDocument(doc)
	return &actor, nil
}

func (r *SalviaReader) followingActorIDs(ctx context.Context, actorID string) ([]string, error) {
	cursor, err := r.db.Collection("follows").Find(ctx, bson.M{
		"followerId": actorID, "status": string(follows.StatusAccepted), "deletedAt": nil,
	}, options.Find().SetProjection(bson.M{"followeeId": 1}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var rows []struct {
		ID string `bson:"followeeId"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids, nil
}

func (r *SalviaReader) blockedActorIDs(ctx context.Context, actorID string) ([]string, error) {
	cursor, err := r.db.Collection("blocks").Find(ctx, bson.M{
		"deletedAt": nil, "$or": bson.A{bson.M{"blockerId": actorID}, bson.M{"blockeeId": actorID}},
	}, options.Find().SetProjection(bson.M{"blockerId": 1, "blockeeId": 1}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var rows []blockDocument
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.BlockerID == actorID {
			ids = append(ids, row.BlockeeID)
		} else {
			ids = append(ids, row.BlockerID)
		}
	}
	return ids, nil
}

func (r *SalviaReader) actorsBlockEachOther(ctx context.Context, first, second string) (bool, error) {
	if first == second {
		return false, nil
	}
	count, err := r.db.Collection("blocks").CountDocuments(ctx, bson.M{"deletedAt": nil, "$or": bson.A{
		bson.M{"blockerId": first, "blockeeId": second}, bson.M{"blockerId": second, "blockeeId": first},
	}})
	return count > 0, err
}

func (r *SalviaReader) reactionSummary(ctx context.Context, noteID, viewerActorID string, blockedActorIDs []string) ([]readmodel.ReactionSummary, error) {
	match := bson.M{"noteId": noteID, "deletedAt": nil}
	if len(blockedActorIDs) > 0 {
		match["actorId"] = bson.M{"$nin": blockedActorIDs}
	}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: match}},
		{{Key: "$group", Value: bson.M{"_id": "$reaction", "count": bson.M{"$sum": 1}, "actors": bson.M{"$addToSet": "$actorId"}}}},
		{{Key: "$sort", Value: bson.M{"_id": 1}}},
	}
	cursor, err := r.db.Collection("reactions").Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var rows []struct {
		Reaction string   `bson:"_id"`
		Count    int      `bson:"count"`
		Actors   []string `bson:"actors"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	result := make([]readmodel.ReactionSummary, 0, len(rows))
	for _, row := range rows {
		reacted := false
		for _, id := range row.Actors {
			if id == viewerActorID {
				reacted = true
				break
			}
		}
		result = append(result, readmodel.ReactionSummary{Reaction: row.Reaction, Count: row.Count, Reacted: reacted})
	}
	return result, nil
}

func (r *SalviaReader) myPollVotes(ctx context.Context, noteID, actorID string) ([]int, error) {
	cursor, err := r.db.Collection("poll_votes").Find(ctx, bson.M{"noteId": noteID, "actorId": actorID}, options.Find().SetSort(bson.D{{Key: "choice", Value: 1}}).SetProjection(bson.M{"choice": 1}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var rows []struct {
		Choice int `bson:"choice"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	result := make([]int, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.Choice)
	}
	return result, nil
}

func withCreatedCursor(filter bson.M, after readmodel.Cursor) bson.M {
	return withCreatedCursorDirection(filter, after, -1)
}

func withCreatedCursorDirection(filter bson.M, after readmodel.Cursor, direction int) bson.M {
	if after.CreatedAt.IsZero() || after.ID == "" {
		return filter
	}
	comparison := "$lt"
	if direction > 0 {
		comparison = "$gt"
	}
	cursorFilter := bson.M{"$or": bson.A{
		bson.M{"createdAt": bson.M{comparison: after.CreatedAt}},
		bson.M{"createdAt": after.CreatedAt, "_id": bson.M{comparison: after.ID}},
	}}
	return bson.M{"$and": bson.A{filter, cursorFilter}}
}

func withExcludedAuthors(filter bson.M, actorIDs []string) bson.M {
	if len(actorIDs) == 0 {
		return filter
	}
	return bson.M{"$and": bson.A{filter, bson.M{"authorId": bson.M{"$nin": actorIDs}}}}
}

func newestSort() bson.D {
	return bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}
}

func actorIDFromFollow(doc followDocument, field string) string {
	if field == "followeeId" {
		return doc.FolloweeID
	}
	return doc.FollowerID
}

func toNote(doc noteDocument) *notes.Note {
	return &notes.Note{
		ID: doc.ID, URI: doc.URI, AttributedTo: doc.AttributedTo, AuthorID: doc.AuthorID,
		Text: doc.Text, ContentWarning: doc.ContentWarning, Sensitive: doc.Sensitive,
		InReplyToURI: doc.InReplyToURI, ReplyID: doc.ReplyID, QuoteURI: doc.QuoteURI,
		QuoteID: doc.QuoteID, RenoteID: doc.RenoteID, RenoteURI: doc.RenoteURI,
		Visibility: notes.Visibility(doc.Visibility), MentionURIs: doc.MentionURIs,
		VisibleUserURIs: doc.VisibleUserURIs, Hashtags: doc.Hashtags,
		Emojis: toDomainEmojis(doc.Emojis), Attachments: toDomainAttachments(doc.Attachments),
		CreatedAt: doc.CreatedAt, PublishedAt: doc.PublishedAt, DeletedAt: doc.DeletedAt,
	}
}

var _ readmodel.Reader = (*SalviaReader)(nil)
