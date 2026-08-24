package mongostore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/nexryai/rosmarinus/internal/domain/emojis"
)

type EmojiRepository struct {
	collection *mongo.Collection
}

type emojiRecord struct {
	ID              string     `bson:"_id"`
	Host            string     `bson:"host"`
	Name            string     `bson:"name"`
	URI             string     `bson:"uri,omitempty"`
	OriginalURL     string     `bson:"originalUrl"`
	PublicURL       string     `bson:"publicUrl"`
	MediaType       string     `bson:"mediaType,omitempty"`
	RemoteUpdatedAt *time.Time `bson:"remoteUpdatedAt,omitempty"`
	CreatedAt       time.Time  `bson:"createdAt"`
	UpdatedAt       time.Time  `bson:"updatedAt"`
}

func NewEmojiRepository(db *mongo.Database) *EmojiRepository {
	return &EmojiRepository{collection: db.Collection("emojis")}
}

func (r *EmojiRepository) UpsertRemote(ctx context.Context, emoji emojis.Emoji) (*emojis.Emoji, error) {
	emoji.Host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(emoji.Host), "."))
	emoji.Name = strings.TrimSpace(emoji.Name)
	if emoji.Host == "" || emoji.Name == "" || emoji.OriginalURL == "" {
		return nil, fmt.Errorf("remote emoji host, name, and original URL are required")
	}
	now := time.Now().UTC()
	if emoji.ID == "" {
		emoji.ID = emojiID(emoji.Host, emoji.Name)
	}
	if emoji.CreatedAt.IsZero() {
		emoji.CreatedAt = now
	}
	emoji.UpdatedAt = now
	doc := fromEmoji(emoji)
	key := bson.M{"host": doc.Host, "name": doc.Name}
	_, err := r.collection.UpdateOne(ctx, key, bson.M{"$setOnInsert": doc}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return nil, err
	}
	refreshConditions := bson.A{
		bson.M{"updatedAt": nil},
		bson.M{"originalUrl": bson.M{"$ne": doc.OriginalURL}},
	}
	if doc.URI != "" {
		refreshConditions = append(refreshConditions, bson.M{"uri": bson.M{"$in": bson.A{"", nil}}})
	}
	if doc.RemoteUpdatedAt != nil {
		refreshConditions = append(refreshConditions,
			bson.M{"remoteUpdatedAt": bson.M{"$in": bson.A{nil}}},
			bson.M{"remoteUpdatedAt": bson.M{"$lt": doc.RemoteUpdatedAt}},
		)
	}
	_, err = r.collection.UpdateOne(ctx, bson.M{
		"host": doc.Host, "name": doc.Name, "$or": refreshConditions,
	}, bson.M{"$set": bson.M{
		"uri": doc.URI, "originalUrl": doc.OriginalURL, "publicUrl": doc.PublicURL,
		"mediaType": doc.MediaType, "remoteUpdatedAt": doc.RemoteUpdatedAt, "updatedAt": now,
	}})
	if err != nil {
		return nil, err
	}
	return r.findOne(ctx, key)
}

func (r *EmojiRepository) findOne(ctx context.Context, filter bson.M) (*emojis.Emoji, error) {
	var doc emojiRecord
	if err := r.collection.FindOne(ctx, filter).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &emojis.Emoji{
		ID: doc.ID, Host: doc.Host, Name: doc.Name, URI: doc.URI,
		OriginalURL: doc.OriginalURL, MediaType: doc.MediaType,
		RemoteUpdatedAt: doc.RemoteUpdatedAt, CreatedAt: doc.CreatedAt, UpdatedAt: doc.UpdatedAt,
	}, nil
}

func fromEmoji(emoji emojis.Emoji) emojiRecord {
	return emojiRecord{
		ID: emoji.ID, Host: emoji.Host, Name: emoji.Name, URI: emoji.URI,
		OriginalURL: emoji.OriginalURL, PublicURL: emoji.OriginalURL,
		MediaType: emoji.MediaType, RemoteUpdatedAt: emoji.RemoteUpdatedAt,
		CreatedAt: emoji.CreatedAt, UpdatedAt: emoji.UpdatedAt,
	}
}

func emojiID(host, name string) string {
	sum := sha256.Sum256([]byte(host + "\x00" + name))
	return "emoji_" + hex.EncodeToString(sum[:])[:24]
}
