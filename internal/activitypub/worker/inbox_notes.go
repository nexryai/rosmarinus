package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	apnotes "github.com/nexryai/rosmarinus/internal/activitypub/notes"
	appolls "github.com/nexryai/rosmarinus/internal/activitypub/polls"
	aptypes "github.com/nexryai/rosmarinus/internal/activitypub/types"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	domainnotes "github.com/nexryai/rosmarinus/internal/domain/notes"
	"github.com/nexryai/rosmarinus/internal/domain/polls"
)

func (h *Handler) performCreate(ctx context.Context, actor *actors.Actor, activity map[string]any) (string, error) {
	if h.notes == nil {
		return "skip: note repository is not configured", nil
	}
	object, err := h.createObject(ctx, activity)
	if err != nil {
		return "", err
	}
	if !aptypes.IsPost(object) {
		return fmt.Sprintf("skip: unknown create object type %v", object["type"]), nil
	}
	attributedTo, err := aptypes.GetOneAPID(object["attributedTo"])
	if err != nil {
		return "skip: note attributedTo is invalid", nil
	}
	if actor.URI != attributedTo {
		return "skip: actor.uri !== note.attributedTo", nil
	}
	uri, err := aptypes.GetAPID(object)
	if err != nil {
		return "skip: note.id is not a string", nil
	}
	actorAuthority, err := aptypes.Authority(actor.URI)
	if err != nil {
		return "skip: actor uri host is invalid", nil
	}
	noteAuthority, err := aptypes.Authority(uri)
	if err != nil || actorAuthority != noteAuthority {
		return "skip: host in actor.uri !== note.id", nil
	}
	existing, err := h.notes.FindByURI(ctx, uri)
	if err != nil {
		return "", err
	}
	if existing != nil {
		if existing.AuthorID != actor.ID || existing.AttributedTo != actor.URI {
			return "skip: existing note belongs to a different actor", nil
		}
		if err := h.storeRemotePoll(ctx, actor, existing, object); err != nil {
			return "", err
		}
		h.scheduleNoteMedia(ctx, existing)
		var reply *domainnotes.Note
		if existing.ReplyID != "" {
			reply, err = h.notes.FindByID(ctx, existing.ReplyID)
			if err != nil {
				return "", err
			}
		}
		activityID, _ := activity["id"].(string)
		if err := h.createNoteNotifications(ctx, actor, existing, reply, activityID); err != nil {
			return "", err
		}
		return "skip: note exists", nil
	}
	parsed, err := apnotes.ParseRemoteNote(object, uri)
	if err != nil {
		return fmt.Sprintf("skip: invalid note: %v", err), nil
	}
	if err := h.upsertRemoteEmojis(ctx, actor, parsed.Emojis); err != nil && h.logger != nil {
		h.logger.Printf("activitypub: store Create emoji tags: %v", err)
	}
	reply, quote, err := h.resolver.ResolveNoteLinks(ctx, parsed.URI, parsed.InReplyToURI, parsed.QuoteURIs...)
	if err != nil {
		return "", err
	}
	if handled, result, err := h.performRemotePollVote(ctx, actor, object, reply); handled {
		return result, err
	}
	note := domainnotes.Note{
		URI:             parsed.URI,
		AttributedTo:    parsed.AttributedTo,
		AuthorID:        actor.ID,
		Text:            parsed.Text,
		ContentWarning:  parsed.ContentWarning,
		Sensitive:       parsed.Sensitive,
		InReplyToURI:    parsed.InReplyToURI,
		QuoteURI:        parsed.QuoteURI,
		Visibility:      domainnotes.Visibility(parsed.Visibility),
		MentionURIs:     parsed.MentionURIs,
		VisibleUserURIs: parsed.VisibleUserURIs,
		Hashtags:        parsed.Hashtags,
		Emojis:          parsed.Emojis,
		Attachments:     parsed.Attachments,
		Raw:             object,
		CreatedAt:       time.Now().UTC(),
		PublishedAt:     publishedAt(object),
	}
	if reply != nil {
		note.ReplyID = reply.ID
	}
	if quote != nil {
		note.QuoteID = quote.ID
		note.QuoteURI = quote.URI
	}
	stored, err := h.notes.UpsertRemoteNote(ctx, note)
	if err != nil {
		return "", err
	}
	if err := h.storeRemotePoll(ctx, actor, stored, object); err != nil {
		return "", err
	}
	h.scheduleNoteMedia(ctx, stored)
	activityID, _ := activity["id"].(string)
	if err := h.createNoteNotifications(ctx, actor, stored, reply, activityID); err != nil {
		return "", err
	}
	if err := h.publishRemoteNoteCreated(ctx, actor, stored); err != nil && h.logger != nil {
		h.logger.Printf("realtime: publish remote note created uri=%s: %v", stored.URI, err)
	}
	return "ok: note created", nil
}

func (h *Handler) storeRemotePoll(ctx context.Context, actor *actors.Actor, note *domainnotes.Note, object map[string]any) error {
	if h.polls == nil || actor == nil || note == nil || !aptypes.IsType(object, "Question") || !aptypes.IsType(note.Raw, "Question") {
		return nil
	}
	poll, err := appolls.ParseQuestion(object)
	if err != nil {
		if h.logger != nil {
			h.logger.Printf("activitypub: ignore invalid Question uri=%s: %v", note.URI, err)
		}
		return nil
	}
	poll.NoteID = note.ID
	poll.AuthorID = actor.ID
	poll.AuthorHost = actor.Host
	_, err = h.polls.UpsertRemote(ctx, *poll)
	return err
}

func (h *Handler) scheduleActorMedia(ctx context.Context, actor *actors.Actor) {
	if actor == nil {
		return
	}
	for _, rawURL := range []string{actor.AvatarURL, actor.BannerURL} {
		if err := h.ScheduleMedia(ctx, rawURL); err != nil && h.logger != nil {
			h.logger.Printf("media: schedule actor source=%s: %v", rawURL, err)
		}
	}
}

func (h *Handler) scheduleNoteMedia(ctx context.Context, note *domainnotes.Note) {
	if note == nil {
		return
	}
	for _, attachment := range note.Attachments {
		if err := h.ScheduleMedia(ctx, attachment.URL); err != nil && h.logger != nil {
			h.logger.Printf("media: schedule note attachment=%s: %v", attachment.URL, err)
		}
	}
	for _, emoji := range note.Emojis {
		if err := h.ScheduleMedia(ctx, emoji.IconURL); err != nil && h.logger != nil {
			h.logger.Printf("media: schedule note emoji=%s: %v", emoji.IconURL, err)
		}
	}
}

func (h *Handler) performRemotePollVote(ctx context.Context, actor *actors.Actor, object map[string]any, reply *domainnotes.Note) (bool, string, error) {
	name, _ := object["name"].(string)
	name = strings.TrimSpace(name)
	if h.polls == nil || reply == nil || name == "" {
		return false, "", nil
	}
	poll, err := h.polls.FindByNoteID(ctx, reply.ID)
	if err != nil {
		return true, "", err
	}
	if poll == nil {
		return false, "", nil
	}
	choice := -1
	for i, value := range poll.Choices {
		if value == name {
			choice = i
			break
		}
	}
	if choice < 0 {
		return true, "skip: poll choice not found", nil
	}
	allowed, err := h.canReactToNote(ctx, actor, reply)
	if err != nil {
		return true, "", err
	}
	if !allowed {
		return true, "skip: poll is not visible to actor", nil
	}
	_, updated, err := h.polls.RecordVote(ctx, reply.ID, actor.ID, choice, time.Now().UTC())
	alreadyVoted := errors.Is(err, polls.ErrAlreadyVoted)
	if errors.Is(err, polls.ErrExpired) {
		return true, "skip: poll expired", nil
	}
	if err != nil && !alreadyVoted {
		return true, "", err
	}
	if updated.AuthorHost == nil {
		owner, err := h.repo.FindLocalByID(ctx, updated.AuthorID)
		if err != nil {
			return true, "", err
		}
		if owner != nil {
			activity := apnotes.RenderQuestionUpdate(reply, updated, time.Now().UTC())
			if err := h.enqueueNoteActivityDeliveries(ctx, owner, reply, activity); err != nil {
				return true, "", err
			}
		}
	}
	if alreadyVoted {
		return true, "skip: already voted", nil
	}
	return true, "ok: poll vote created", nil
}

func (h *Handler) createObject(ctx context.Context, activity map[string]any) (map[string]any, error) {
	object := activity["object"]
	if obj, ok := object.(map[string]any); ok {
		copyCreateAudience(activity, obj)
		if obj["attributedTo"] == nil {
			obj["attributedTo"] = activity["actor"]
		}
		return obj, nil
	}
	uri, err := aptypes.GetAPID(object)
	if err != nil {
		return nil, fmt.Errorf("create object is invalid: %w", err)
	}
	obj, err := h.client.FetchObject(ctx, uri, h.localActor)
	if err != nil {
		return nil, fmt.Errorf("resolve create object: %w", err)
	}
	return obj, nil
}
