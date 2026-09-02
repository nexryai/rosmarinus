package mongostore

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func BootstrapIndexes(ctx context.Context, db *mongo.Database) error {
	_, err := db.Collection("accounts").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "usernameLower", Value: 1}},
			Options: options.Index().
				SetName("uniq_accounts_username_lower").
				SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "webAuthnId", Value: 1}},
			Options: options.Index().
				SetName("uniq_accounts_webauthn_id").
				SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "bootstrapSlot", Value: 1}},
			Options: options.Index().
				SetName("uniq_accounts_bootstrap_slot").
				SetUnique(true).
				SetSparse(true),
		},
		{
			Keys: bson.D{{Key: "credentials.id", Value: 1}},
			Options: options.Index().
				SetName("uniq_accounts_credential_id").
				SetUnique(true).
				SetSparse(true),
		},
	})
	if err != nil {
		return err
	}
	_, err = db.Collection("webauthn_challenges").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "expiresAt", Value: 1}},
		Options: options.Index().
			SetName("ttl_webauthn_challenges_expires_at").
			SetExpireAfterSeconds(0),
	})
	if err != nil {
		return err
	}
	_, err = db.Collection("sessions").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "tokenHash", Value: 1}},
			Options: options.Index().
				SetName("uniq_sessions_token_hash").
				SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "accountId", Value: 1}, {Key: "expiresAt", Value: -1}},
			Options: options.Index().
				SetName("idx_sessions_account_expires_at"),
		},
		{
			Keys: bson.D{{Key: "expiresAt", Value: 1}},
			Options: options.Index().
				SetName("ttl_sessions_expires_at").
				SetExpireAfterSeconds(0),
		},
	})
	if err != nil {
		return err
	}
	_, err = db.Collection("actors").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "uri", Value: 1}},
			Options: options.Index().
				SetName("uniq_actors_uri").
				SetUnique(true).
				SetSparse(true),
		},
		{
			Keys: bson.D{{Key: "usernameLower", Value: 1}, {Key: "host", Value: 1}},
			Options: options.Index().
				SetName("uniq_actors_username_host").
				SetUnique(true).
				SetSparse(true),
		},
		{
			Keys: bson.D{{Key: "ownerAccountId", Value: 1}, {Key: "isSuspended", Value: 1}},
			Options: options.Index().
				SetName("idx_actors_owner_suspended").
				SetSparse(true),
		},
	})
	if err != nil {
		return err
	}
	_, err = db.Collection("notes").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "uri", Value: 1}},
			Options: options.Index().
				SetName("uniq_notes_uri").
				SetUnique(true).
				SetSparse(true),
		},
		{
			Keys: bson.D{{Key: "authorId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().
				SetName("idx_notes_author_created_at"),
		},
		{
			Keys: bson.D{{Key: "renoteId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().
				SetName("idx_notes_renote_created_at").
				SetSparse(true),
		},
		{
			Keys:    bson.D{{Key: "replyId", Value: 1}, {Key: "deletedAt", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("idx_notes_reply_active_created_at"),
		},
		{
			Keys:    bson.D{{Key: "renoteId", Value: 1}, {Key: "deletedAt", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("idx_notes_renote_active_created_at"),
		},
		{
			Keys:    bson.D{{Key: "quoteId", Value: 1}, {Key: "deletedAt", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("idx_notes_quote_active_created_at"),
		},
	})
	if err != nil {
		return err
	}
	_, err = db.Collection("polls").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "authorId", Value: 1}, {Key: "expiresAt", Value: 1}},
			Options: options.Index().SetName("idx_polls_author_expires_at"),
		},
		{
			Keys:    bson.D{{Key: "expiresAt", Value: 1}},
			Options: options.Index().SetName("idx_polls_expires_at").SetSparse(true),
		},
	})
	if err != nil {
		return err
	}
	_, err = db.Collection("poll_votes").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "noteId", Value: 1}, {Key: "choice", Value: 1}},
			Options: options.Index().SetName("idx_poll_votes_note_choice"),
		},
		{
			Keys:    bson.D{{Key: "actorId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("idx_poll_votes_actor_created_at"),
		},
	})
	if err != nil {
		return err
	}
	_, err = db.Collection("reactions").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "noteId", Value: 1}, {Key: "actorId", Value: 1}},
			Options: options.Index().
				SetName("uniq_reactions_note_actor").
				SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "noteId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().
				SetName("idx_reactions_note_created_at"),
		},
		{
			Keys:    bson.D{{Key: "noteId", Value: 1}, {Key: "deletedAt", Value: 1}, {Key: "reaction", Value: 1}},
			Options: options.Index().SetName("idx_reactions_note_active_reaction"),
		},
		{
			Keys: bson.D{{Key: "actorId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().
				SetName("idx_reactions_actor_created_at"),
		},
	})
	if err != nil {
		return err
	}
	_, err = db.Collection("emojis").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "host", Value: 1}, {Key: "name", Value: 1}},
			Options: options.Index().
				SetName("uniq_emojis_host_name").
				SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "uri", Value: 1}},
			Options: options.Index().
				SetName("idx_emojis_uri").
				SetSparse(true),
		},
	})
	if err != nil {
		return err
	}
	_, err = db.Collection("blocks").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "blockerId", Value: 1}, {Key: "blockeeId", Value: 1}},
			Options: options.Index().
				SetName("uniq_blocks_blocker_blockee").
				SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "blockeeId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().
				SetName("idx_blocks_blockee_created_at"),
		},
	})
	if err != nil {
		return err
	}
	_, err = db.Collection("abuse_reports").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "remoteActivityId", Value: 1}},
			Options: options.Index().
				SetName("uniq_abuse_reports_remote_activity").
				SetUnique(true).
				SetSparse(true),
		},
		{
			Keys: bson.D{{Key: "targetUserId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().
				SetName("idx_abuse_reports_target_created_at"),
		},
		{
			Keys: bson.D{{Key: "reporterId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().
				SetName("idx_abuse_reports_reporter_created_at"),
		},
	})
	if err != nil {
		return err
	}
	_, err = db.Collection("follows").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "followerId", Value: 1}, {Key: "followeeId", Value: 1}},
			Options: options.Index().
				SetName("uniq_follows_follower_followee").
				SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "remoteActivityId", Value: 1}},
			Options: options.Index().
				SetName("uniq_follows_remote_activity").
				SetUnique(true).
				SetPartialFilterExpression(bson.M{"remoteActivityId": bson.M{"$type": "string", "$gt": ""}}),
		},
		{
			Keys: bson.D{{Key: "followerId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().
				SetName("idx_follows_follower_created_at"),
		},
		{
			Keys: bson.D{{Key: "followerId", Value: 1}, {Key: "status", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().
				SetName("idx_follows_follower_status_created_at"),
		},
		{
			Keys: bson.D{{Key: "followeeId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().
				SetName("idx_follows_followee_created_at"),
		},
		{
			Keys: bson.D{{Key: "followeeId", Value: 1}, {Key: "status", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().
				SetName("idx_follows_followee_status_created_at"),
		},
		{
			Keys: bson.D{{Key: "followerHost", Value: 1}, {Key: "status", Value: 1}, {Key: "deletedAt", Value: 1}},
			Options: options.Index().
				SetName("idx_follows_follower_host_status_active"),
		},
		{
			Keys: bson.D{{Key: "followeeHost", Value: 1}, {Key: "status", Value: 1}, {Key: "deletedAt", Value: 1}},
			Options: options.Index().
				SetName("idx_follows_followee_host_status_active"),
		},
	})
	if err != nil {
		return err
	}
	_, err = db.Collection("connector_command_receipts").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "accountId", Value: 1}, {Key: "requestId", Value: 1}},
			Options: options.Index().
				SetName("uniq_connector_receipts_account_request").
				SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "expiresAt", Value: 1}},
			Options: options.Index().
				SetName("ttl_connector_receipts_expires_at").
				SetExpireAfterSeconds(0),
		},
	})
	if err != nil {
		return err
	}
	_, err = db.Collection("inbox_activity_receipts").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "expiresAt", Value: 1}},
		Options: options.Index().
			SetName("ttl_inbox_activity_receipts_expires_at").
			SetExpireAfterSeconds(0),
	})
	if err != nil {
		return err
	}
	_, err = db.Collection("notifications").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "recipientActorId", Value: 1}, {Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}},
			Options: options.Index().SetName("idx_notifications_recipient_created_at"),
		},
		{
			Keys:    bson.D{{Key: "recipientAccountId", Value: 1}, {Key: "isRead", Value: 1}, {Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}},
			Options: options.Index().SetName("idx_notifications_account_read_created_at"),
		},
		{
			Keys:    bson.D{{Key: "recipientActorId", Value: 1}, {Key: "kind", Value: 1}, {Key: "remoteActivityId", Value: 1}},
			Options: options.Index().SetName("uniq_notifications_recipient_kind_activity").SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "sourceActorId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("idx_notifications_source_created_at").SetSparse(true),
		},
	})
	if err != nil {
		return err
	}
	_, err = db.Collection("media").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "originalUrl", Value: 1}},
			Options: options.Index().SetName("uniq_media_original_url").SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "state", Value: 1}, {Key: "createdAt", Value: 1}},
			Options: options.Index().SetName("idx_media_state_created_at"),
		},
		{
			Keys:    bson.D{{Key: "sha256", Value: 1}},
			Options: options.Index().SetName("idx_media_sha256").SetSparse(true),
		},
	})
	if err != nil {
		return err
	}
	_, err = db.Collection("instances").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "host", Value: 1}},
			Options: options.Index().SetName("uniq_instances_host").SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "suspensionState", Value: 1}, {Key: "updatedAt", Value: -1}},
			Options: options.Index().SetName("idx_instances_suspension_updated_at"),
		},
		{
			Keys:    bson.D{{Key: "infoUpdatedAt", Value: 1}},
			Options: options.Index().SetName("idx_instances_info_updated_at").SetSparse(true),
		},
	})
	return err
}
