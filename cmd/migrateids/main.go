package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/nexryai/rosmarinus/internal/idgen"
	mongostore "github.com/nexryai/rosmarinus/internal/store/mongo"
)

const migrationName = "database_ids_objectid_hex_v1"

type fieldRef struct {
	collection string
	field      string
}

type idMapping struct {
	Old any    `bson:"old"`
	New string `bson:"new"`
}

type migrationPlan struct {
	accounts map[string]idMapping
	actors   map[string]idMapping
	notes    map[string]idMapping
	media    map[string]idMapping
	ids      map[string]map[string]idMapping
}

var accountRefs = []fieldRef{
	{"actors", "ownerAccountId"},
	{"notifications", "recipientAccountId"},
	{"sessions", "accountId"},
	{"webauthn_challenges", "accountId"},
	{"api_idempotency_receipts", "accountId"},
	{"ui_settings", "_id"},
	{"actor_settings", "accountId"},
}

var actorRefs = []fieldRef{
	{"notes", "authorId"},
	{"polls", "authorId"},
	{"poll_votes", "actorId"},
	{"reactions", "actorId"},
	{"follows", "followerId"},
	{"follows", "followeeId"},
	{"blocks", "blockerId"},
	{"blocks", "blockeeId"},
	{"notifications", "recipientActorId"},
	{"notifications", "sourceActorId"},
	{"media", "ownerActorId"},
	{"api_idempotency_receipts", "actorId"},
	{"ui_settings", "selectedActorId"},
	{"actor_settings", "_id"},
	{"abuse_reports", "targetUserId"},
	{"abuse_reports", "reporterId"},
}

var noteRefs = []fieldRef{
	{"notes", "replyId"},
	{"notes", "quoteId"},
	{"notes", "renoteId"},
	{"polls", "noteId"},
	{"poll_votes", "noteId"},
	{"reactions", "noteId"},
	{"notifications", "noteId"},
	{"actors", "featuredNoteIds"},
}

var knownCollections = []string{
	"accounts", "actors", "notes", "polls", "poll_votes", "reactions", "emojis", "blocks",
	"abuse_reports", "follows", "api_idempotency_receipts", "inbox_activity_receipts",
	"notifications", "media", "instances", "sessions", "webauthn_challenges", "ui_settings",
	"actor_settings", "migration_audits", "media_fs.files", "media_fs.chunks",
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
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
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
	plan, err := buildPlan(ctx, db)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("migration plan: ids_requiring_migration=%d", plan.count())
	if !*apply {
		if err := verifyReferences(ctx, db, false); err != nil {
			log.Printf("verification before migration: %v", err)
		}
		log.Print("dry run complete; pass --apply to migrate")
		return
	}
	if err := applyPlan(ctx, client, db, plan); err != nil {
		log.Fatal(err)
	}
	if err := mongostore.BootstrapIndexes(ctx, db); err != nil {
		log.Fatal(err)
	}
	if err := verify(ctx, db); err != nil {
		log.Fatal(err)
	}
	log.Printf("migration complete: migrated_ids=%d", plan.count())
}

func buildPlan(ctx context.Context, db *mongo.Database) (*migrationPlan, error) {
	plan := &migrationPlan{ids: make(map[string]map[string]idMapping)}
	var err error
	if plan.accounts, err = buildIdentityMappings(ctx, db, "accounts", accountRefs); err != nil {
		return nil, err
	}
	if plan.actors, err = buildIdentityMappings(ctx, db, "actors", actorRefs); err != nil {
		return nil, err
	}
	if plan.notes, err = buildIdentityMappings(ctx, db, "notes", noteRefs); err != nil {
		return nil, err
	}
	mediaRefs := []fieldRef{{"media_fs.files", "_id"}, {"media_fs.chunks", "files_id"}}
	if plan.media, err = buildIdentityMappings(ctx, db, "media", mediaRefs); err != nil {
		return nil, err
	}

	sharedIDs := map[string]bool{"accounts": true, "actors": true, "notes": true, "media": true, "ui_settings": true, "actor_settings": true, "media_fs.files": true}
	for _, collection := range knownCollections {
		if sharedIDs[collection] {
			continue
		}
		values, err := distinctValues(ctx, db.Collection(collection), "_id")
		if err != nil {
			return nil, err
		}
		values, err = appendReservedLegacyIDs(ctx, db.Collection(collection), values)
		if err != nil {
			return nil, err
		}
		plan.ids[collection], err = newMappings(values)
		if err != nil {
			return nil, fmt.Errorf("plan %s ids: %w", collection, err)
		}
	}
	return plan, nil
}

func buildIdentityMappings(ctx context.Context, db *mongo.Database, collection string, refs []fieldRef) (map[string]idMapping, error) {
	values, err := distinctValues(ctx, db.Collection(collection), "_id")
	if err != nil {
		return nil, err
	}
	for _, ref := range refs {
		found, err := distinctValues(ctx, db.Collection(ref.collection), ref.field)
		if err != nil {
			return nil, err
		}
		values = append(values, found...)
	}
	values, err = appendReservedLegacyIDs(ctx, db.Collection(collection), values)
	if err != nil {
		return nil, err
	}
	return newMappings(values)
}

func appendReservedLegacyIDs(ctx context.Context, collection *mongo.Collection, values []any) ([]any, error) {
	legacyIDs, err := distinctValues(ctx, collection, "legacyIds")
	if err != nil {
		return nil, err
	}
	for _, value := range legacyIDs {
		if text, ok := value.(string); ok && isObjectIDString(text) {
			values = append(values, text)
		}
	}
	return values, nil
}

func distinctValues(ctx context.Context, collection *mongo.Collection, field string) ([]any, error) {
	var values []any
	err := collection.Distinct(ctx, field, bson.M{field: bson.M{"$exists": true, "$nin": bson.A{"", nil}}}).Decode(&values)
	if errors.Is(err, mongo.ErrNoDocuments) || isNamespaceNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("collect %s.%s: %w", collection.Name(), field, err)
	}
	return values, nil
}

func newMappings(values []any) (map[string]idMapping, error) {
	unique := make(map[string]any)
	used := make(map[string]string)
	for _, value := range values {
		key, err := valueKey(value)
		if err != nil {
			return nil, err
		}
		unique[key] = value
		if text, ok := value.(string); ok && isObjectIDString(text) {
			used[text] = key
		}
	}
	keys := make([]string, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make(map[string]idMapping)
	for _, key := range keys {
		value := unique[key]
		if text, ok := value.(string); ok && isObjectIDString(text) {
			continue
		}
		candidate := ""
		if objectID, ok := value.(bson.ObjectID); ok {
			candidate = objectID.Hex()
		}
		if owner, collision := used[candidate]; candidate == "" || (collision && owner != key) {
			for {
				candidate = bson.NewObjectID().Hex()
				if _, collision := used[candidate]; !collision {
					break
				}
			}
		}
		used[candidate] = key
		result[key] = idMapping{Old: value, New: candidate}
	}
	return result, nil
}

func applyPlan(ctx context.Context, client *mongo.Client, db *mongo.Database, plan *migrationPlan) error {
	session, err := client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(ctx)
	_, err = session.WithTransaction(ctx, func(tx context.Context) (any, error) {
		for _, item := range []struct {
			mappings map[string]idMapping
			refs     []fieldRef
		}{{plan.accounts, accountRefs}, {plan.actors, actorRefs}, {plan.notes, noteRefs}} {
			if err := rewriteReferences(tx, db, item.mappings, item.refs); err != nil {
				return nil, err
			}
		}
		if err := rewriteReferences(tx, db, plan.media, []fieldRef{{"media_fs.chunks", "files_id"}}); err != nil {
			return nil, err
		}

		if err := rekeyCollection(tx, db.Collection("accounts"), plan.accounts, false); err != nil {
			return nil, err
		}
		if err := rekeyCollection(tx, db.Collection("actors"), plan.actors, true); err != nil {
			return nil, err
		}
		if err := rekeyCollection(tx, db.Collection("notes"), plan.notes, true); err != nil {
			return nil, err
		}
		if err := rekeyCollection(tx, db.Collection("media"), plan.media, true); err != nil {
			return nil, err
		}
		if err := rekeyCollection(tx, db.Collection("ui_settings"), plan.accounts, false); err != nil {
			return nil, err
		}
		if err := rekeyCollection(tx, db.Collection("actor_settings"), plan.actors, false); err != nil {
			return nil, err
		}
		if err := rekeyCollection(tx, db.Collection("media_fs.files"), plan.media, false); err != nil {
			return nil, err
		}

		if err := backfillPolls(tx, db, plan); err != nil {
			return nil, err
		}
		if err := backfillActivityReceipts(tx, db, plan.ids["inbox_activity_receipts"]); err != nil {
			return nil, err
		}
		for collection, mappings := range plan.ids {
			if collection == "polls" || collection == "poll_votes" || collection == "inbox_activity_receipts" {
				continue
			}
			if err := rekeyCollection(tx, db.Collection(collection), mappings, legacyLookupCollection(collection)); err != nil {
				return nil, err
			}
		}
		if err := backfillPollVotes(tx, db, plan.ids["poll_votes"]); err != nil {
			return nil, err
		}
		auditID, err := idgen.NewUniqueObjectID(tx, func(ctx context.Context, id string) (bool, error) {
			count, err := db.Collection("migration_audits").CountDocuments(ctx, bson.M{"$or": bson.A{
				bson.M{"_id": id}, bson.M{"legacyIds": id},
			}})
			return count != 0, err
		})
		if err != nil {
			return nil, err
		}
		_, err = db.Collection("migration_audits").InsertOne(tx, bson.M{
			"_id": auditID, "name": migrationName, "migratedIds": plan.count(), "completedAt": time.Now().UTC(),
		})
		return nil, err
	})
	return err
}

func rewriteReferences(ctx context.Context, db *mongo.Database, mappings map[string]idMapping, refs []fieldRef) error {
	writesByCollection := make(map[string][]mongo.WriteModel)
	for _, mapping := range mappings {
		for _, ref := range refs {
			if ref.field == "_id" {
				continue
			}
			if ref.collection == "actors" && ref.field == "featuredNoteIds" {
				pipeline := mongo.Pipeline{{{Key: "$set", Value: bson.M{ref.field: bson.M{"$map": bson.M{
					"input": "$" + ref.field, "as": "id", "in": bson.M{"$cond": bson.A{
						bson.M{"$eq": bson.A{"$$id", mapping.Old}}, mapping.New, "$$id",
					}},
				}}}}}}
				writesByCollection[ref.collection] = append(writesByCollection[ref.collection], mongo.NewUpdateManyModel().
					SetFilter(bson.M{ref.field: mapping.Old}).
					SetUpdate(pipeline))
				continue
			}
			writesByCollection[ref.collection] = append(writesByCollection[ref.collection], mongo.NewUpdateManyModel().
				SetFilter(bson.M{ref.field: mapping.Old}).
				SetUpdate(bson.M{"$set": bson.M{ref.field: mapping.New}}))
		}
	}
	collections := make([]string, 0, len(writesByCollection))
	for collection := range writesByCollection {
		collections = append(collections, collection)
	}
	sort.Strings(collections)
	for _, name := range collections {
		if _, err := db.Collection(name).BulkWrite(ctx, writesByCollection[name], options.BulkWrite().SetOrdered(false)); err != nil && !isNamespaceNotFound(err) {
			return fmt.Errorf("rewrite references in %s: %w", name, err)
		}
	}
	return nil
}

func rekeyCollection(ctx context.Context, collection *mongo.Collection, mappings map[string]idMapping, preserveLegacy bool) error {
	if len(mappings) == 0 {
		return nil
	}
	oldIDs := make([]any, 0, len(mappings))
	for _, mapping := range mappings {
		oldIDs = append(oldIDs, mapping.Old)
	}
	cursor, err := collection.Find(ctx, bson.M{"_id": bson.M{"$in": oldIDs}})
	if isNamespaceNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	documents := make([]bson.M, 0, len(mappings))
	originalIDs := make([]any, 0, len(mappings))
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return err
		}
		oldID := doc["_id"]
		key, err := valueKey(oldID)
		if err != nil {
			return err
		}
		mapping, exists := mappings[key]
		if !exists {
			return fmt.Errorf("missing %s id mapping for %v", collection.Name(), oldID)
		}
		doc["_id"] = mapping.New
		if preserveLegacy {
			legacyID := legacyString(oldID)
			if legacyID != mapping.New {
				doc["legacyIds"] = appendUniqueString(doc["legacyIds"], legacyID)
			}
		}
		documents = append(documents, doc)
		originalIDs = append(originalIDs, oldID)
	}
	if err := cursor.Err(); err != nil {
		return err
	}
	return replaceDocuments(ctx, collection, originalIDs, documents)
}

func backfillPolls(ctx context.Context, db *mongo.Database, plan *migrationPlan) error {
	collection := db.Collection("polls")
	cursor, err := collection.Find(ctx, bson.M{})
	if isNamespaceNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	originalIDs := make([]any, 0)
	documents := make([]bson.M, 0)
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return err
		}
		oldID := doc["_id"]
		noteID := doc["noteId"]
		if noteID == nil || noteID == "" {
			noteID = oldID
		}
		doc["noteId"] = mappedValue(plan.notes, noteID)
		newID := mappedValue(plan.ids["polls"], oldID)
		doc["_id"] = newID
		originalIDs = append(originalIDs, oldID)
		documents = append(documents, doc)
	}
	if err := cursor.Err(); err != nil {
		return err
	}
	return replaceDocuments(ctx, collection, originalIDs, documents)
}

func backfillActivityReceipts(ctx context.Context, db *mongo.Database, mappings map[string]idMapping) error {
	collection := db.Collection("inbox_activity_receipts")
	cursor, err := collection.Find(ctx, bson.M{})
	if isNamespaceNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	originalIDs := make([]any, 0)
	documents := make([]bson.M, 0)
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return err
		}
		oldID := doc["_id"]
		if _, exists := doc["activityId"]; !exists {
			doc["activityId"] = legacyString(oldID)
		}
		doc["_id"] = mappedValue(mappings, oldID)
		originalIDs = append(originalIDs, oldID)
		documents = append(documents, doc)
	}
	if err := cursor.Err(); err != nil {
		return err
	}
	return replaceDocuments(ctx, collection, originalIDs, documents)
}

func backfillPollVotes(ctx context.Context, db *mongo.Database, mappings map[string]idMapping) error {
	polls := make(map[string]bool)
	pollCursor, err := db.Collection("polls").Find(ctx, bson.M{}, options.Find().SetProjection(bson.M{"noteId": 1, "multiple": 1}))
	if err == nil {
		defer pollCursor.Close(ctx)
		for pollCursor.Next(ctx) {
			var doc struct {
				NoteID   string `bson:"noteId"`
				Multiple bool   `bson:"multiple"`
			}
			if err := pollCursor.Decode(&doc); err != nil {
				return err
			}
			polls[doc.NoteID] = doc.Multiple
		}
	} else if !isNamespaceNotFound(err) {
		return err
	}
	collection := db.Collection("poll_votes")
	cursor, err := collection.Find(ctx, bson.M{})
	if isNamespaceNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	originalIDs := make([]any, 0)
	documents := make([]bson.M, 0)
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return err
		}
		oldID := doc["_id"]
		noteID, _ := doc["noteId"].(string)
		actorID, _ := doc["actorId"].(string)
		choice := numericInt(doc["choice"])
		multiple := polls[noteID]
		doc["multiple"] = multiple
		doc["scopeKey"] = voteScope(noteID, actorID, choice, multiple)
		doc["_id"] = mappedValue(mappings, oldID)
		originalIDs = append(originalIDs, oldID)
		documents = append(documents, doc)
	}
	if err := cursor.Err(); err != nil {
		return err
	}
	return replaceDocuments(ctx, collection, originalIDs, documents)
}

func replaceDocuments(ctx context.Context, collection *mongo.Collection, originalIDs []any, documents []bson.M) error {
	if len(originalIDs) != len(documents) {
		return fmt.Errorf("replace %s documents: id and document counts differ", collection.Name())
	}
	changedIDs := make([]any, 0, len(originalIDs))
	changedDocuments := make([]any, 0, len(documents))
	unchanged := make([]mongo.WriteModel, 0, len(documents))
	for index, doc := range documents {
		oldID := originalIDs[index]
		newID := doc["_id"]
		if oldText, ok := oldID.(string); ok && oldText == newID {
			unchanged = append(unchanged, mongo.NewReplaceOneModel().SetFilter(bson.M{"_id": oldID}).SetReplacement(doc))
			continue
		}
		changedIDs = append(changedIDs, oldID)
		changedDocuments = append(changedDocuments, doc)
	}
	if len(changedIDs) != 0 {
		if _, err := collection.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": changedIDs}}); err != nil {
			return fmt.Errorf("delete legacy %s documents: %w", collection.Name(), err)
		}
		if _, err := collection.InsertMany(ctx, changedDocuments); err != nil {
			return fmt.Errorf("insert migrated %s documents: %w", collection.Name(), err)
		}
	}
	if len(unchanged) != 0 {
		if _, err := collection.BulkWrite(ctx, unchanged, options.BulkWrite().SetOrdered(false)); err != nil {
			return fmt.Errorf("replace unchanged-id %s documents: %w", collection.Name(), err)
		}
	}
	return nil
}

func verify(ctx context.Context, db *mongo.Database) error {
	for _, name := range knownCollections {
		count, err := db.Collection(name).CountDocuments(ctx, bson.M{"$nor": bson.A{
			bson.M{"_id": bson.M{"$type": "string", "$regex": "^[0-9a-f]{24}$"}},
		}})
		if isNamespaceNotFound(err) {
			continue
		}
		if err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("%s contains %d non-ObjectID _id values", name, count)
		}
	}
	for _, required := range []fieldRef{
		{"polls", "noteId"},
		{"poll_votes", "scopeKey"},
		{"poll_votes", "multiple"},
		{"inbox_activity_receipts", "activityId"},
	} {
		count, err := db.Collection(required.collection).CountDocuments(ctx, bson.M{"$or": bson.A{
			bson.M{required.field: bson.M{"$exists": false}}, bson.M{required.field: ""},
		}})
		if isNamespaceNotFound(err) {
			continue
		}
		if err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("%s contains %d documents without %s", required.collection, count, required.field)
		}
	}
	return verifyReferences(ctx, db, true)
}

func verifyReferences(ctx context.Context, db *mongo.Database, requireAll bool) error {
	refs := append(append(append([]fieldRef{}, accountRefs...), actorRefs...), noteRefs...)
	refs = append(refs, fieldRef{"media_fs.chunks", "files_id"})
	for _, ref := range refs {
		if ref.field == "_id" {
			continue
		}
		values, err := distinctValues(ctx, db.Collection(ref.collection), ref.field)
		if err != nil {
			return err
		}
		for _, value := range values {
			text, ok := value.(string)
			if ok && isObjectIDString(text) {
				continue
			}
			label := "non-ObjectID"
			if !requireAll {
				label = "legacy"
			}
			return fmt.Errorf("%s.%s contains a %s reference %v", ref.collection, ref.field, label, value)
		}
	}
	return nil
}

func mappedValue(mappings map[string]idMapping, value any) string {
	key, err := valueKey(value)
	if err == nil {
		if mapping, exists := mappings[key]; exists {
			return mapping.New
		}
	}
	return legacyString(value)
}

func valueKey(value any) (string, error) {
	switch value := value.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("empty id")
		}
		return "string:" + value, nil
	case bson.ObjectID:
		return "objectid:" + value.Hex(), nil
	default:
		return "", fmt.Errorf("unsupported id type %T", value)
	}
}

func legacyString(value any) string {
	switch value := value.(type) {
	case string:
		return value
	case bson.ObjectID:
		return value.Hex()
	default:
		return fmt.Sprint(value)
	}
}

func appendUniqueString(value any, item string) bson.A {
	result := bson.A{}
	switch values := value.(type) {
	case bson.A:
		result = append(result, values...)
	case []any:
		result = append(result, values...)
	case []string:
		for _, value := range values {
			result = append(result, value)
		}
	}
	for _, value := range result {
		if value == item {
			return result
		}
	}
	return append(result, item)
}

func isObjectIDString(value string) bool {
	if len(value) != 24 || value != strings.ToLower(value) {
		return false
	}
	_, err := bson.ObjectIDFromHex(value)
	return err == nil
}

func legacyLookupCollection(name string) bool {
	switch name {
	case "reactions", "notifications":
		return true
	default:
		return false
	}
}

func voteScope(noteID, actorID string, choice int, multiple bool) string {
	if multiple {
		return noteID + ":" + actorID + ":" + strconv.Itoa(choice)
	}
	return noteID + ":" + actorID
}

func numericInt(value any) int {
	switch value := value.(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	default:
		return 0
	}
}

func isNamespaceNotFound(err error) bool {
	var commandError mongo.CommandError
	return errors.As(err, &commandError) && commandError.Code == 26
}

func (p *migrationPlan) count() int {
	total := len(p.accounts) + len(p.actors) + len(p.notes) + len(p.media)
	for _, mappings := range p.ids {
		total += len(mappings)
	}
	return total
}
