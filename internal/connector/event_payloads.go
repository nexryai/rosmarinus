package connector

type PostCreated struct {
	AccountID string `json:"-"`
	ActorID   string `json:"actor_id"`
	NoteID    string `json:"note_id"`
	URI       string `json:"uri"`
}

type NotificationCreated struct {
	AccountID        string `json:"-"`
	RecipientActorID string `json:"recipient_actor_id"`
	NotificationID   string `json:"notification_id"`
	Kind             string `json:"kind"`
	SourceActorID    string `json:"source_actor_id,omitempty"`
	NoteID           string `json:"note_id,omitempty"`
}

type FollowApproval struct {
	AccountID   string `json:"-"`
	FollowerID  string `json:"follower_id"`
	FolloweeID  string `json:"followee_id"`
	FollowerURI string `json:"follower_uri"`
	FolloweeURI string `json:"followee_uri"`
}
