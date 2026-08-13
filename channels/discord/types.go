package discord

import "time"

// Intents is a bitset of Discord Gateway intents.
type Intents uint64

const (
	IntentsGuilds                  Intents = 1 << 0
	IntentsGuildMembers            Intents = 1 << 1
	IntentsGuildModeration         Intents = 1 << 2
	IntentsGuildExpressions        Intents = 1 << 3
	IntentsGuildIntegrations       Intents = 1 << 4
	IntentsGuildWebhooks           Intents = 1 << 5
	IntentsGuildInvites            Intents = 1 << 6
	IntentsGuildVoiceStates        Intents = 1 << 7
	IntentsGuildPresences          Intents = 1 << 8
	IntentsGuildMessages           Intents = 1 << 9
	IntentsGuildMessageReactions   Intents = 1 << 10
	IntentsGuildMessageTyping      Intents = 1 << 11
	IntentsDirectMessages          Intents = 1 << 12
	IntentsDirectMessageReactions  Intents = 1 << 13
	IntentsDirectMessageTyping     Intents = 1 << 14
	IntentsMessageContent          Intents = 1 << 15
	IntentsGuildScheduledEvents    Intents = 1 << 16
	IntentsAutoModerationConfig    Intents = 1 << 20
	IntentsAutoModerationExecution Intents = 1 << 21
)

// User is a deliberately partial Discord user object.
type User struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	GlobalName string `json:"global_name"`
	Avatar     string `json:"avatar"`
	Bot        bool   `json:"bot"`
}

// Guild is a deliberately partial Discord guild (server) object.
type Guild struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Icon        string `json:"icon"`
	OwnerID     string `json:"owner_id"`
	Description string `json:"description"`
}

// ChannelType identifies a Discord channel or thread type.
type ChannelType int

const (
	ChannelGuildText          ChannelType = 0
	ChannelDM                 ChannelType = 1
	ChannelGuildVoice         ChannelType = 2
	ChannelGroupDM            ChannelType = 3
	ChannelGuildCategory      ChannelType = 4
	ChannelGuildAnnouncement  ChannelType = 5
	ChannelAnnouncementThread ChannelType = 10
	ChannelPublicThread       ChannelType = 11
	ChannelPrivateThread      ChannelType = 12
	ChannelGuildStageVoice    ChannelType = 13
	ChannelGuildDirectory     ChannelType = 14
	ChannelGuildForum         ChannelType = 15
	ChannelGuildMedia         ChannelType = 16
)

// Channel is a deliberately partial Discord channel object. Threads are
// channels with one of the thread ChannelType values.
type Channel struct {
	ID             string          `json:"id"`
	Type           ChannelType     `json:"type"`
	GuildID        string          `json:"guild_id"`
	Name           string          `json:"name"`
	Topic          string          `json:"topic"`
	ParentID       string          `json:"parent_id"`
	LastMessageID  string          `json:"last_message_id"`
	ThreadMetadata *ThreadMetadata `json:"thread_metadata"`
}

// ThreadMetadata contains thread state returned on a Channel.
type ThreadMetadata struct {
	Archived            bool       `json:"archived"`
	AutoArchiveDuration int        `json:"auto_archive_duration"`
	ArchiveTimestamp    time.Time  `json:"archive_timestamp"`
	Locked              bool       `json:"locked"`
	Invitable           bool       `json:"invitable"`
	CreateTimestamp     *time.Time `json:"create_timestamp"`
}

// Attachment is a file attached to a message.
type Attachment struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	Description string `json:"description"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
	URL         string `json:"url"`
	ProxyURL    string `json:"proxy_url"`
	Height      int    `json:"height"`
	Width       int    `json:"width"`
}

// Message is a deliberately partial Discord message object.
type Message struct {
	ID              string            `json:"id"`
	ChannelID       string            `json:"channel_id"`
	GuildID         string            `json:"guild_id"`
	Author          User              `json:"author"`
	Content         string            `json:"content"`
	Timestamp       time.Time         `json:"timestamp"`
	EditedTimestamp *time.Time        `json:"edited_timestamp"`
	TTS             bool              `json:"tts"`
	MentionEveryone bool              `json:"mention_everyone"`
	Mentions        []User            `json:"mentions"`
	Attachments     []Attachment      `json:"attachments"`
	WebhookID       string            `json:"webhook_id"`
	Type            int               `json:"type"`
	Reference       *MessageReference `json:"message_reference"`
}

// MessageReference identifies a message being replied to.
type MessageReference struct {
	MessageID       string `json:"message_id"`
	ChannelID       string `json:"channel_id,omitempty"`
	GuildID         string `json:"guild_id,omitempty"`
	FailIfNotExists *bool  `json:"fail_if_not_exists,omitempty"`
}

// AllowedMentions controls mentions parsed from outbound message content. When
// omitted from MessageCreate or MessageEdit, no mentions are parsed.
type AllowedMentions struct {
	Parse       []string `json:"parse"`
	Roles       []string `json:"roles,omitempty"`
	Users       []string `json:"users,omitempty"`
	RepliedUser bool     `json:"replied_user,omitempty"`
}

// MessageCreate describes a message to send.
type MessageCreate struct {
	Content         string            `json:"content"`
	TTS             bool              `json:"tts,omitempty"`
	AllowedMentions *AllowedMentions  `json:"allowed_mentions,omitempty"`
	Reference       *MessageReference `json:"message_reference,omitempty"`
}

// MessageEdit describes fields to change on a message.
type MessageEdit struct {
	Content         *string          `json:"content,omitempty"`
	AllowedMentions *AllowedMentions `json:"allowed_mentions,omitempty"`
}

// MessagesOptions controls message history retrieval. At most one of Before,
// After, and Around may be set. Limit must be between 1 and 100 when nonzero.
type MessagesOptions struct {
	Before string
	After  string
	Around string
	Limit  int
}

// ThreadCreate describes a new thread without a source message.
type ThreadCreate struct {
	Name                string      `json:"name"`
	AutoArchiveDuration int         `json:"auto_archive_duration,omitempty"`
	Type                ChannelType `json:"type,omitempty"`
	Invitable           *bool       `json:"invitable,omitempty"`
}

// ThreadFromMessage describes a new thread attached to a message.
type ThreadFromMessage struct {
	Name                string `json:"name"`
	AutoArchiveDuration int    `json:"auto_archive_duration,omitempty"`
	RateLimitPerUser    int    `json:"rate_limit_per_user,omitempty"`
}

// ThreadEdit describes editable thread fields.
type ThreadEdit struct {
	Name                *string `json:"name,omitempty"`
	Archived            *bool   `json:"archived,omitempty"`
	AutoArchiveDuration *int    `json:"auto_archive_duration,omitempty"`
	Locked              *bool   `json:"locked,omitempty"`
	Invitable           *bool   `json:"invitable,omitempty"`
	RateLimitPerUser    *int    `json:"rate_limit_per_user,omitempty"`
}
