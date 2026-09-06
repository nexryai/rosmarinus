package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const migrationName = "actor_ids_objectid_hex_v1"

type reference struct {
	collection string
	field      string
}

var actorReferences = []reference{
	{collection: "notes", field: "authorId"},
	{collection: "polls", field: "authorId"},
	{collection: "poll_votes", field: "actorId"},
	{collection: "reactions", field: "actorId"},
	{collection: "follows", field: "followerId"},
	{collection: "follows", field: "followeeId"},
	{collection: "blocks", field: "blockerId"},
	{collection: "blocks", field: "blockeeId"},
	{collection: "notifications", field: "recipientActorId"},
	{collection: "notifications", field: "sourceActorId"},
	{collection: "media", field: "ownerActorId"},
	{collection: "api_idempotency_receipts", field: "actorId"},
	{collection: "ui_settings", field: "selectedActorId"},
	{collection: "abuse_reports", field: "targetUserId"},
	{collection: "abuse_reports", field: "reporterId"},
}

type idMapping struct {
	Old string `bson:"old"`
	New string `bson:"new"`
}

func main() {
	apply := flag.Bool("apply", false, "apply the migration transaction")
	flag.Parse()

	uri := strings.TrimSpace(os.Getenv("MONGO_URI"))
	if uri == "" {
		log.Fatal("MONGO_URI is required")
	}
	database := strings.TrimSpace(os.Getenv("MONGO_DATABASE"))
	if database == "" {
		database = "rosmarinus"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = client.Disconnect(context.Background()) }()
	if err := client.Ping(ctx, nil); err != nil {
		log.Fatal(err)
	}
	db := client.Database(database)

	ids, actorCount, err := collectActorIDs(ctx, db)
	if err != nil {
		log.Fatal(err)
	}
	mappings := newMappings(ids)
	log.Printf("migration plan: actors=%d distinct_actor_references=%d ids_requiring_migration=%d", actorCount, len(ids), len(mappings))
	if !*apply {
		log.Print("dry run complete; pass --apply to migrate")
		return
	}
	if len(mappings) == 0 {
		if err := verify(ctx, db, nil); err != nil {
			log.Fatal(err)
		}
		log.Print("all Actor IDs and references are already ObjectID strings")
		return
	}
	if err := ensureMigrationCollections(ctx, db); err != nil {
		log.Fatal(err)
	}
	if err := migrate(ctx, client, db, mappings); err != nil {
		log.Fatal(err)
	}
	if err := verify(ctx, db, mappings); err != nil {
		log.Fatal(err)
	}
	log.Printf("migration complete: migrated_ids=%d", len(mappings))
}

func collectActorIDs(ctx context.Context, db *mongo.Database) (map[string]struct{}, int64, error) {
	ids := make(map[string]struct{})
	actors := db.Collection("actors")
	cursor, err := actors.Find(ctx, bson.M{}, options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)
	var actorCount int64
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return nil, 0, err
		}
		id, ok := doc["_id"].(string)
		if !ok || strings.TrimSpace(id) == "" {
			return nil, 0, fmt.Errorf("actors contains a non-string or empty _id")
		}
		ids[id] = struct{}{}
		actorCount++
	}
	if err := cursor.Err(); err != nil {
		return nil, 0, err
	}

	allReferences := append([]reference{{collection: "actor_settings", field: "_id"}}, actorReferences...)
	for _, ref := range allReferences {
		var values []string
		err := db.Collection(ref.collection).Distinct(ctx, ref.field, bson.M{
			ref.field: bson.M{"$type": "string", "$ne": ""},
		}).Decode(&values)
		if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
			return nil, 0, fmt.Errorf("collect %s.%s: %w", ref.collection, ref.field, err)
		}
		for _, id := range values {
			ids[id] = struct{}{}
		}
	}
	return ids, actorCount, nil
}

func newMappings(ids map[string]struct{}) []idMapping {
	used := make(map[string]struct{}, len(ids)*2)
	for id := range ids {
		used[id] = struct{}{}
	}
	oldIDs := make([]string, 0, len(ids))
	for id := range ids {
		if _, err := bson.ObjectIDFromHex(id); err != nil {
			oldIDs = append(oldIDs, id)
		}
	}
	sort.Strings(oldIDs)
	mappings := make([]idMapping, 0, len(oldIDs))
	for _, oldID := range oldIDs {
		for {
			newID := bson.NewObjectID().Hex()
			if _, collision := used[newID]; collision {
				continue
			}
			used[newID] = struct{}{}
			mappings = append(mappings, idMapping{Old: oldID, New: newID})
			break
		}
	}
	return mappings
}

func ensureMigrationCollections(ctx context.Context, db *mongo.Database) error {
	if err := db.CreateCollection(ctx, "migration_audits"); err != nil {
		var commandError mongo.CommandError
		if !errors.As(err, &commandError) || commandError.Code != 48 {
			return fmt.Errorf("create migration audit collection: %w", err)
		}
	}
	_, err := db.Collection("actors").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "legacyIds", Value: 1}},
		Options: options.Index().SetName("uniq_actors_legacy_ids").SetUnique(true).SetSparse(true),
	})
	return err
}

func migrate(ctx context.Context, client *mongo.Client, db *mongo.Database, mappings []idMapping) error {
	session, err := client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(ctx)
	_, err = session.WithTransaction(ctx, func(tx context.Context) (any, error) {
		for _, mapping := range mappings {
			if err := migrateActor(ctx, tx, db.Collection("actors"), mapping); err != nil {
				return nil, err
			}
			if err := migrateDocumentID(ctx, tx, db.Collection("actor_settings"), mapping); err != nil {
				return nil, err
			}
			for _, ref := range actorReferences {
				if _, err := db.Collection(ref.collection).UpdateMany(tx,
					bson.M{ref.field: mapping.Old}, bson.M{"$set": bson.M{ref.field: mapping.New}}); err != nil {
					return nil, fmt.Errorf("update %s.%s: %w", ref.collection, ref.field, err)
				}
			}
		}
		_, err := db.Collection("migration_audits").InsertOne(tx, bson.M{
			"_id": migrationName + "_" + bson.NewObjectID().Hex(), "name": migrationName,
			"mappings": mappings, "completedAt": time.Now().UTC(),
		})
		return nil, err
	})
	return err
}

func migrateActor(ctx, tx context.Context, collection *mongo.Collection, mapping idMapping) error {
	var doc bson.M
	err := collection.FindOne(tx, bson.M{"_id": mapping.Old}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil
	}
	if err != nil {
		return err
	}
	legacy := bson.A{mapping.Old}
	if existing, ok := doc["legacyIds"].(bson.A); ok {
		legacy = append(existing, mapping.Old)
	}
	doc["_id"] = mapping.New
	doc["legacyIds"] = uniqueStrings(legacy)
	if _, err := collection.DeleteOne(tx, bson.M{"_id": mapping.Old}); err != nil {
		return err
	}
	if _, err := collection.InsertOne(tx, doc); err != nil {
		return fmt.Errorf("replace actor document: %w", err)
	}
	return nil
}

func migrateDocumentID(ctx, tx context.Context, collection *mongo.Collection, mapping idMapping) error {
	var doc bson.M
	err := collection.FindOne(tx, bson.M{"_id": mapping.Old}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil
	}
	if err != nil {
		return err
	}
	doc["_id"] = mapping.New
	if _, err := collection.DeleteOne(tx, bson.M{"_id": mapping.Old}); err != nil {
		return err
	}
	_, err = collection.InsertOne(tx, doc)
	return err
}

func uniqueStrings(values bson.A) bson.A {
	seen := make(map[string]struct{}, len(values))
	result := make(bson.A, 0, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok || text == "" {
			continue
		}
		if _, exists := seen[text]; exists {
			continue
		}
		seen[text] = struct{}{}
		result = append(result, text)
	}
	return result
}

func verify(ctx context.Context, db *mongo.Database, mappings []idMapping) error {
	ids, _, err := collectActorIDs(ctx, db)
	if err != nil {
		return err
	}
	for id := range ids {
		if _, err := bson.ObjectIDFromHex(id); err != nil {
			return fmt.Errorf("verification found a non-ObjectID Actor reference")
		}
	}
	for _, mapping := range mappings {
		for _, ref := range actorReferences {
			count, err := db.Collection(ref.collection).CountDocuments(ctx, bson.M{ref.field: mapping.Old})
			if err != nil {
				return err
			}
			if count != 0 {
				return fmt.Errorf("verification found stale %s.%s references", ref.collection, ref.field)
			}
		}
	}
	return nil
}
