package mongostore

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/antennas"
	"github.com/nexryai/rosmarinus/internal/domain/emojis"
	"github.com/nexryai/rosmarinus/internal/domain/follows"
	"github.com/nexryai/rosmarinus/internal/domain/mutes"
	"github.com/nexryai/rosmarinus/internal/domain/notes"
	"github.com/nexryai/rosmarinus/internal/domain/notifications"
	"github.com/nexryai/rosmarinus/internal/readmodel"
)

type SalviaReader struct {
	db    *mongo.Database
	mutes mutes.Repository
}

func NewSalviaReader(db *mongo.Database) *SalviaReader {
	return NewSalviaReaderWithMuteRepository(db, NewMuteRepository(db))
}

func NewSalviaReaderWithMuteRepository(db *mongo.Database, muteRepository mutes.Repository) *SalviaReader {
	return &SalviaReader{db: db, mutes: muteRepository}
}

func (r *SalviaReader) ListPublicTimeline(ctx context.Context, viewerActorID string, after readmodel.Cursor, limit int) ([]readmodel.Note, error) {
	blocked, err := r.blockedActorIDs(ctx, viewerActorID)
	if err != nil {
		return nil, err
	}
	muted, err := r.mutedActorIDs(ctx, viewerActorID)
	if err != nil {
		return nil, err
	}
	filter := bson.M{"deletedAt": nil, "visibility": string(notes.VisibilityPublic)}
	filter = withExcludedAuthors(filter, append(blocked, muted...))
	filter = withCreatedCursor(filter, after)
	return r.listNotesWithTimelineMutes(ctx, viewerActorID, filter, limit, -1, muted)
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
	muted, err := r.mutedActorIDs(ctx, viewerActorID)
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
	filter = withExcludedAuthors(filter, append(blocked, muted...))
	filter = withCreatedCursor(filter, after)
	return r.listNotesWithTimelineMutes(ctx, viewerActorID, filter, limit, -1, muted)
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
	docs, err := r.listNoteDocuments(ctx, filter, limit, 1, nil)
	if err != nil {
		return nil, err
	}
	return r.enrichNotes(ctx, viewerActorID, docs)
}

func (r *SalviaReader) ListProfileNotes(ctx context.Context, viewerActorID, actorID string, after readmodel.Cursor, limit int) ([]readmodel.Note, error) {
	visibility, err := r.visibleNoteFilter(ctx, viewerActorID)
	if err != nil {
		return nil, err
	}
	filter := bson.M{"$and": bson.A{
		bson.M{"authorId": actorID, "deletedAt": nil},
		visibility,
	}}
	filter = withCreatedCursor(filter, after)
	return r.listNotes(ctx, viewerActorID, filter, limit, -1)
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
	case "sent_requests":
		filter["followerId"] = actorID
		filter["status"] = string(follows.StatusPending)
		actorField = "followeeId"
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
		if doc.Kind == notifications.KindReaction {
			var reaction reactionDocument
			reactionErr := r.db.Collection("reactions").FindOne(ctx, bson.M{"remoteActivityId": doc.RemoteActivityID}).Decode(&reaction)
			if reactionErr != nil && !errors.Is(reactionErr, mongo.ErrNoDocuments) {
				return nil, reactionErr
			}
			if reactionErr == nil {
				item.Reaction = reaction.Reaction
				item.ReactionEmoji, err = r.reactionEmoji(ctx, reaction.Reaction, reaction.EmojiName, reaction.EmojiURL, reaction.EmojiMediaType)
				if err != nil {
					return nil, err
				}
			}
		}
		result = append(result, item)
	}
	return result, nil
}

func (r *SalviaReader) ListAntennas(ctx context.Context, accountID, actorID string) ([]antennas.Antenna, error) {
	cursor, err := r.db.Collection("antennas").Find(ctx, antennaOwnerFilter(accountID, actorID), options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}}).SetLimit(antennas.MaxPerActor))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []antennaDocument
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	result := make([]antennas.Antenna, 0, len(docs))
	for _, doc := range docs {
		result = append(result, toAntenna(doc))
	}
	return result, nil
}

func (r *SalviaReader) FindAntenna(ctx context.Context, accountID, actorID, antennaID string) (*antennas.Antenna, error) {
	filter := antennaOwnerFilter(accountID, actorID)
	filter["_id"] = antennaID
	var doc antennaDocument
	if err := r.db.Collection("antennas").FindOne(ctx, filter).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	antenna := toAntenna(doc)
	return &antenna, nil
}

func (r *SalviaReader) ListAntennaNotes(ctx context.Context, accountID, actorID, antennaID string, after readmodel.Cursor, limit int) ([]readmodel.Note, error) {
	antenna, err := r.FindAntenna(ctx, accountID, actorID, antennaID)
	if err != nil || antenna == nil {
		return nil, err
	}
	visibility, err := r.visibleNoteFilter(ctx, actorID)
	if err != nil {
		return nil, err
	}
	blocked, err := r.blockedActorIDs(ctx, actorID)
	if err != nil {
		return nil, err
	}
	muted, err := r.mutedActorIDs(ctx, actorID)
	if err != nil {
		return nil, err
	}
	criteria := bson.A{bson.M{"deletedAt": nil}, visibility}
	if !antenna.WithReplies {
		criteria = append(criteria, bson.M{"replyId": bson.M{"$in": bson.A{nil, ""}}})
	}
	if antenna.WithFile {
		criteria = append(criteria, bson.M{"attachments.0": bson.M{"$exists": true}})
	}
	if keywordFilter := antennaKeywordFilter(antenna.Keywords, antenna.CaseSensitive); keywordFilter != nil {
		criteria = append(criteria, keywordFilter)
	}
	if excludeFilter := antennaExcludeKeywordFilter(antenna.ExcludeKeywords, antenna.CaseSensitive); excludeFilter != nil {
		criteria = append(criteria, excludeFilter)
	}
	userIDs, err := r.antennaUserIDs(ctx, antenna.Users)
	if err != nil {
		return nil, err
	}
	switch antenna.Source {
	case antennas.SourceUsers:
		criteria = append(criteria, bson.M{"authorId": bson.M{"$in": userIDs}})
	case antennas.SourceUsersBlacklist:
		if len(userIDs) > 0 {
			criteria = append(criteria, bson.M{"authorId": bson.M{"$nin": userIDs}})
		}
	}
	excludedAuthors := append(blocked, muted...)
	if antenna.LocalOnly || antenna.ExcludeBots {
		attributeFilter := bson.M{"deletedAt": nil}
		if antenna.LocalOnly {
			attributeFilter["host"] = nil
		}
		if antenna.ExcludeBots {
			attributeFilter["isBot"] = bson.M{"$ne": true}
		}
		allowed, err := r.actorIDs(ctx, attributeFilter)
		if err != nil {
			return nil, err
		}
		criteria = append(criteria, bson.M{"authorId": bson.M{"$in": allowed}})
	}
	filter := withExcludedAuthors(bson.M{"$and": criteria}, excludedAuthors)
	filter = withCreatedCursor(filter, after)
	return r.listNotesWithTimelineMutes(ctx, actorID, filter, limit, -1, muted)
}

func antennaKeywordFilter(groups [][]string, caseSensitive bool) bson.M {
	groupFilters := make(bson.A, 0, len(groups))
	for _, group := range groups {
		wordFilters := make(bson.A, 0, len(group))
		for _, keyword := range group {
			optionsValue := ""
			if !caseSensitive {
				optionsValue = "i"
			}
			pattern := bson.Regex{Pattern: regexp.QuoteMeta(keyword), Options: optionsValue}
			wordFilters = append(wordFilters, bson.M{"$or": bson.A{bson.M{"text": pattern}, bson.M{"contentWarning": pattern}}})
		}
		if len(wordFilters) > 0 {
			groupFilters = append(groupFilters, bson.M{"$and": wordFilters})
		}
	}
	if len(groupFilters) == 0 {
		return nil
	}
	return bson.M{"$or": groupFilters}
}

func antennaExcludeKeywordFilter(groups [][]string, caseSensitive bool) bson.M {
	include := antennaKeywordFilter(groups, caseSensitive)
	if include == nil {
		return nil
	}
	return bson.M{"$nor": bson.A{include}}
}

func (r *SalviaReader) antennaUserIDs(ctx context.Context, users []string) ([]string, error) {
	if len(users) == 0 {
		return []string{}, nil
	}
	conditions := bson.A{}
	for _, user := range users {
		user = strings.TrimSpace(user)
		if parsed, err := url.Parse(user); err == nil && parsed.IsAbs() && parsed.Host != "" {
			conditions = append(conditions, bson.M{"uri": user})
			continue
		}
		account := strings.TrimPrefix(user, "@")
		parts := strings.SplitN(account, "@", 2)
		condition := bson.M{"usernameLower": strings.ToLower(parts[0]), "deletedAt": nil}
		if len(parts) == 2 && parts[1] != "" {
			condition["host"] = strings.ToLower(parts[1])
		} else {
			condition["host"] = nil
		}
		conditions = append(conditions, condition)
	}
	return r.actorIDs(ctx, bson.M{"$or": conditions})
}

func (r *SalviaReader) actorIDs(ctx context.Context, filter bson.M) ([]string, error) {
	cursor, err := r.db.Collection("actors").Find(ctx, filter, options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []struct {
		ID string `bson:"_id"`
	}
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(docs))
	for _, doc := range docs {
		ids = append(ids, doc.ID)
	}
	return ids, nil
}

func (r *SalviaReader) CountUnreadNotifications(ctx context.Context, accountID, actorID string) (int64, error) {
	filter := bson.M{"recipientAccountId": accountID, "isRead": bson.M{"$ne": true}}
	if actorID != "" {
		filter["recipientActorId"] = actorID
	}
	return r.db.Collection("notifications").CountDocuments(ctx, filter)
}

func (r *SalviaReader) FindProfile(ctx context.Context, viewerActorID, actorID string) (*readmodel.Profile, error) {
	blockedViewer, err := r.actorBlocks(ctx, actorID, viewerActorID)
	if err != nil || blockedViewer {
		return nil, err
	}
	blockedByViewer, err := r.actorBlocks(ctx, viewerActorID, actorID)
	if err != nil {
		return nil, err
	}
	activeMute, err := r.mutes.FindActive(ctx, viewerActorID, actorID, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	mutedByViewer := activeMute != nil
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
	followStatus := ""
	var follow struct {
		Status string `bson:"status"`
	}
	err = r.db.Collection("follows").FindOne(ctx, bson.M{"followerId": viewerActorID, "followeeId": actorID, "deletedAt": nil}, options.FindOne().SetProjection(bson.M{"status": 1})).Decode(&follow)
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, err
	}
	if err == nil {
		followStatus = follow.Status
	}
	pinnedNotes, err := r.listPinnedNotes(ctx, viewerActorID, actor)
	if err != nil {
		return nil, err
	}
	var muteExpiresAt *time.Time
	if activeMute != nil {
		muteExpiresAt = activeMute.ExpiresAt
	}
	return &readmodel.Profile{Actor: actor, FollowersCount: int(followers), FollowingCount: int(following), FollowStatus: followStatus, BlockedByViewer: blockedByViewer, MutedByViewer: mutedByViewer, MuteExpiresAt: muteExpiresAt, PinnedNotes: pinnedNotes}, nil
}

func (r *SalviaReader) listPinnedNotes(ctx context.Context, viewerActorID string, actor *actors.Actor) ([]readmodel.Note, error) {
	if len(actor.FeaturedNoteIDs) == 0 {
		return []readmodel.Note{}, nil
	}
	visibility, err := r.visibleNoteFilter(ctx, viewerActorID)
	if err != nil {
		return nil, err
	}
	filter := bson.M{"$and": bson.A{
		bson.M{"_id": bson.M{"$in": actor.FeaturedNoteIDs}, "authorId": actor.ID, "deletedAt": nil},
		visibility,
	}}
	items, err := r.listNotes(ctx, viewerActorID, filter, len(actor.FeaturedNoteIDs), -1)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]readmodel.Note, len(items))
	for _, item := range items {
		byID[item.Note.ID] = item
	}
	ordered := make([]readmodel.Note, 0, len(items))
	for _, noteID := range actor.FeaturedNoteIDs {
		if item, ok := byID[noteID]; ok {
			ordered = append(ordered, item)
		}
	}
	return ordered, nil
}

func (r *SalviaReader) ListEmojis(ctx context.Context, query readmodel.EmojiListQuery) ([]emojis.Emoji, error) {
	filter := bson.M{"host": ""}
	if query.Remote {
		filter = bson.M{"host": bson.M{"$ne": ""}}
	}
	if host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(query.Host), ".")); host != "" {
		if !query.Remote {
			return []emojis.Emoji{}, nil
		}
		filter["host"] = host
	}
	if value := strings.TrimSpace(query.Query); value != "" {
		filter["name"] = bson.M{"$regex": regexp.QuoteMeta(value), "$options": "i"}
	}
	if query.After.ID != "" {
		cursorFilter := bson.M{"$or": bson.A{
			bson.M{"host": bson.M{"$gt": query.After.Host}},
			bson.M{"host": query.After.Host, "name": bson.M{"$gt": query.After.Name}},
			bson.M{"host": query.After.Host, "name": query.After.Name, "_id": bson.M{"$gt": query.After.ID}},
		}}
		filter = bson.M{"$and": bson.A{filter, cursorFilter}}
	}
	cursor, err := r.db.Collection("emojis").Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "host", Value: 1}, {Key: "name", Value: 1}, {Key: "_id", Value: 1}}).SetLimit(int64(query.Limit)))
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
	return r.listNotesWithTimelineMutes(ctx, viewerActorID, filter, limit, direction, nil)
}

func (r *SalviaReader) listNotesWithTimelineMutes(ctx context.Context, viewerActorID string, filter bson.M, limit, direction int, mutedActorIDs []string) ([]readmodel.Note, error) {
	docs, err := r.listNoteDocuments(ctx, filter, limit, direction, mutedActorIDs)
	if err != nil {
		return nil, err
	}
	return r.enrichNotes(ctx, viewerActorID, docs)
}

func (r *SalviaReader) listNoteDocuments(ctx context.Context, filter bson.M, limit, direction int, mutedActorIDs []string) ([]noteDocument, error) {
	pipeline := noteListPipeline(filter, limit, direction, mutedActorIDs)
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

func noteListPipeline(filter bson.M, limit, direction int, mutedActorIDs []string) mongo.Pipeline {
	unsetFields := bson.A{"activeAuthor"}
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
	}
	if len(mutedActorIDs) > 0 {
		unsetFields = append(unsetFields, "renoteTarget")
		pipeline = append(pipeline,
			bson.D{{Key: "$lookup", Value: bson.M{
				"from": "notes", "localField": "renoteId", "foreignField": "_id",
				"pipeline": bson.A{bson.M{"$project": bson.M{"authorId": 1}}}, "as": "renoteTarget",
			}}},
			bson.D{{Key: "$match", Value: bson.M{"renoteTarget.authorId": bson.M{"$nin": mutedActorIDs}}}},
		)
	}
	pipeline = append(pipeline,
		bson.D{{Key: "$limit", Value: int64(limit)}},
		bson.D{{Key: "$unset", Value: unsetFields}},
	)
	return pipeline
}

func (r *SalviaReader) mutedActorIDs(ctx context.Context, viewerActorID string) ([]string, error) {
	return r.mutes.ListActiveMuteeIDs(ctx, viewerActorID, time.Now().UTC())
}

func (r *SalviaReader) enrichNotes(ctx context.Context, viewerActorID string, docs []noteDocument) ([]readmodel.Note, error) {
	result := make([]readmodel.Note, 0, len(docs))
	noteIDs := make([]string, 0, len(docs)*4)
	seenNoteIDs := make(map[string]struct{}, len(docs)*4)
	for _, doc := range docs {
		for _, noteID := range []string{doc.ID, doc.ReplyID, doc.QuoteID, doc.RenoteID} {
			if noteID == "" {
				continue
			}
			if _, exists := seenNoteIDs[noteID]; exists {
				continue
			}
			seenNoteIDs[noteID] = struct{}{}
			noteIDs = append(noteIDs, noteID)
		}
	}
	repliesCount, err := r.replyCounts(ctx, noteIDs)
	if err != nil {
		return nil, err
	}
	blocked, err := r.blockedActorIDs(ctx, viewerActorID)
	if err != nil {
		return nil, err
	}
	visibility, err := r.visibleNoteFilter(ctx, viewerActorID)
	if err != nil {
		return nil, err
	}
	for _, doc := range docs {
		item := readmodel.Note{Note: *toNote(doc), RepliesCount: repliesCount[doc.ID]}
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
		item.Reply, err = r.findNoteReference(ctx, doc.ReplyID, visibility, viewerActorID, blocked, false)
		if err != nil {
			return nil, err
		}
		if item.Reply != nil {
			item.Reply.RepliesCount = repliesCount[doc.ReplyID]
		}
		item.Quote, err = r.findNoteReference(ctx, doc.QuoteID, visibility, viewerActorID, blocked, false)
		if err != nil {
			return nil, err
		}
		if item.Quote != nil {
			item.Quote.RepliesCount = repliesCount[doc.QuoteID]
		}
		item.Renote, err = r.findNoteReference(ctx, doc.RenoteID, visibility, viewerActorID, blocked, true)
		if err != nil {
			return nil, err
		}
		if item.Renote != nil {
			item.Renote.RepliesCount = repliesCount[doc.RenoteID]
		}
		result = append(result, item)
	}
	return result, nil
}

func (r *SalviaReader) replyCounts(ctx context.Context, noteIDs []string) (map[string]int, error) {
	result := make(map[string]int, len(noteIDs))
	if len(noteIDs) == 0 {
		return result, nil
	}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"replyId": bson.M{"$in": noteIDs}, "deletedAt": nil}}},
		{{Key: "$group", Value: bson.M{"_id": "$replyId", "count": bson.M{"$sum": 1}}}},
	}
	cursor, err := r.db.Collection("notes").Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var rows []struct {
		NoteID string `bson:"_id"`
		Count  int    `bson:"count"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.NoteID] = row.Count
	}
	return result, nil
}

func (r *SalviaReader) findNoteReference(ctx context.Context, noteID string, visibility bson.M, viewerActorID string, blockedActorIDs []string, includeReactions bool) (*readmodel.NoteReference, error) {
	// One nested reply or quote preserves the target's context without allowing cycles in API projections.
	return r.findNoteReferenceWithContext(ctx, noteID, visibility, viewerActorID, blockedActorIDs, true, includeReactions)
}

func (r *SalviaReader) findNoteReferenceWithContext(ctx context.Context, noteID string, visibility bson.M, viewerActorID string, blockedActorIDs []string, includeContext, includeReactions bool) (*readmodel.NoteReference, error) {
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
	reference := &readmodel.NoteReference{Note: *toNote(doc), Author: author}
	if includeReactions {
		reference.Reactions, err = r.reactionSummary(ctx, doc.ID, viewerActorID, blockedActorIDs)
		if err != nil {
			return nil, err
		}
	}
	if includeContext {
		reference.Reply, err = r.findNoteReferenceWithContext(ctx, doc.ReplyID, visibility, viewerActorID, blockedActorIDs, false, false)
		if err != nil {
			return nil, err
		}
		reference.Quote, err = r.findNoteReferenceWithContext(ctx, doc.QuoteID, visibility, viewerActorID, blockedActorIDs, false, false)
		if err != nil {
			return nil, err
		}
	}
	return reference, nil
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
	if err := r.hydrateActorEmojis(ctx, &actor); err != nil {
		return nil, err
	}
	return &actor, nil
}

func (r *SalviaReader) hydrateActorEmojis(ctx context.Context, actor *actors.Actor) error {
	actor.ResolvedEmojis = []emojis.Reference{}
	if len(actor.EmojiNames) == 0 {
		return nil
	}
	host := ""
	if actor.Host != nil {
		host = *actor.Host
	}
	cursor, err := r.db.Collection("emojis").Find(ctx, bson.M{"host": host, "name": bson.M{"$in": actor.EmojiNames}})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	var docs []emojiRecord
	if err := cursor.All(ctx, &docs); err != nil {
		return err
	}
	byName := make(map[string]emojis.Reference, len(docs))
	for _, doc := range docs {
		url := doc.PublicURL
		if url == "" {
			url = doc.OriginalURL
		}
		if url != "" {
			byName[doc.Name] = emojis.Reference{Name: doc.Name, URL: url, MediaType: doc.MediaType}
		}
	}
	for _, name := range actor.EmojiNames {
		if emoji, ok := byName[name]; ok {
			actor.ResolvedEmojis = append(actor.ResolvedEmojis, emoji)
		}
	}
	return nil
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

func (r *SalviaReader) actorBlocks(ctx context.Context, blockerID, blockeeID string) (bool, error) {
	if blockerID == blockeeID {
		return false, nil
	}
	count, err := r.db.Collection("blocks").CountDocuments(ctx, bson.M{"blockerId": blockerID, "blockeeId": blockeeID, "deletedAt": nil})
	return count > 0, err
}

func (r *SalviaReader) reactionSummary(ctx context.Context, noteID, viewerActorID string, blockedActorIDs []string) ([]readmodel.ReactionSummary, error) {
	match := bson.M{"noteId": noteID, "deletedAt": nil}
	if len(blockedActorIDs) > 0 {
		match["actorId"] = bson.M{"$nin": blockedActorIDs}
	}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: match}},
		{{Key: "$group", Value: bson.M{
			"_id": "$reaction", "count": bson.M{"$sum": 1}, "actors": bson.M{"$addToSet": "$actorId"},
			"emojiName": bson.M{"$max": "$emojiName"}, "emojiUrl": bson.M{"$max": "$emojiUrl"}, "emojiMediaType": bson.M{"$max": "$emojiMediaType"},
		}}},
		{{Key: "$sort", Value: bson.M{"_id": 1}}},
	}
	cursor, err := r.db.Collection("reactions").Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var rows []struct {
		Reaction       string   `bson:"_id"`
		Count          int      `bson:"count"`
		Actors         []string `bson:"actors"`
		EmojiName      string   `bson:"emojiName"`
		EmojiURL       string   `bson:"emojiUrl"`
		EmojiMediaType string   `bson:"emojiMediaType"`
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
		emoji, err := r.reactionEmoji(ctx, row.Reaction, row.EmojiName, row.EmojiURL, row.EmojiMediaType)
		if err != nil {
			return nil, err
		}
		result = append(result, readmodel.ReactionSummary{Reaction: row.Reaction, Count: row.Count, Reacted: reacted, Emoji: emoji})
	}
	return result, nil
}

func (r *SalviaReader) reactionEmoji(ctx context.Context, reaction, name, url, mediaType string) (*emojis.Reference, error) {
	if name != "" && url != "" {
		return &emojis.Reference{Name: name, URL: url, MediaType: mediaType}, nil
	}
	if len(reaction) < 5 || reaction[0] != ':' || reaction[len(reaction)-1] != ':' {
		return nil, nil
	}
	value := reaction[1 : len(reaction)-1]
	separator := strings.LastIndexByte(value, '@')
	if separator <= 0 || separator == len(value)-1 {
		return nil, nil
	}
	host := value[separator+1:]
	if host == "." {
		host = ""
	}
	var doc emojiRecord
	err := r.db.Collection("emojis").FindOne(ctx, bson.M{"host": host, "name": value[:separator]}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	resolvedURL := doc.PublicURL
	if resolvedURL == "" {
		resolvedURL = doc.OriginalURL
	}
	if resolvedURL == "" {
		return nil, nil
	}
	return &emojis.Reference{Name: doc.Name, URL: resolvedURL, MediaType: doc.MediaType}, nil
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
