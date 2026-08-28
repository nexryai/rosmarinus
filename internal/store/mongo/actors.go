package mongostore

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/nexryai/rosmarinus/internal/domain/actors"
)

type ActorRepository struct {
	collection *mongo.Collection
}

type actorDocument struct {
	ID              string                      `bson:"_id,omitempty"`
	OwnerAccountID  string                      `bson:"ownerAccountId,omitempty"`
	IsSystemActor   bool                        `bson:"isSystemActor"`
	Username        string                      `bson:"username"`
	UsernameLower   string                      `bson:"usernameLower"`
	Name            string                      `bson:"name,omitempty"`
	Summary         string                      `bson:"summary,omitempty"`
	URL             string                      `bson:"url,omitempty"`
	ProfileFields   []actorProfileFieldDocument `bson:"profileFields,omitempty"`
	Birthday        string                      `bson:"birthday,omitempty"`
	Location        string                      `bson:"location,omitempty"`
	AvatarURL       string                      `bson:"avatarUrl,omitempty"`
	BannerURL       string                      `bson:"bannerUrl,omitempty"`
	Tags            []string                    `bson:"tags,omitempty"`
	EmojiNames      []string                    `bson:"emojiNames,omitempty"`
	IsBot           bool                        `bson:"isBot,omitempty"`
	IsCat           bool                        `bson:"isCat,omitempty"`
	IsLocked        bool                        `bson:"isLocked,omitempty"`
	IsDiscoverable  bool                        `bson:"isDiscoverable,omitempty"`
	Type            string                      `bson:"type"`
	Host            *string                     `bson:"host"`
	URI             string                      `bson:"uri"`
	Inbox           string                      `bson:"inbox"`
	SharedInbox     string                      `bson:"sharedInbox"`
	FollowersURI    string                      `bson:"followersUri,omitempty"`
	FollowingURI    string                      `bson:"followingUri,omitempty"`
	FeaturedURI     string                      `bson:"featuredUri,omitempty"`
	FeaturedNoteIDs []string                    `bson:"featuredNoteIds,omitempty"`
	MovedToURI      string                      `bson:"movedToUri,omitempty"`
	AlsoKnownAs     []string                    `bson:"alsoKnownAs,omitempty"`
	MovedAt         *time.Time                  `bson:"movedAt,omitempty"`
	LastFetchedAt   time.Time                   `bson:"lastFetchedAt,omitempty"`
	PublicKeyID     string                      `bson:"publicKeyId"`
	PublicKeyPEM    string                      `bson:"publicKeyPem"`
	PrivateKeyPEM   string                      `bson:"privateKeyPem,omitempty"`
	IsSuspended     bool                        `bson:"isSuspended"`
	DeletedAt       *time.Time                  `bson:"deletedAt,omitempty"`
}

type actorProfileFieldDocument struct {
	Name  string `bson:"name"`
	Value string `bson:"value"`
}

func NewActorRepository(db *mongo.Database) *ActorRepository {
	return &ActorRepository{collection: db.Collection("actors")}
}

func (r *ActorRepository) EnsureLocalActor(ctx context.Context, actor actors.Actor) (*actors.Actor, error) {
	if actor.UsernameLower == "" {
		actor.UsernameLower = strings.ToLower(actor.Username)
	}
	existing, err := r.FindLocalByUsername(ctx, actor.Username)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if actor.IsSystemActor && !existing.IsSystemActor {
			_, err := r.collection.UpdateOne(ctx, bson.M{
				"_id":            existing.ID,
				"host":           nil,
				"ownerAccountId": bson.M{"$in": bson.A{nil, ""}},
			}, bson.M{"$set": bson.M{"isSystemActor": true}})
			if err != nil {
				return nil, err
			}
			return r.FindLocalByID(ctx, existing.ID)
		}
		return existing, nil
	}
	if actor.PublicKeyPEM == "" || actor.PrivateKeyPEM == "" {
		publicKey, privateKey, err := generateRSAKeyPair()
		if err != nil {
			return nil, err
		}
		actor.PublicKeyPEM = publicKey
		actor.PrivateKeyPEM = privateKey
	}
	doc := fromActor(actor)
	_, err = r.collection.UpdateOne(ctx, bson.M{"_id": actor.ID}, bson.M{"$setOnInsert": doc}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return nil, err
	}
	return r.FindLocalByID(ctx, actor.ID)
}

func (r *ActorRepository) FindOwnedLocalByID(ctx context.Context, accountID, actorID string) (*actors.Actor, error) {
	accountID = strings.TrimSpace(accountID)
	actorID = strings.TrimSpace(actorID)
	if accountID == "" || actorID == "" {
		return nil, nil
	}
	return r.findOne(ctx, bson.M{
		"_id":            actorID,
		"ownerAccountId": accountID,
		"host":           nil,
		"isSystemActor":  bson.M{"$ne": true},
		"isSuspended":    false,
	})
}

func (r *ActorRepository) CreateOwnedLocalActor(ctx context.Context, actor actors.Actor) (*actors.Actor, error) {
	actor.ID = strings.TrimSpace(actor.ID)
	actor.OwnerAccountID = strings.TrimSpace(actor.OwnerAccountID)
	actor.Username = strings.TrimSpace(actor.Username)
	actor.URI = strings.TrimSpace(actor.URI)
	if actor.ID == "" || actor.OwnerAccountID == "" || actor.Username == "" || actor.URI == "" {
		return nil, fmt.Errorf("owned local actor id, owner, username, and uri are required")
	}
	if actor.Host != nil {
		return nil, fmt.Errorf("owned actor must be local")
	}
	if actor.IsSystemActor {
		return nil, fmt.Errorf("owned actor must not be a system actor")
	}
	actor.UsernameLower = strings.ToLower(actor.Username)
	if actor.PublicKeyPEM == "" || actor.PrivateKeyPEM == "" {
		publicKey, privateKey, err := generateRSAKeyPair()
		if err != nil {
			return nil, err
		}
		actor.PublicKeyPEM = publicKey
		actor.PrivateKeyPEM = privateKey
	}
	if _, err := r.collection.InsertOne(ctx, fromActor(actor)); err != nil {
		return nil, err
	}
	return r.FindOwnedLocalByID(ctx, actor.OwnerAccountID, actor.ID)
}

// UpdateOwnedLocalActor applies only profile fields. The ownership and local
// actor predicates are part of the same MongoDB update filter so a stale
// authorization lookup cannot update an actor after ownership/state changes.
func (r *ActorRepository) UpdateOwnedLocalActor(ctx context.Context, accountID, actorID string, patch actors.ActorPatch) (*actors.Actor, error) {
	accountID = strings.TrimSpace(accountID)
	actorID = strings.TrimSpace(actorID)
	if accountID == "" || actorID == "" {
		return nil, fmt.Errorf("owner account id and actor id are required")
	}
	set, unset := actorPatchUpdate(patch)
	if len(set) == 0 && len(unset) == 0 {
		return r.FindOwnedLocalByID(ctx, accountID, actorID)
	}
	update := bson.M{}
	if len(set) != 0 {
		update["$set"] = set
	}
	if len(unset) != 0 {
		update["$unset"] = unset
	}
	result, err := r.collection.UpdateOne(ctx, bson.M{
		"_id":            actorID,
		"ownerAccountId": accountID,
		"host":           nil,
		"isSystemActor":  bson.M{"$ne": true},
		"isSuspended":    false,
	}, update)
	if err != nil {
		return nil, err
	}
	if result.MatchedCount == 0 {
		return nil, nil
	}
	return r.FindOwnedLocalByID(ctx, accountID, actorID)
}

func actorPatchUpdate(patch actors.ActorPatch) (bson.M, bson.M) {
	set := bson.M{}
	unset := bson.M{}
	setString := func(field actors.Optional[string], key string) {
		if !field.IsSet() {
			return
		}
		if field.Value == nil {
			unset[key] = ""
			return
		}
		set[key] = *field.Value
	}
	setString(patch.Name, "name")
	setString(patch.Summary, "summary")
	setString(patch.URL, "url")
	setString(patch.Birthday, "birthday")
	setString(patch.Location, "location")
	setString(patch.AvatarURL, "avatarUrl")
	setString(patch.BannerURL, "bannerUrl")

	if patch.ProfileFields.IsSet() {
		if patch.ProfileFields.Value == nil {
			unset["profileFields"] = ""
		} else {
			set["profileFields"] = profileFieldDocuments(*patch.ProfileFields.Value)
		}
	}
	if patch.Tags.IsSet() {
		if patch.Tags.Value == nil {
			unset["tags"] = ""
		} else {
			set["tags"] = append([]string(nil), (*patch.Tags.Value)...)
		}
	}
	if patch.EmojiNames.IsSet() {
		if patch.EmojiNames.Value == nil {
			unset["emojiNames"] = ""
		} else {
			set["emojiNames"] = append([]string(nil), (*patch.EmojiNames.Value)...)
		}
	}
	setBool := func(field actors.Optional[bool], key string) {
		if !field.IsSet() {
			return
		}
		if field.Value == nil {
			unset[key] = ""
			return
		}
		set[key] = *field.Value
	}
	setBool(patch.IsBot, "isBot")
	setBool(patch.IsCat, "isCat")
	setBool(patch.IsLocked, "isLocked")
	setBool(patch.IsDiscoverable, "isDiscoverable")
	return set, unset
}

func (r *ActorRepository) SuspendOwnedLocalActors(ctx context.Context, accountID string) (int64, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return 0, fmt.Errorf("owner account id is required")
	}
	now := time.Now().UTC()
	result, err := r.collection.UpdateMany(ctx, bson.M{
		"ownerAccountId": accountID,
		"host":           nil,
		"isSystemActor":  bson.M{"$ne": true},
		"isSuspended":    false,
	}, bson.M{"$set": bson.M{
		"isSuspended": true,
		"suspendedAt": now,
	}})
	if err != nil {
		return 0, err
	}
	return result.ModifiedCount, nil
}

func (r *ActorRepository) ListOwnedAccountIDs(ctx context.Context) ([]string, error) {
	var accountIDs []string
	err := r.collection.Distinct(ctx, "ownerAccountId", bson.M{
		"ownerAccountId": bson.M{"$exists": true, "$ne": ""},
		"host":           nil,
		"isSystemActor":  bson.M{"$ne": true},
	}).Decode(&accountIDs)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	return accountIDs, err
}

func (r *ActorRepository) FindLocalByID(ctx context.Context, id string) (*actors.Actor, error) {
	return r.findOne(ctx, bson.M{
		"_id":         id,
		"host":        nil,
		"isSuspended": false,
	})
}

func (r *ActorRepository) FindByID(ctx context.Context, id string) (*actors.Actor, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil
	}
	return r.findOne(ctx, bson.M{
		"_id":         id,
		"isSuspended": false,
	})
}

func (r *ActorRepository) FindLocalByUsername(ctx context.Context, username string) (*actors.Actor, error) {
	return r.findOne(ctx, bson.M{
		"usernameLower": strings.ToLower(username),
		"host":          nil,
		"isSuspended":   false,
	})
}

func (r *ActorRepository) FindByURI(ctx context.Context, uri string) (*actors.Actor, error) {
	return r.findOne(ctx, bson.M{
		"uri":         uri,
		"isSuspended": false,
	})
}

func (r *ActorRepository) FindAnyByURI(ctx context.Context, uri string) (*actors.Actor, error) {
	return r.findOne(ctx, bson.M{"uri": uri})
}

func (r *ActorRepository) FilterActiveRemoteIDs(ctx context.Context, ids []string) (map[string]struct{}, error) {
	result := make(map[string]struct{})
	if len(ids) == 0 {
		return result, nil
	}
	cursor, err := r.collection.Find(ctx, bson.M{
		"_id":         bson.M{"$in": ids},
		"host":        bson.M{"$ne": nil},
		"isSuspended": false,
		"deletedAt":   nil,
	}, options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var document struct {
			ID string `bson:"_id"`
		}
		if err := cursor.Decode(&document); err != nil {
			return nil, err
		}
		result[document.ID] = struct{}{}
	}
	return result, cursor.Err()
}

func (r *ActorRepository) FindByPublicKeyID(ctx context.Context, keyID string) (*actors.Actor, error) {
	return r.findOne(ctx, bson.M{
		"publicKeyId": keyID,
		"isSuspended": false,
	})
}

func (r *ActorRepository) UpsertRemoteActor(ctx context.Context, actor actors.Actor) (*actors.Actor, error) {
	if actor.URI == "" {
		return nil, fmt.Errorf("remote actor uri is required")
	}
	if actor.Host == nil || *actor.Host == "" {
		return nil, fmt.Errorf("remote actor host is required")
	}
	actor.UsernameLower = strings.ToLower(actor.Username)
	doc := fromActor(actor)
	doc.LastFetchedAt = time.Now().UTC()
	fields := bson.M{
		"username":        doc.Username,
		"usernameLower":   doc.UsernameLower,
		"name":            doc.Name,
		"summary":         doc.Summary,
		"url":             doc.URL,
		"profileFields":   doc.ProfileFields,
		"birthday":        doc.Birthday,
		"location":        doc.Location,
		"avatarUrl":       doc.AvatarURL,
		"bannerUrl":       doc.BannerURL,
		"tags":            doc.Tags,
		"emojiNames":      doc.EmojiNames,
		"isBot":           doc.IsBot,
		"isCat":           doc.IsCat,
		"isLocked":        doc.IsLocked,
		"isDiscoverable":  doc.IsDiscoverable,
		"type":            doc.Type,
		"host":            doc.Host,
		"inbox":           doc.Inbox,
		"sharedInbox":     doc.SharedInbox,
		"followersUri":    doc.FollowersURI,
		"followingUri":    doc.FollowingURI,
		"featuredUri":     doc.FeaturedURI,
		"featuredNoteIds": doc.FeaturedNoteIDs,
		"movedToUri":      doc.MovedToURI,
		"alsoKnownAs":     doc.AlsoKnownAs,
		"lastFetchedAt":   doc.LastFetchedAt,
		"publicKeyId":     doc.PublicKeyID,
		"publicKeyPem":    doc.PublicKeyPEM,
		"isSuspended":     doc.IsSuspended,
	}
	// movedAt records Rosmarinus' validation decision, not remote profile data.
	// Ordinary Actor refreshes must not clear a previously accepted migration.
	if doc.MovedAt != nil {
		fields["movedAt"] = doc.MovedAt
	}
	_, err := r.collection.UpdateOne(ctx, bson.M{"uri": actor.URI}, bson.M{
		"$set": fields,
		"$setOnInsert": bson.M{
			"_id": doc.ID,
			"uri": doc.URI,
		},
	}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return nil, err
	}
	return r.FindByURI(ctx, actor.URI)
}

func (r *ActorRepository) AddRemoteFeaturedNote(ctx context.Context, actorURI, noteID string, limit int) (*actors.Actor, error) {
	if actorURI == "" || noteID == "" {
		return nil, fmt.Errorf("remote actor uri and featured note id are required")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("featured note limit must be positive")
	}
	existing := bson.M{"$ifNull": bson.A{"$featuredNoteIds", bson.A{}}}
	update := mongo.Pipeline{bson.D{{Key: "$set", Value: bson.M{
		"featuredNoteIds": bson.M{"$cond": bson.A{
			bson.M{"$in": bson.A{noteID, existing}},
			existing,
			bson.M{"$slice": bson.A{
				bson.M{"$concatArrays": bson.A{existing, bson.A{noteID}}},
				limit,
			}},
		}},
	}}}}
	result, err := r.collection.UpdateOne(ctx, bson.M{
		"uri":  actorURI,
		"host": bson.M{"$ne": nil},
	}, update)
	if err != nil {
		return nil, err
	}
	if result.MatchedCount == 0 {
		return nil, fmt.Errorf("remote actor not found")
	}
	return r.FindAnyByURI(ctx, actorURI)
}

func (r *ActorRepository) RemoveRemoteFeaturedNote(ctx context.Context, actorURI, noteID string) (*actors.Actor, error) {
	if actorURI == "" || noteID == "" {
		return nil, fmt.Errorf("remote actor uri and featured note id are required")
	}
	existing := bson.M{"$ifNull": bson.A{"$featuredNoteIds", bson.A{}}}
	update := mongo.Pipeline{bson.D{{Key: "$set", Value: bson.M{
		"featuredNoteIds": bson.M{"$filter": bson.M{
			"input": existing,
			"as":    "noteId",
			"cond":  bson.M{"$ne": bson.A{"$$noteId", noteID}},
		}},
	}}}}
	result, err := r.collection.UpdateOne(ctx, bson.M{
		"uri":  actorURI,
		"host": bson.M{"$ne": nil},
	}, update)
	if err != nil {
		return nil, err
	}
	if result.MatchedCount == 0 {
		return nil, fmt.Errorf("remote actor not found")
	}
	return r.FindAnyByURI(ctx, actorURI)
}

func (r *ActorRepository) MarkRemoteActorDeleted(ctx context.Context, uri string) error {
	if uri == "" {
		return fmt.Errorf("remote actor uri is required")
	}
	now := time.Now().UTC()
	_, err := r.collection.UpdateOne(ctx, bson.M{"uri": uri, "host": bson.M{"$ne": nil}}, bson.M{
		"$set": bson.M{
			"isSuspended": true,
			"deletedAt":   now,
		},
	})
	return err
}

func (r *ActorRepository) findOne(ctx context.Context, filter bson.M) (*actors.Actor, error) {
	var doc actorDocument
	if err := r.collection.FindOne(ctx, filter).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &actors.Actor{
		ID:              doc.ID,
		OwnerAccountID:  doc.OwnerAccountID,
		IsSystemActor:   doc.IsSystemActor,
		Username:        doc.Username,
		UsernameLower:   doc.UsernameLower,
		Name:            doc.Name,
		Summary:         doc.Summary,
		URL:             doc.URL,
		ProfileFields:   profileFieldsFromDocuments(doc.ProfileFields),
		Birthday:        doc.Birthday,
		Location:        doc.Location,
		AvatarURL:       doc.AvatarURL,
		BannerURL:       doc.BannerURL,
		Tags:            doc.Tags,
		EmojiNames:      doc.EmojiNames,
		IsBot:           doc.IsBot,
		IsCat:           doc.IsCat,
		IsLocked:        doc.IsLocked,
		IsDiscoverable:  doc.IsDiscoverable,
		Type:            doc.Type,
		Host:            doc.Host,
		URI:             doc.URI,
		Inbox:           doc.Inbox,
		SharedInbox:     doc.SharedInbox,
		FollowersURI:    doc.FollowersURI,
		FollowingURI:    doc.FollowingURI,
		FeaturedURI:     doc.FeaturedURI,
		FeaturedNoteIDs: doc.FeaturedNoteIDs,
		MovedToURI:      doc.MovedToURI,
		AlsoKnownAs:     doc.AlsoKnownAs,
		MovedAt:         doc.MovedAt,
		LastFetchedAt:   doc.LastFetchedAt,
		PublicKeyID:     doc.PublicKeyID,
		PublicKeyPEM:    doc.PublicKeyPEM,
		PrivateKeyPEM:   doc.PrivateKeyPEM,
		IsSuspended:     doc.IsSuspended,
	}, nil
}

func fromActor(actor actors.Actor) actorDocument {
	return actorDocument{
		ID:              actor.ID,
		OwnerAccountID:  actor.OwnerAccountID,
		IsSystemActor:   actor.IsSystemActor,
		Username:        actor.Username,
		UsernameLower:   actor.UsernameLower,
		Name:            actor.Name,
		Summary:         actor.Summary,
		URL:             actor.URL,
		ProfileFields:   profileFieldDocuments(actor.ProfileFields),
		Birthday:        actor.Birthday,
		Location:        actor.Location,
		AvatarURL:       actor.AvatarURL,
		BannerURL:       actor.BannerURL,
		Tags:            actor.Tags,
		EmojiNames:      actor.EmojiNames,
		IsBot:           actor.IsBot,
		IsCat:           actor.IsCat,
		IsLocked:        actor.IsLocked,
		IsDiscoverable:  actor.IsDiscoverable,
		Type:            actor.Type,
		Host:            actor.Host,
		URI:             actor.URI,
		Inbox:           actor.Inbox,
		SharedInbox:     actor.SharedInbox,
		FollowersURI:    actor.FollowersURI,
		FollowingURI:    actor.FollowingURI,
		FeaturedURI:     actor.FeaturedURI,
		FeaturedNoteIDs: actor.FeaturedNoteIDs,
		MovedToURI:      actor.MovedToURI,
		AlsoKnownAs:     actor.AlsoKnownAs,
		MovedAt:         actor.MovedAt,
		LastFetchedAt:   actor.LastFetchedAt,
		PublicKeyID:     actor.PublicKeyID,
		PublicKeyPEM:    actor.PublicKeyPEM,
		PrivateKeyPEM:   actor.PrivateKeyPEM,
		IsSuspended:     actor.IsSuspended,
	}
}

func profileFieldDocuments(fields []actors.ProfileField) []actorProfileFieldDocument {
	result := make([]actorProfileFieldDocument, 0, len(fields))
	for _, field := range fields {
		result = append(result, actorProfileFieldDocument{Name: field.Name, Value: field.Value})
	}
	return result
}

func profileFieldsFromDocuments(fields []actorProfileFieldDocument) []actors.ProfileField {
	result := make([]actors.ProfileField, 0, len(fields))
	for _, field := range fields {
		result = append(result, actors.ProfileField{Name: field.Name, Value: field.Value})
	}
	return result
}

func generateRSAKeyPair() (string, string, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", fmt.Errorf("generate actor keypair: %w", err)
	}
	privateDER := x509.MarshalPKCS1PrivateKey(privateKey)
	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: privateDER})
	publicDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", "", fmt.Errorf("marshal actor public key: %w", err)
	}
	publicPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})
	return string(publicPEM), string(privatePEM), nil
}
