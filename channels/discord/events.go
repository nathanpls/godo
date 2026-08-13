package discord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// EventType identifies a Discord Gateway dispatch event.
type EventType string

const (
	EventReady             EventType = "READY"
	EventResumed           EventType = "RESUMED"
	EventMessageCreate     EventType = "MESSAGE_CREATE"
	EventMessageUpdate     EventType = "MESSAGE_UPDATE"
	EventMessageDelete     EventType = "MESSAGE_DELETE"
	EventReactionAdd       EventType = "MESSAGE_REACTION_ADD"
	EventReactionRemove    EventType = "MESSAGE_REACTION_REMOVE"
	EventThreadCreate      EventType = "THREAD_CREATE"
	EventThreadUpdate      EventType = "THREAD_UPDATE"
	EventThreadDelete      EventType = "THREAD_DELETE"
	EventInteractionCreate EventType = "INTERACTION_CREATE"
)

// RawHandler handles an untyped Gateway dispatch. Raw handlers bypass the
// configured AccessPolicy.
type RawHandler func(context.Context, json.RawMessage) error

// MessageHandler handles a message create event.
type MessageHandler func(context.Context, *MessageEvent) error

// ReactionHandler handles a reaction add or remove event.
type ReactionHandler func(context.Context, *ReactionEvent) error

// ThreadHandler handles a thread create, update, or delete event.
type ThreadHandler func(context.Context, *ThreadEvent) error

type handlers struct {
	raw       map[EventType][]RawHandler
	messages  []MessageHandler
	reactions []ReactionHandler
	threads   []ThreadHandler
}

// MessageEvent is a typed MESSAGE_CREATE dispatch.
type MessageEvent struct {
	Message Message
	Raw     json.RawMessage
	client  *Client
}

// Reply replies to the source message.
func (e *MessageEvent) Reply(ctx context.Context, message MessageCreate) error {
	_, err := e.client.Reply(ctx, e.Message.ChannelID, e.Message.ID, message)
	return err
}

// React adds the bot's reaction to the source message.
func (e *MessageEvent) React(ctx context.Context, emoji string) error {
	return e.client.React(ctx, e.Message.ChannelID, e.Message.ID, emoji)
}

// Emoji identifies a Unicode or custom Discord emoji.
type Emoji struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Animated bool   `json:"animated"`
}

// String returns the value accepted by Discord reaction endpoints.
func (e Emoji) String() string {
	if e.ID == "" {
		return e.Name
	}
	return e.Name + ":" + e.ID
}

// ReactionEvent is a typed reaction add or remove dispatch.
type ReactionEvent struct {
	Type      EventType
	UserID    string
	ChannelID string
	MessageID string
	GuildID   string
	Emoji     Emoji
	Raw       json.RawMessage
}

// Added reports whether the reaction was added rather than removed.
func (e *ReactionEvent) Added() bool {
	return e.Type == EventReactionAdd
}

// ThreadEvent is a typed thread create, update, or delete dispatch.
type ThreadEvent struct {
	Type   EventType
	Thread Channel
	Raw    json.RawMessage
}

// On registers a raw handler for a Gateway dispatch type. Raw handlers bypass
// access filtering. Handlers cannot be registered while Run is active.
func (c *Client) On(eventType EventType, handler RawHandler) error {
	if eventType == "" || handler == nil {
		return errors.New("discord raw event type and handler are required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return errors.New("discord handlers cannot be registered after Run starts")
	}
	if c.handlers.raw == nil {
		c.handlers.raw = make(map[EventType][]RawHandler)
	}
	c.handlers.raw[eventType] = append(c.handlers.raw[eventType], handler)
	return nil
}

// OnMessage registers a MESSAGE_CREATE handler.
func (c *Client) OnMessage(handler MessageHandler) error {
	if handler == nil {
		return errors.New("discord message handler is required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return errors.New("discord handlers cannot be registered after Run starts")
	}
	c.handlers.messages = append(c.handlers.messages, handler)
	return nil
}

// OnReaction registers a handler for reaction add and remove events.
func (c *Client) OnReaction(handler ReactionHandler) error {
	if handler == nil {
		return errors.New("discord reaction handler is required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return errors.New("discord handlers cannot be registered after Run starts")
	}
	c.handlers.reactions = append(c.handlers.reactions, handler)
	return nil
}

// OnThread registers a handler for thread create, update, and delete events.
func (c *Client) OnThread(handler ThreadHandler) error {
	if handler == nil {
		return errors.New("discord thread handler is required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return errors.New("discord handlers cannot be registered after Run starts")
	}
	c.handlers.threads = append(c.handlers.threads, handler)
	return nil
}

func (c *Client) dispatch(ctx context.Context, event gatewayPayload) {
	c.dispatchRaw(ctx, event)

	switch event.Type {
	case EventMessageCreate:
		var message Message
		if err := json.Unmarshal(event.Data, &message); err != nil {
			c.report(fmt.Errorf("decode Discord message event: %w", err))
			return
		}
		if message.Author.Bot || message.WebhookID != "" || !c.allowed(ctx, Actor{UserID: message.Author.ID, GuildID: message.GuildID, ChannelID: message.ChannelID}) {
			return
		}
		typed := &MessageEvent{Message: message, Raw: event.Data, client: c}
		for _, handler := range c.handlers.messages {
			c.invoke(func() error { return handler(ctx, typed) })
		}
	case EventReactionAdd, EventReactionRemove:
		var reaction struct {
			UserID    string `json:"user_id"`
			ChannelID string `json:"channel_id"`
			MessageID string `json:"message_id"`
			GuildID   string `json:"guild_id"`
			Emoji     Emoji  `json:"emoji"`
			Member    struct {
				User User `json:"user"`
			} `json:"member"`
		}
		if err := json.Unmarshal(event.Data, &reaction); err != nil {
			c.report(fmt.Errorf("decode Discord reaction event: %w", err))
			return
		}
		c.mu.Lock()
		userID := c.userID
		c.mu.Unlock()
		if reaction.UserID == userID || reaction.Member.User.Bot || !c.allowed(ctx, Actor{UserID: reaction.UserID, GuildID: reaction.GuildID, ChannelID: reaction.ChannelID}) {
			return
		}
		typed := &ReactionEvent{Type: event.Type, UserID: reaction.UserID, ChannelID: reaction.ChannelID, MessageID: reaction.MessageID, GuildID: reaction.GuildID, Emoji: reaction.Emoji, Raw: event.Data}
		for _, handler := range c.handlers.reactions {
			c.invoke(func() error { return handler(ctx, typed) })
		}
	case EventThreadCreate, EventThreadUpdate, EventThreadDelete:
		var thread Channel
		if err := json.Unmarshal(event.Data, &thread); err != nil {
			c.report(fmt.Errorf("decode Discord thread event: %w", err))
			return
		}
		typed := &ThreadEvent{Type: event.Type, Thread: thread, Raw: event.Data}
		for _, handler := range c.handlers.threads {
			c.invoke(func() error { return handler(ctx, typed) })
		}
	}
}

func (c *Client) dispatchRaw(ctx context.Context, event gatewayPayload) {
	for _, handler := range c.handlers.raw[event.Type] {
		c.invoke(func() error { return handler(ctx, event.Data) })
	}
}

func (c *Client) invoke(handler func() error) {
	c.report(c.call(handler))
}

func (c *Client) call(handler func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic in Discord handler: %v", recovered)
		}
	}()
	return handler()
}
