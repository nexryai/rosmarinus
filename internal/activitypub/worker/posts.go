package worker

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	apnotes "github.com/nexryai/rosmarinus/internal/activitypub/notes"
	appolls "github.com/nexryai/rosmarinus/internal/activitypub/polls"
	apreactions "github.com/nexryai/rosmarinus/internal/activitypub/reactions"
	"github.com/nexryai/rosmarinus/internal/connector"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/emojis"
	"github.com/nexryai/rosmarinus/internal/domain/follows"
	domainmedia "github.com/nexryai/rosmarinus/internal/domain/media"
	domainnotes "github.com/nexryai/rosmarinus/internal/domain/notes"
	"github.com/nexryai/rosmarinus/internal/domain/polls"
	"github.com/nexryai/rosmarinus/internal/domain/reactions"
	"github.com/nexryai/rosmarinus/internal/queue"
)

func (h *Handler) CreatePost(ctx context.Context, command connector.PostCreateCommand) (connector.PostCreated, error) {
	if h.notes == nil {
		return connector.PostCreated{}, fmt.Errorf("note repository is not configured")
	}
	actor, err := h.repo.FindLocalByID(ctx, command.ActorID)
	if err != nil {
		return connector.PostCreated{}, err
	}
	if actor == nil {
		return connector.PostCreated{}, fmt.Errorf("local actor not found: %s", command.ActorID)
	}
	visibility, err := postVisibility(command.Visibility)
	if err != nil {
		return connector.PostCreated{}, err
	}
	if strings.TrimSpace(command.RenoteID) != "" {
		return h.createRenote(ctx, actor, command, visibility)
	}
	var replyTarget, quoteTarget *domainnotes.Note
	if strings.TrimSpace(command.InReplyToURI) != "" {
		replyTarget, err = h.resolveLocalPostReference(ctx, actor, command.InReplyToURI, "reply")
		if err != nil {
			return connector.PostCreated{}, err
		}
	}
	if strings.TrimSpace(command.QuoteURI) != "" {
		quoteTarget, err = h.resolveLocalPostReference(ctx, actor, command.QuoteURI, "quote")
		if err != nil {
			return connector.PostCreated{}, err
		}
		if isPureRenote(quoteTarget) {
			return connector.PostCreated{}, fmt.Errorf("cannot quote a pure renote")
		}
		visibility, err = localRenoteVisibility(actor, quoteTarget, visibility)
		if err != nil {
			return connector.PostCreated{}, fmt.Errorf("quote target is not shareable: %w", err)
		}
	}
	additionalRecipientURIs := make([]string, 0, 2)
	if replyTarget != nil {
		additionalRecipientURIs = append(additionalRecipientURIs, replyTarget.AttributedTo)
	}
	if quoteTarget != nil {
		additionalRecipientURIs = append(additionalRecipientURIs, quoteTarget.AttributedTo)
	}
	var localPoll *polls.Poll
	if command.Poll != nil {
		if h.polls == nil {
			return connector.PostCreated{}, fmt.Errorf("poll repository is not configured")
		}
		localPoll, err = appolls.NewLocalPoll(command.Poll.Choices, command.Poll.Multiple, command.Poll.ExpiresAt)
		if err != nil {
			return connector.PostCreated{}, err
		}
	}
	mentionURIs := command.MentionURIs
	visibleUserURIs := []string(nil)
	var specifiedRecipients []*actors.Actor
	if visibility == domainnotes.VisibilitySpecified {
		_, mentionURIs, err = h.resolveDirectRecipients(ctx, actor, mentionURIs)
		if err != nil {
			return connector.PostCreated{}, err
		}
		deliveryURIs := append(append([]string(nil), mentionURIs...), additionalRecipientURIs...)
		specifiedRecipients, visibleUserURIs, err = h.resolveSpecifiedRecipients(ctx, actor, deliveryURIs)
		if err != nil {
			return connector.PostCreated{}, err
		}
	} else if len(mentionURIs) > 0 {
		_, mentionURIs, err = h.resolveDirectRecipients(ctx, actor, mentionURIs)
		if err != nil {
			return connector.PostCreated{}, err
		}
	}
	now := time.Now().UTC()
	localEmojis, err := h.resolveLocalEmojis(ctx, command.EmojiNames)
	if err != nil {
		return connector.PostCreated{}, err
	}
	attachments, err := h.resolveLocalAttachments(ctx, actor.ID, command.MediaIDs, command.Sensitive)
	if err != nil {
		return connector.PostCreated{}, err
	}
	noteID, err := h.notes.NewID(ctx)
	if err != nil {
		return connector.PostCreated{}, fmt.Errorf("generate note id: %w", err)
	}
	noteURI := strings.TrimRight(h.cfg.PublicURL, "/") + "/notes/" + url.PathEscape(noteID)
	inReplyToURI, replyID := "", ""
	if replyTarget != nil {
		inReplyToURI, replyID = replyTarget.URI, replyTarget.ID
	}
	quoteURI, quoteID := "", ""
	if quoteTarget != nil {
		quoteURI, quoteID = quoteTarget.URI, quoteTarget.ID
	}
	note, err := h.notes.CreateLocalNote(ctx, domainnotes.Note{
		ID:              noteID,
		URI:             noteURI,
		AttributedTo:    actor.URI,
		AuthorID:        actor.ID,
		Text:            command.Text,
		ContentWarning:  command.ContentWarning,
		Sensitive:       command.Sensitive,
		InReplyToURI:    inReplyToURI,
		ReplyID:         replyID,
		QuoteURI:        quoteURI,
		QuoteID:         quoteID,
		Visibility:      visibility,
		MentionURIs:     mentionURIs,
		VisibleUserURIs: visibleUserURIs,
		Hashtags:        command.Hashtags,
		Emojis:          localEmojis,
		Attachments:     attachments,
		CreatedAt:       now,
		PublishedAt:     &now,
	})
	if err != nil {
		return connector.PostCreated{}, err
	}
	if localPoll != nil {
		localPoll.NoteID = note.ID
		localPoll.AuthorID = actor.ID
		localPoll.AuthorHost = nil
		localPoll.CreatedAt = now
		localPoll.UpdatedAt = now
		localPoll, err = h.polls.UpsertLocal(ctx, *localPoll)
		if err != nil {
			return connector.PostCreated{}, err
		}
		if localPoll.ExpiresAt != nil {
			if h.queue == nil {
				return connector.PostCreated{}, fmt.Errorf("queue is required for poll expiration")
			}
			if err := h.queue.Enqueue(ctx, queue.NewPollEndedTask(note.ID, localPoll.ExpiresAt.Sub(now))); err != nil {
				return connector.PostCreated{}, err
			}
		}
	}
	if note.Visibility == domainnotes.VisibilitySpecified {
		if err := h.enqueueSpecifiedCreateNoteDeliveries(ctx, actor, note, localPoll, specifiedRecipients); err != nil {
			return connector.PostCreated{}, err
		}
	} else {
		activity := apnotes.RenderCreateWithPoll(note, localPoll)
		if err := h.enqueueNoteActivityDeliveriesTo(ctx, actor, note, activity, additionalRecipientURIs); err != nil {
			return connector.PostCreated{}, err
		}
	}
	payload := connector.PostCreated{
		AccountID: actor.OwnerAccountID,
		ActorID:   actor.ID,
		NoteID:    note.ID,
		URI:       note.URI,
	}
	if h.connector != nil {
		if err := h.connector.PublishPostCreated(ctx, payload); err != nil {
			return connector.PostCreated{}, err
		}
	}
	return payload, nil
}

func (h *Handler) resolveLocalPostReference(ctx context.Context, actor *actors.Actor, rawURI, kind string) (*domainnotes.Note, error) {
	if h.resolver == nil {
		return nil, fmt.Errorf("note resolver is not configured")
	}
	uri := strings.TrimSpace(rawURI)
	target, err := h.resolver.ResolveNote(ctx, uri)
	if err != nil {
		return nil, fmt.Errorf("resolve %s target %s: %w", kind, uri, err)
	}
	if target == nil {
		return nil, fmt.Errorf("%s target not found: %s", kind, uri)
	}
	allowed, err := h.canReactToNote(ctx, actor, target)
	if err != nil {
		return nil, fmt.Errorf("validate %s target visibility: %w", kind, err)
	}
	if !allowed {
		return nil, fmt.Errorf("%s target is not visible to actor", kind)
	}
	return target, nil
}

func (h *Handler) createRenote(ctx context.Context, actor *actors.Actor, command connector.PostCreateCommand, visibility domainnotes.Visibility) (connector.PostCreated, error) {
	if strings.TrimSpace(command.Text) != "" || command.ContentWarning != nil || command.Sensitive || command.InReplyToURI != "" || command.QuoteURI != "" || len(command.MentionURIs) != 0 || len(command.Hashtags) != 0 || len(command.EmojiNames) != 0 || command.Poll != nil {
		return connector.PostCreated{}, fmt.Errorf("a pure renote cannot contain post content or metadata")
	}
	if visibility == domainnotes.VisibilitySpecified {
		return connector.PostCreated{}, fmt.Errorf("a pure renote cannot use specified visibility")
	}
	target, err := h.notes.FindByID(ctx, strings.TrimSpace(command.RenoteID))
	if err != nil {
		return connector.PostCreated{}, err
	}
	if target == nil {
		return connector.PostCreated{}, fmt.Errorf("renote target not found: %s", command.RenoteID)
	}
	if isPureRenote(target) {
		return connector.PostCreated{}, fmt.Errorf("cannot renote a pure renote")
	}
	targetHost, err := hostOf(target.URI)
	if err != nil {
		return connector.PostCreated{}, fmt.Errorf("invalid renote target URI: %w", err)
	}
	if h.cfg.IsFederationHostBlocked(targetHost) {
		return connector.PostCreated{}, fmt.Errorf("renote target host is blocked")
	}
	blocked, err := h.isBlockedPair(ctx, actor.ID, target.AuthorID)
	if err != nil {
		return connector.PostCreated{}, err
	}
	if blocked {
		return connector.PostCreated{}, fmt.Errorf("renote target relationship is blocked")
	}
	visibility, err = localRenoteVisibility(actor, target, visibility)
	if err != nil {
		return connector.PostCreated{}, err
	}
	now := time.Now().UTC()
	noteID, err := h.notes.NewID(ctx)
	if err != nil {
		return connector.PostCreated{}, fmt.Errorf("generate note id: %w", err)
	}
	noteURI := strings.TrimRight(h.cfg.PublicURL, "/") + "/notes/" + url.PathEscape(noteID)
	note, err := h.notes.CreateLocalNote(ctx, domainnotes.Note{
		ID: noteID, URI: noteURI, AttributedTo: actor.URI, AuthorID: actor.ID,
		RenoteID: target.ID, RenoteURI: target.URI, Visibility: visibility,
		CreatedAt: now, PublishedAt: &now,
	})
	if err != nil {
		return connector.PostCreated{}, err
	}
	if err := h.enqueueNoteActivityDeliveriesTo(ctx, actor, note, apnotes.RenderLocalAnnounce(note), []string{target.AttributedTo}); err != nil {
		return connector.PostCreated{}, err
	}
	payload := connector.PostCreated{AccountID: actor.OwnerAccountID, ActorID: actor.ID, NoteID: note.ID, URI: note.URI}
	if h.connector != nil {
		if err := h.connector.PublishPostCreated(ctx, payload); err != nil {
			return connector.PostCreated{}, err
		}
	}
	return payload, nil
}

func localRenoteVisibility(actor *actors.Actor, target *domainnotes.Note, requested domainnotes.Visibility) (domainnotes.Visibility, error) {
	switch target.Visibility {
	case domainnotes.VisibilityPublic:
		return requested, nil
	case domainnotes.VisibilityHome:
		if requested == domainnotes.VisibilityPublic {
			return domainnotes.VisibilityHome, nil
		}
		return requested, nil
	case domainnotes.VisibilityFollowers:
		if actor == nil || actor.ID != target.AuthorID {
			return "", fmt.Errorf("renote target is not shareable")
		}
		return domainnotes.VisibilityFollowers, nil
	default:
		return "", fmt.Errorf("renote target is not shareable")
	}
}

func isPureRenote(note *domainnotes.Note) bool {
	return note != nil && note.RenoteID != "" && note.Text == "" && note.ContentWarning == nil && note.InReplyToURI == "" && note.QuoteURI == "" && len(note.Attachments) == 0
}

func (h *Handler) resolveLocalEmojis(ctx context.Context, names []string) ([]domainnotes.Emoji, error) {
	if len(names) == 0 {
		return nil, nil
	}
	if len(names) > 100 {
		return nil, fmt.Errorf("at most 100 custom emojis are allowed")
	}
	for _, name := range names {
		if !validLocalEmojiName(strings.Trim(strings.TrimSpace(name), ":")) {
			return nil, fmt.Errorf("invalid local custom emoji name: %q", name)
		}
	}
	if h.emojis == nil {
		return nil, fmt.Errorf("emoji repository is not configured")
	}
	stored, err := h.emojis.FindLocalByNames(ctx, names)
	if err != nil {
		return nil, err
	}
	result := make([]domainnotes.Emoji, 0, len(stored))
	for _, emoji := range stored {
		iconURL := emoji.PublicURL
		if iconURL == "" {
			iconURL = emoji.OriginalURL
		}
		if iconURL == "" {
			continue
		}
		updatedAt := emoji.UpdatedAt
		result = append(result, domainnotes.Emoji{
			Name: emoji.Name, URI: emoji.URI, IconURL: iconURL,
			MediaType: emoji.MediaType, UpdatedAt: &updatedAt,
		})
	}
	return result, nil
}

func (h *Handler) resolveLocalAttachments(ctx context.Context, actorID string, mediaIDs []string, sensitive bool) ([]domainnotes.Attachment, error) {
	if len(mediaIDs) == 0 {
		return nil, nil
	}
	if len(mediaIDs) > 4 {
		return nil, fmt.Errorf("at most 4 media attachments are allowed")
	}
	if h.media == nil {
		return nil, fmt.Errorf("media repository is not configured")
	}
	seen := make(map[string]struct{}, len(mediaIDs))
	attachments := make([]domainnotes.Attachment, 0, len(mediaIDs))
	for _, mediaID := range mediaIDs {
		mediaID = strings.TrimSpace(mediaID)
		if mediaID == "" {
			return nil, fmt.Errorf("media id is required")
		}
		if _, duplicate := seen[mediaID]; duplicate {
			return nil, fmt.Errorf("duplicate media id: %s", mediaID)
		}
		seen[mediaID] = struct{}{}
		item, err := h.media.FindByID(ctx, mediaID)
		if err != nil {
			return nil, err
		}
		if item == nil || item.State != domainmedia.StateReady || item.OwnerActorID != actorID {
			return nil, fmt.Errorf("media is not an owned ready upload: %s", mediaID)
		}
		if !strings.HasPrefix(item.ContentType, "image/") {
			return nil, fmt.Errorf("media is not an image: %s", mediaID)
		}
		attachments = append(attachments, domainnotes.Attachment{
			Type: "Image", MediaType: item.ContentType, URL: item.PublicURL,
			Name: item.Name, Width: item.Width, Height: item.Height, Sensitive: sensitive,
		})
	}
	return attachments, nil
}

func validLocalEmojiName(name string) bool {
	if name == "" || len(name) > 100 {
		return false
	}
	for _, char := range name {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '_' {
			return false
		}
	}
	return true
}

func (h *Handler) VotePoll(ctx context.Context, command connector.PollVoteCommand) (connector.PollVoted, error) {
	if h.notes == nil || h.polls == nil {
		return connector.PollVoted{}, fmt.Errorf("note and poll repositories are required")
	}
	actor, err := h.repo.FindLocalByID(ctx, strings.TrimSpace(command.ActorID))
	if err != nil {
		return connector.PollVoted{}, err
	}
	if actor == nil {
		return connector.PollVoted{}, fmt.Errorf("local actor not found: %s", command.ActorID)
	}
	note, err := h.notes.FindByID(ctx, strings.TrimSpace(command.NoteID))
	if err != nil {
		return connector.PollVoted{}, err
	}
	if note == nil {
		return connector.PollVoted{}, fmt.Errorf("poll note not found: %s", command.NoteID)
	}
	allowed, err := h.canReactToNote(ctx, actor, note)
	if err != nil {
		return connector.PollVoted{}, err
	}
	if !allowed {
		return connector.PollVoted{}, fmt.Errorf("poll is not visible to actor")
	}
	now := time.Now().UTC()
	vote, poll, err := h.polls.RecordVote(ctx, note.ID, actor.ID, command.Choice, now)
	if err != nil && !errors.Is(err, polls.ErrAlreadyVoted) {
		return connector.PollVoted{}, err
	}
	result := connector.PollVoted{VoteID: vote.ID, NoteID: note.ID, Choice: vote.Choice}
	if poll.AuthorHost != nil {
		if h.queue == nil {
			return connector.PollVoted{}, fmt.Errorf("queue is required for remote poll vote")
		}
		owner, err := h.repo.FindByURI(ctx, note.AttributedTo)
		if err != nil {
			return connector.PollVoted{}, err
		}
		if owner == nil || owner.Host == nil || strings.TrimSpace(owner.Inbox) == "" {
			return connector.PollVoted{}, fmt.Errorf("remote poll owner inbox is unavailable")
		}
		activity := appolls.RenderVote(actor.URI, vote, note, poll, now)
		if err := h.queue.Enqueue(ctx, queue.NewDeliverTask(actor.ID, owner.Inbox, activity, h.cfg.DeliverQueue.MaxRetry, h.cfg.DeliverQueue.Timeout)); err != nil {
			return connector.PollVoted{}, err
		}
		result.URI, _ = activity["id"].(string)
	} else {
		owner, err := h.repo.FindLocalByID(ctx, poll.AuthorID)
		if err != nil {
			return connector.PollVoted{}, err
		}
		if owner == nil {
			return connector.PollVoted{}, fmt.Errorf("local poll owner is unavailable")
		}
		activity := apnotes.RenderQuestionUpdate(note, poll, now)
		if err := h.enqueueNoteActivityDeliveries(ctx, owner, note, activity); err != nil {
			return connector.PollVoted{}, err
		}
	}
	return result, nil
}

func (h *Handler) DeletePost(ctx context.Context, command connector.PostDeleteCommand) (connector.PostDeleted, error) {
	if h.notes == nil || h.queue == nil {
		return connector.PostDeleted{}, fmt.Errorf("note repository and queue are required")
	}
	actor, err := h.repo.FindLocalByID(ctx, strings.TrimSpace(command.ActorID))
	if err != nil {
		return connector.PostDeleted{}, err
	}
	if actor == nil {
		return connector.PostDeleted{}, fmt.Errorf("local actor not found: %s", command.ActorID)
	}
	note, err := h.notes.FindAnyByID(ctx, strings.TrimSpace(command.NoteID))
	if err != nil {
		return connector.PostDeleted{}, err
	}
	if note == nil || note.AuthorID != actor.ID || note.AttributedTo != actor.URI {
		return connector.PostDeleted{}, fmt.Errorf("owned local note not found: %s", command.NoteID)
	}
	var renoteTarget *domainnotes.Note
	if isPureRenote(note) {
		renoteTarget, err = h.notes.FindAnyByID(ctx, note.RenoteID)
		if err != nil {
			return connector.PostDeleted{}, err
		}
	}
	additionalRecipients, err := h.activeReferenceAuthorURIs(ctx, note.ID)
	if err != nil {
		return connector.PostDeleted{}, fmt.Errorf("list remote post references: %w", err)
	}
	deletedAt := time.Now().UTC()
	if note.DeletedAt == nil {
		if err := h.notes.DeleteLocalNote(ctx, note.ID, actor.ID); err != nil {
			return connector.PostDeleted{}, err
		}
	} else {
		deletedAt = note.DeletedAt.UTC()
	}
	if err := h.cleanupNote(ctx, note.ID); err != nil {
		return connector.PostDeleted{}, err
	}
	activity := apnotes.RenderDelete(note, deletedAt)
	if renoteTarget != nil {
		activity = apnotes.RenderUndoAnnounce(note, deletedAt)
		additionalRecipients = append(additionalRecipients, renoteTarget.AttributedTo)
	}
	if err := h.enqueueNoteActivityDeliveriesTo(ctx, actor, note, activity, additionalRecipients); err != nil {
		return connector.PostDeleted{}, err
	}
	return connector.PostDeleted{ActorID: actor.ID, NoteID: note.ID, URI: note.URI}, nil
}

func (h *Handler) activeReferenceAuthorURIs(ctx context.Context, noteID string) ([]string, error) {
	result := make([]string, 0)
	afterURI := ""
	for {
		page, err := h.notes.ListActiveReferenceAuthorURIsPage(ctx, noteID, afterURI, postDeliveryFollowerLimit)
		if err != nil {
			return nil, err
		}
		result = append(result, page...)
		if len(page) < postDeliveryFollowerLimit {
			return result, nil
		}
		next := page[len(page)-1]
		if next <= afterURI {
			return nil, fmt.Errorf("reference author pagination did not advance")
		}
		afterURI = next
	}
}

func (h *Handler) CreateReaction(ctx context.Context, command connector.ReactionCreateCommand) (connector.ReactionCreated, error) {
	if h.notes == nil || h.reactions == nil || h.queue == nil {
		return connector.ReactionCreated{}, fmt.Errorf("note repository, reaction repository, and queue are required")
	}
	actor, err := h.repo.FindLocalByID(ctx, strings.TrimSpace(command.ActorID))
	if err != nil {
		return connector.ReactionCreated{}, err
	}
	if actor == nil {
		return connector.ReactionCreated{}, fmt.Errorf("local actor not found: %s", command.ActorID)
	}
	note, err := h.notes.FindByID(ctx, strings.TrimSpace(command.NoteID))
	if err != nil {
		return connector.ReactionCreated{}, err
	}
	if note == nil {
		return connector.ReactionCreated{}, fmt.Errorf("note not found: %s", command.NoteID)
	}
	allowed, err := h.canReactToNote(ctx, actor, note)
	if err != nil {
		return connector.ReactionCreated{}, err
	}
	if !allowed {
		return connector.ReactionCreated{}, fmt.Errorf("note is not visible to actor")
	}
	reactionValue := strings.TrimSpace(command.Reaction)
	if reactionValue == "" {
		return connector.ReactionCreated{}, fmt.Errorf("reaction is required")
	}
	reactionValue, localEmoji, err := h.resolveLocalReactionEmoji(ctx, reactionValue)
	if err != nil {
		return connector.ReactionCreated{}, err
	}
	recipient, err := h.repo.FindByURI(ctx, note.AttributedTo)
	if err != nil {
		return connector.ReactionCreated{}, err
	}
	if recipient == nil || recipient.Host == nil {
		return connector.ReactionCreated{}, fmt.Errorf("reaction target author is not remote")
	}
	inbox := strings.TrimSpace(recipient.Inbox)
	if inbox == "" {
		return connector.ReactionCreated{}, fmt.Errorf("reaction target inbox is empty")
	}
	reactionRecord := reactions.Reaction{
		NoteID:    note.ID,
		NoteURI:   note.URI,
		ActorID:   actor.ID,
		ActorURI:  actor.URI,
		ActorHost: actor.Host,
		Reaction:  reactionValue,
		CreatedAt: time.Now().UTC(),
	}
	if localEmoji != nil {
		reactionRecord.EmojiName = localEmoji.Name
		reactionRecord.EmojiURL = localEmoji.PublicURL
		if reactionRecord.EmojiURL == "" {
			reactionRecord.EmojiURL = localEmoji.OriginalURL
		}
		reactionRecord.EmojiMediaType = localEmoji.MediaType
	}
	stored, err := h.reactions.Upsert(ctx, reactionRecord)
	if err != nil {
		return connector.ReactionCreated{}, err
	}
	activity := apreactions.RenderLikeWithEmoji(h.cfg.PublicURL, stored, localEmoji)
	task := queue.NewDeliverTask(actor.ID, inbox, activity, h.cfg.DeliverQueue.MaxRetry, h.cfg.DeliverQueue.Timeout)
	if err := h.queue.Enqueue(ctx, task); err != nil {
		return connector.ReactionCreated{}, fmt.Errorf("enqueue Like delivery: %w", err)
	}
	return connector.ReactionCreated{
		ReactionID: stored.ID,
		NoteID:     stored.NoteID,
		Reaction:   stored.Reaction,
		URI:        activity["id"].(string),
	}, nil
}

func (h *Handler) DeleteReaction(ctx context.Context, command connector.ReactionDeleteCommand) (connector.ReactionDeleted, error) {
	if h.notes == nil || h.reactions == nil || h.queue == nil {
		return connector.ReactionDeleted{}, fmt.Errorf("note repository, reaction repository, and queue are required")
	}
	actor, err := h.repo.FindLocalByID(ctx, strings.TrimSpace(command.ActorID))
	if err != nil {
		return connector.ReactionDeleted{}, err
	}
	if actor == nil {
		return connector.ReactionDeleted{}, fmt.Errorf("local actor not found: %s", command.ActorID)
	}
	note, err := h.notes.FindByID(ctx, strings.TrimSpace(command.NoteID))
	if err != nil {
		return connector.ReactionDeleted{}, err
	}
	if note == nil {
		return connector.ReactionDeleted{}, fmt.Errorf("note not found: %s", command.NoteID)
	}
	existing, err := h.reactions.Find(ctx, note.ID, actor.ID)
	if err != nil {
		return connector.ReactionDeleted{}, err
	}
	if existing == nil {
		return connector.ReactionDeleted{}, fmt.Errorf("reaction not found")
	}
	recipient, err := h.repo.FindByURI(ctx, note.AttributedTo)
	if err != nil {
		return connector.ReactionDeleted{}, err
	}
	if recipient == nil || recipient.Host == nil {
		return connector.ReactionDeleted{}, fmt.Errorf("reaction target author is not remote")
	}
	inbox := strings.TrimSpace(recipient.Inbox)
	if inbox == "" {
		return connector.ReactionDeleted{}, fmt.Errorf("reaction target inbox is empty")
	}
	_, localEmoji, err := h.resolveLocalReactionEmoji(ctx, existing.Reaction)
	if err != nil {
		return connector.ReactionDeleted{}, err
	}
	undo := apreactions.RenderUndoLikeWithEmoji(h.cfg.PublicURL, existing, localEmoji, time.Now().UTC())
	if err := h.reactions.Delete(ctx, note.ID, actor.ID, ""); err != nil {
		return connector.ReactionDeleted{}, err
	}
	task := queue.NewDeliverTask(actor.ID, inbox, undo, h.cfg.DeliverQueue.MaxRetry, h.cfg.DeliverQueue.Timeout)
	if err := h.queue.Enqueue(ctx, task); err != nil {
		return connector.ReactionDeleted{}, fmt.Errorf("enqueue Undo(Like) delivery: %w", err)
	}
	return connector.ReactionDeleted{
		ReactionID: existing.ID,
		NoteID:     existing.NoteID,
		URI:        undo["id"].(string),
	}, nil
}

func (h *Handler) resolveLocalReactionEmoji(ctx context.Context, reaction string) (string, *emojis.Emoji, error) {
	name, local := apreactions.LocalEmojiName(reaction)
	if !local {
		return reaction, nil, nil
	}
	if !validLocalEmojiName(name) {
		return "", nil, fmt.Errorf("invalid local custom emoji reaction: %q", reaction)
	}
	if h.emojis == nil {
		return "", nil, fmt.Errorf("emoji repository is not configured")
	}
	emoji, err := h.emojis.FindLocalByName(ctx, name)
	if err != nil {
		return "", nil, fmt.Errorf("resolve local reaction emoji: %w", err)
	}
	if emoji == nil || (emoji.PublicURL == "" && emoji.OriginalURL == "") {
		return "", nil, fmt.Errorf("local custom emoji not found: %s", name)
	}
	return ":" + name + "@.:", emoji, nil
}

func (h *Handler) canReactToNote(ctx context.Context, actor *actors.Actor, note *domainnotes.Note) (bool, error) {
	blocked, err := h.isBlockedPair(ctx, actor.ID, note.AuthorID)
	if err != nil || blocked {
		return false, err
	}
	switch note.Visibility {
	case domainnotes.VisibilityPublic, domainnotes.VisibilityHome:
		return true, nil
	case domainnotes.VisibilityFollowers:
		if h.follows == nil {
			return false, nil
		}
		follow, err := h.follows.Find(ctx, actor.ID, note.AuthorID)
		return err == nil && follow != nil && follow.Status == follows.StatusAccepted, err
	case domainnotes.VisibilitySpecified:
		audience := note.VisibleUserURIs
		if len(audience) == 0 {
			audience = note.MentionURIs
		}
		for _, uri := range audience {
			if uri == actor.URI {
				return true, nil
			}
		}
	}
	return false, nil
}

func (h *Handler) resolveSpecifiedRecipients(ctx context.Context, actor *actors.Actor, mentionURIs []string) ([]*actors.Actor, []string, error) {
	recipients, uris, err := h.resolveDirectRecipients(ctx, actor, mentionURIs)
	if err != nil {
		return nil, nil, err
	}
	if len(recipients) == 0 {
		return nil, nil, fmt.Errorf("specified visibility requires at least one visible recipient")
	}
	return recipients, uris, nil
}

func (h *Handler) resolveDirectRecipients(ctx context.Context, actor *actors.Actor, mentionURIs []string) ([]*actors.Actor, []string, error) {
	if h.resolver == nil {
		return nil, nil, fmt.Errorf("actor resolver is not configured")
	}
	recipients := make([]*actors.Actor, 0, len(mentionURIs))
	uris := make([]string, 0, len(mentionURIs))
	seen := make(map[string]struct{}, len(mentionURIs))
	for _, rawURI := range mentionURIs {
		uri := strings.TrimSpace(rawURI)
		if uri == "" {
			continue
		}
		if _, exists := seen[uri]; exists {
			continue
		}
		recipient, err := h.resolver.ResolveActor(ctx, uri)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve specified recipient %s: %w", uri, err)
		}
		blocked, err := h.isBlockedPair(ctx, actor.ID, recipient.ID)
		if err != nil {
			return nil, nil, err
		}
		if blocked {
			continue
		}
		seen[uri] = struct{}{}
		uris = append(uris, uri)
		recipients = append(recipients, recipient)
	}
	return recipients, uris, nil
}

func (h *Handler) enqueueSpecifiedCreateNoteDeliveries(ctx context.Context, actor *actors.Actor, note *domainnotes.Note, poll *polls.Poll, recipients []*actors.Actor) error {
	if h.queue == nil {
		return fmt.Errorf("queue is required for specified post delivery")
	}
	activity := apnotes.RenderCreateWithPoll(note, poll)
	destinations := make(map[string]struct{}, len(recipients))
	for _, recipient := range recipients {
		if recipient == nil || recipient.Host == nil {
			continue
		}
		if h.cfg.IsFederationHostBlocked(*recipient.Host) {
			continue
		}
		blocked, err := h.isBlockedPair(ctx, actor.ID, recipient.ID)
		if err != nil {
			return err
		}
		if blocked {
			continue
		}
		inbox := strings.TrimSpace(recipient.Inbox)
		if inbox == "" {
			return fmt.Errorf("specified recipient inbox is empty: %s", recipient.URI)
		}
		if _, exists := destinations[inbox]; exists {
			continue
		}
		destinations[inbox] = struct{}{}
		task := queue.NewDeliverTask(actor.ID, inbox, activity, h.cfg.DeliverQueue.MaxRetry, h.cfg.DeliverQueue.Timeout)
		if err := h.queue.Enqueue(ctx, task); err != nil {
			return fmt.Errorf("enqueue specified Create(Note) delivery to %s: %w", inbox, err)
		}
	}
	return nil
}

func (h *Handler) enqueueNoteActivityDeliveries(ctx context.Context, actor *actors.Actor, note *domainnotes.Note, activity map[string]any) error {
	return h.enqueueNoteActivityDeliveriesTo(ctx, actor, note, activity, nil)
}

func (h *Handler) enqueueNoteActivityDeliveriesTo(ctx context.Context, actor *actors.Actor, note *domainnotes.Note, activity map[string]any, additionalRecipientURIs []string) error {
	if h.follows == nil || h.queue == nil {
		return fmt.Errorf("follow repository and queue are required for post delivery")
	}
	destinations := make(map[string]struct{})
	recipientIDs := make(map[string]struct{})
	if note.Visibility != domainnotes.VisibilitySpecified {
		afterID := ""
		for {
			followers, err := h.follows.ListFollowersPage(ctx, actor.ID, afterID, postDeliveryFollowerLimit)
			if err != nil {
				return fmt.Errorf("list followers for post delivery: %w", err)
			}
			remoteIDs := make([]string, 0, len(followers))
			for _, follow := range followers {
				if follow.FollowerHost != nil {
					remoteIDs = append(remoteIDs, follow.FollowerID)
				}
			}
			activeRemoteIDs, err := h.repo.FilterActiveRemoteIDs(ctx, remoteIDs)
			if err != nil {
				return fmt.Errorf("filter active post recipients: %w", err)
			}
			for _, follow := range followers {
				if follow.FollowerHost == nil {
					continue
				}
				if _, active := activeRemoteIDs[follow.FollowerID]; !active {
					continue
				}
				if h.cfg.IsFederationHostBlocked(*follow.FollowerHost) {
					continue
				}
				blocked, err := h.isBlockedPair(ctx, actor.ID, follow.FollowerID)
				if err != nil {
					return err
				}
				if blocked {
					continue
				}
				inbox := strings.TrimSpace(follow.FollowerSharedInbox)
				isSharedInbox := inbox != ""
				if inbox == "" {
					inbox = strings.TrimSpace(follow.FollowerInbox)
				}
				if err := h.enqueueNoteActivity(ctx, actor.ID, inbox, activity, destinations, isSharedInbox); err != nil {
					return err
				}
				recipientIDs[follow.FollowerID] = struct{}{}
			}
			if len(followers) < postDeliveryFollowerLimit {
				break
			}
			afterID = followers[len(followers)-1].ID
		}
	}
	directURIs := append([]string(nil), note.MentionURIs...)
	if note.Visibility == domainnotes.VisibilitySpecified && len(note.VisibleUserURIs) > 0 {
		directURIs = append([]string(nil), note.VisibleUserURIs...)
	}
	directURIs = append(directURIs, additionalRecipientURIs...)
	for _, uri := range directURIs {
		recipient, err := h.resolver.ResolveActor(ctx, uri)
		if err != nil {
			return fmt.Errorf("resolve direct post recipient %s: %w", uri, err)
		}
		if recipient == nil || recipient.Host == nil {
			continue
		}
		if _, exists := recipientIDs[recipient.ID]; exists {
			continue
		}
		if h.cfg.IsFederationHostBlocked(*recipient.Host) {
			continue
		}
		blocked, err := h.isBlockedPair(ctx, actor.ID, recipient.ID)
		if err != nil {
			return err
		}
		if blocked {
			continue
		}
		if err := h.enqueueNoteActivity(ctx, actor.ID, strings.TrimSpace(recipient.Inbox), activity, destinations, false); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) enqueueNoteActivity(ctx context.Context, actorID, inbox string, activity map[string]any, destinations map[string]struct{}, isSharedInbox bool) error {
	if inbox == "" {
		return nil
	}
	if _, exists := destinations[inbox]; exists {
		return nil
	}
	destinations[inbox] = struct{}{}
	task := queue.NewDeliverTask(actorID, inbox, activity, h.cfg.DeliverQueue.MaxRetry, h.cfg.DeliverQueue.Timeout)
	if isSharedInbox {
		task = queue.NewSharedInboxDeliverTask(actorID, inbox, activity, h.cfg.DeliverQueue.MaxRetry, h.cfg.DeliverQueue.Timeout)
	}
	if err := h.queue.Enqueue(ctx, task); err != nil {
		return fmt.Errorf("enqueue note activity delivery to %s: %w", inbox, err)
	}
	return nil
}
