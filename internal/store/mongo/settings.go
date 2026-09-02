package mongostore

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/nexryai/rosmarinus/internal/domain/notes"
	"github.com/nexryai/rosmarinus/internal/settings"
)

type SettingsRepository struct {
	accounts *mongo.Collection
	actors   *mongo.Collection
}

type accountSettingsDocument struct {
	AccountID       string    `bson:"_id"`
	Theme           string    `bson:"theme"`
	ReduceMotion    bool      `bson:"reduceMotion"`
	CompactMode     bool      `bson:"compactMode"`
	SelectedActorID string    `bson:"selectedActorId,omitempty"`
	UpdatedAt       time.Time `bson:"updatedAt"`
}

type actorSettingsDocument struct {
	ActorID            string    `bson:"_id"`
	AccountID          string    `bson:"accountId"`
	DefaultVisibility  string    `bson:"defaultVisibility"`
	ShowContentWarning bool      `bson:"showContentWarning"`
	DisplayOrder       int       `bson:"displayOrder"`
	Color              string    `bson:"color,omitempty"`
	Pinned             bool      `bson:"pinned"`
	UpdatedAt          time.Time `bson:"updatedAt"`
}

func NewSettingsRepository(db *mongo.Database) *SettingsRepository {
	return &SettingsRepository{accounts: db.Collection("ui_settings"), actors: db.Collection("actor_settings")}
}

func (r *SettingsRepository) GetAccount(ctx context.Context, accountID string) (*settings.Account, error) {
	var doc accountSettingsDocument
	err := r.accounts.FindOne(ctx, bson.M{"_id": accountID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return defaultAccountSettings(accountID), nil
	}
	if err != nil {
		return nil, err
	}
	return accountSettingsFromDocument(doc), nil
}

func (r *SettingsRepository) UpdateAccount(ctx context.Context, accountID string, patch settings.AccountPatch) (*settings.Account, error) {
	set := bson.M{"updatedAt": time.Now().UTC()}
	setOnInsert := bson.M{}
	if patch.Theme != nil {
		set["theme"] = *patch.Theme
	} else {
		setOnInsert["theme"] = settings.DefaultTheme
	}
	if patch.ReduceMotion != nil {
		set["reduceMotion"] = *patch.ReduceMotion
	} else {
		setOnInsert["reduceMotion"] = false
	}
	if patch.CompactMode != nil {
		set["compactMode"] = *patch.CompactMode
	} else {
		setOnInsert["compactMode"] = false
	}
	if patch.SelectedActorID != nil {
		set["selectedActorId"] = *patch.SelectedActorID
	} else {
		setOnInsert["selectedActorId"] = ""
	}
	var doc accountSettingsDocument
	err := r.accounts.FindOneAndUpdate(ctx, bson.M{"_id": accountID}, bson.M{
		"$set": set, "$setOnInsert": setOnInsert,
	}, options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)).Decode(&doc)
	if err != nil {
		return nil, err
	}
	return accountSettingsFromDocument(doc), nil
}

func (r *SettingsRepository) GetActor(ctx context.Context, accountID, actorID string) (*settings.Actor, error) {
	var doc actorSettingsDocument
	err := r.actors.FindOne(ctx, bson.M{"_id": actorID, "accountId": accountID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return defaultActorSettings(accountID, actorID), nil
	}
	if err != nil {
		return nil, err
	}
	return actorSettingsFromDocument(doc), nil
}

func (r *SettingsRepository) UpdateActor(ctx context.Context, accountID, actorID string, patch settings.ActorPatch) (*settings.Actor, error) {
	set := bson.M{"updatedAt": time.Now().UTC()}
	setOnInsert := bson.M{}
	if patch.DefaultVisibility != nil {
		set["defaultVisibility"] = *patch.DefaultVisibility
	} else {
		setOnInsert["defaultVisibility"] = string(notes.VisibilityPublic)
	}
	if patch.ShowContentWarning != nil {
		set["showContentWarning"] = *patch.ShowContentWarning
	} else {
		setOnInsert["showContentWarning"] = true
	}
	if patch.DisplayOrder != nil {
		set["displayOrder"] = *patch.DisplayOrder
	} else {
		setOnInsert["displayOrder"] = 0
	}
	if patch.Color != nil {
		set["color"] = *patch.Color
	} else {
		setOnInsert["color"] = ""
	}
	if patch.Pinned != nil {
		set["pinned"] = *patch.Pinned
	} else {
		setOnInsert["pinned"] = false
	}
	var doc actorSettingsDocument
	err := r.actors.FindOneAndUpdate(ctx, bson.M{"_id": actorID, "accountId": accountID}, bson.M{
		"$set":         set,
		"$setOnInsert": setOnInsert,
	}, options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)).Decode(&doc)
	if err != nil {
		return nil, err
	}
	return actorSettingsFromDocument(doc), nil
}

func defaultAccountSettings(accountID string) *settings.Account {
	return &settings.Account{AccountID: accountID, Theme: settings.DefaultTheme}
}

func defaultActorSettings(accountID, actorID string) *settings.Actor {
	return &settings.Actor{AccountID: accountID, ActorID: actorID, DefaultVisibility: string(notes.VisibilityPublic), ShowContentWarning: true}
}

func accountSettingsFromDocument(doc accountSettingsDocument) *settings.Account {
	theme := doc.Theme
	if theme == "" {
		theme = settings.DefaultTheme
	}
	return &settings.Account{
		AccountID: doc.AccountID, Theme: theme, ReduceMotion: doc.ReduceMotion,
		CompactMode: doc.CompactMode, SelectedActorID: doc.SelectedActorID, UpdatedAt: doc.UpdatedAt,
	}
}

func actorSettingsFromDocument(doc actorSettingsDocument) *settings.Actor {
	visibility := doc.DefaultVisibility
	if visibility == "" {
		visibility = string(notes.VisibilityPublic)
	}
	return &settings.Actor{
		AccountID: doc.AccountID, ActorID: doc.ActorID, DefaultVisibility: visibility,
		ShowContentWarning: doc.ShowContentWarning, DisplayOrder: doc.DisplayOrder,
		Color: doc.Color, Pinned: doc.Pinned, UpdatedAt: doc.UpdatedAt,
	}
}

var _ settings.Repository = (*SettingsRepository)(nil)
