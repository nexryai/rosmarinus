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

	"github.com/nexryai/rosmarinus/internal/domain/blocks"
)

type BlockRepository struct {
	collection *mongo.Collection
}

type blockDocument struct {
	ID                   string     `bson:"_id,omitempty"`
	BlockerID            string     `bson:"blockerId"`
	BlockeeID            string     `bson:"blockeeId"`
	BlockerURI           string     `bson:"blockerUri"`
	BlockeeURI           string     `bson:"blockeeUri"`
	BlockerHost          *string    `bson:"blockerHost"`
	BlockeeHost          *string    `bson:"blockeeHost"`
	CreatedAt            time.Time  `bson:"createdAt"`
	RemoteActivityID     string     `bson:"remoteActivityId,omitempty"`
	RemoteUndoActivityID string     `bson:"remoteUndoActivityId,omitempty"`
	DeletedAt            *time.Time `bson:"deletedAt,omitempty"`
}

func NewBlockRepository(db *mongo.Database) *BlockRepository {
	return &BlockRepository{collection: db.Collection("blocks")}
}

func (r *BlockRepository) Find(ctx context.Context, blockerID, blockeeID string) (*blocks.Block, error) {
	return r.findOne(ctx, bson.M{
		"blockerId": blockerID,
		"blockeeId": blockeeID,
		"deletedAt": nil,
	})
}

func (r *BlockRepository) Upsert(ctx context.Context, block blocks.Block) (*blocks.Block, error) {
	if block.BlockerID == "" || block.BlockeeID == "" {
		return nil, fmt.Errorf("blockerId and blockeeId are required")
	}
	if block.ID == "" {
		block.ID = blockID(block.BlockerID, block.BlockeeID)
	}
	if block.CreatedAt.IsZero() {
		block.CreatedAt = time.Now().UTC()
	}
	doc := fromBlock(block)
	_, err := r.collection.UpdateOne(ctx, bson.M{
		"blockerId": doc.BlockerID,
		"blockeeId": doc.BlockeeID,
	}, bson.M{
		"$setOnInsert": doc,
		"$set": bson.M{
			"deletedAt": nil,
		},
	}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return nil, err
	}
	return r.Find(ctx, block.BlockerID, block.BlockeeID)
}

func (r *BlockRepository) Delete(ctx context.Context, blockerID, blockeeID, remoteUndoActivityID string) error {
	now := time.Now().UTC()
	update := bson.M{
		"deletedAt": now,
	}
	if remoteUndoActivityID != "" {
		update["remoteUndoActivityId"] = remoteUndoActivityID
	}
	_, err := r.collection.UpdateOne(ctx, bson.M{
		"blockerId": blockerID,
		"blockeeId": blockeeID,
		"deletedAt": nil,
	}, bson.M{"$set": update})
	return err
}

func (r *BlockRepository) findOne(ctx context.Context, filter bson.M) (*blocks.Block, error) {
	var doc blockDocument
	if err := r.collection.FindOne(ctx, filter).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return toBlock(doc), nil
}

func fromBlock(block blocks.Block) blockDocument {
	return blockDocument{
		ID:                   block.ID,
		BlockerID:            block.BlockerID,
		BlockeeID:            block.BlockeeID,
		BlockerURI:           block.BlockerURI,
		BlockeeURI:           block.BlockeeURI,
		BlockerHost:          block.BlockerHost,
		BlockeeHost:          block.BlockeeHost,
		CreatedAt:            block.CreatedAt,
		RemoteActivityID:     block.RemoteActivityID,
		RemoteUndoActivityID: block.RemoteUndoActivityID,
	}
}

func toBlock(doc blockDocument) *blocks.Block {
	return &blocks.Block{
		ID:                   doc.ID,
		BlockerID:            doc.BlockerID,
		BlockeeID:            doc.BlockeeID,
		BlockerURI:           doc.BlockerURI,
		BlockeeURI:           doc.BlockeeURI,
		BlockerHost:          doc.BlockerHost,
		BlockeeHost:          doc.BlockeeHost,
		CreatedAt:            doc.CreatedAt,
		RemoteActivityID:     doc.RemoteActivityID,
		RemoteUndoActivityID: doc.RemoteUndoActivityID,
	}
}

func blockID(blockerID, blockeeID string) string {
	sum := sha256.Sum256([]byte(blockerID + "\x00" + blockeeID))
	return "block_" + hex.EncodeToString(sum[:])[:24]
}
