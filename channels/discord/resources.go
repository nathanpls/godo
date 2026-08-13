package discord

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Guild returns a guild (server) visible to the bot.
func (c *Client) Guild(ctx context.Context, guildID string) (*Guild, error) {
	if err := validSnowflake("guild ID", guildID); err != nil {
		return nil, err
	}
	var guild Guild
	err := c.request(ctx, http.MethodGet, "/guilds/"+guildID, "GET /guilds/:guild "+guildID, nil, &guild)
	return &guild, err
}

// GuildChannels returns the channels in a guild.
func (c *Client) GuildChannels(ctx context.Context, guildID string) ([]Channel, error) {
	if err := validSnowflake("guild ID", guildID); err != nil {
		return nil, err
	}
	var channels []Channel
	err := c.request(ctx, http.MethodGet, "/guilds/"+guildID+"/channels", "GET /guilds/:guild/channels "+guildID, nil, &channels)
	return channels, err
}

// Channel returns a channel or thread visible to the bot.
func (c *Client) Channel(ctx context.Context, channelID string) (*Channel, error) {
	if err := validSnowflake("channel ID", channelID); err != nil {
		return nil, err
	}
	var channel Channel
	err := c.request(ctx, http.MethodGet, "/channels/"+channelID, "GET /channels/:channel "+channelID, nil, &channel)
	return &channel, err
}

// Messages returns message history for a channel or thread.
func (c *Client) Messages(ctx context.Context, channelID string, options MessagesOptions) ([]Message, error) {
	if err := validSnowflake("channel ID", channelID); err != nil {
		return nil, err
	}
	query := make(url.Values)
	cursors := 0
	for name, value := range map[string]string{"before": options.Before, "after": options.After, "around": options.Around} {
		if value == "" {
			continue
		}
		if err := validSnowflake(name+" message ID", value); err != nil {
			return nil, err
		}
		cursors++
		query.Set(name, value)
	}
	if cursors > 1 {
		return nil, errors.New("discord messages accepts only one of Before, After, or Around")
	}
	if options.Limit < 0 || options.Limit > 100 {
		return nil, errors.New("discord message limit must be between 1 and 100 when set")
	}
	if options.Limit != 0 {
		query.Set("limit", strconv.Itoa(options.Limit))
	}
	path := "/channels/" + channelID + "/messages"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var messages []Message
	err := c.request(ctx, http.MethodGet, path, "GET /channels/:channel/messages "+channelID, nil, &messages)
	return messages, err
}

// Send creates a message in a channel or thread.
func (c *Client) Send(ctx context.Context, channelID string, message MessageCreate) (*Message, error) {
	if err := validSnowflake("channel ID", channelID); err != nil {
		return nil, err
	}
	message = safeMessage(message)
	var created Message
	err := c.request(ctx, http.MethodPost, "/channels/"+channelID+"/messages", "POST /channels/:channel/messages "+channelID, message, &created)
	return &created, err
}

// Reply creates a reply to a message.
func (c *Client) Reply(ctx context.Context, channelID, messageID string, message MessageCreate) (*Message, error) {
	if err := validSnowflake("message ID", messageID); err != nil {
		return nil, err
	}
	fail := false
	message.Reference = &MessageReference{MessageID: messageID, ChannelID: channelID, FailIfNotExists: &fail}
	return c.Send(ctx, channelID, message)
}

// EditMessage edits a message created by the bot.
func (c *Client) EditMessage(ctx context.Context, channelID, messageID string, edit MessageEdit) (*Message, error) {
	if err := validMessageLocation(channelID, messageID); err != nil {
		return nil, err
	}
	if edit.AllowedMentions == nil {
		edit.AllowedMentions = normalizedAllowedMentions(nil)
	} else {
		edit.AllowedMentions = normalizedAllowedMentions(edit.AllowedMentions)
	}
	var message Message
	path := fmt.Sprintf("/channels/%s/messages/%s", channelID, messageID)
	err := c.request(ctx, http.MethodPatch, path, "PATCH /channels/:channel/messages/:message "+channelID, edit, &message)
	return &message, err
}

// DeleteMessage deletes a message.
func (c *Client) DeleteMessage(ctx context.Context, channelID, messageID string) error {
	if err := validMessageLocation(channelID, messageID); err != nil {
		return err
	}
	path := fmt.Sprintf("/channels/%s/messages/%s", channelID, messageID)
	return c.request(ctx, http.MethodDelete, path, "DELETE /channels/:channel/messages/:message "+channelID, nil, nil)
}

// React adds the bot's reaction to a message. Emoji may be Unicode or a
// Discord name:id custom emoji value.
func (c *Client) React(ctx context.Context, channelID, messageID, emoji string) error {
	if err := validMessageLocation(channelID, messageID); err != nil {
		return err
	}
	if emoji == "" || strings.ContainsAny(emoji, "\r\n") {
		return errors.New("discord reaction emoji must be non-empty")
	}
	path := fmt.Sprintf("/channels/%s/messages/%s/reactions/%s/@me", channelID, messageID, url.PathEscape(emoji))
	return c.request(ctx, http.MethodPut, path, "PUT /channels/:channel/messages/:message/reactions/:emoji/@me "+channelID, nil, nil)
}

// RemoveReaction removes the bot's reaction from a message.
func (c *Client) RemoveReaction(ctx context.Context, channelID, messageID, emoji string) error {
	if err := validMessageLocation(channelID, messageID); err != nil {
		return err
	}
	if emoji == "" || strings.ContainsAny(emoji, "\r\n") {
		return errors.New("discord reaction emoji must be non-empty")
	}
	path := fmt.Sprintf("/channels/%s/messages/%s/reactions/%s/@me", channelID, messageID, url.PathEscape(emoji))
	return c.request(ctx, http.MethodDelete, path, "DELETE /channels/:channel/messages/:message/reactions/:emoji/@me "+channelID, nil, nil)
}

// StartThread creates a thread without a source message.
func (c *Client) StartThread(ctx context.Context, channelID string, thread ThreadCreate) (*Channel, error) {
	if err := validSnowflake("channel ID", channelID); err != nil {
		return nil, err
	}
	if err := validThreadName(thread.Name); err != nil {
		return nil, err
	}
	var created Channel
	err := c.request(ctx, http.MethodPost, "/channels/"+channelID+"/threads", "POST /channels/:channel/threads "+channelID, thread, &created)
	return &created, err
}

// StartThreadFromMessage creates a thread attached to a message.
func (c *Client) StartThreadFromMessage(ctx context.Context, channelID, messageID string, thread ThreadFromMessage) (*Channel, error) {
	if err := validMessageLocation(channelID, messageID); err != nil {
		return nil, err
	}
	if err := validThreadName(thread.Name); err != nil {
		return nil, err
	}
	var created Channel
	path := fmt.Sprintf("/channels/%s/messages/%s/threads", channelID, messageID)
	err := c.request(ctx, http.MethodPost, path, "POST /channels/:channel/messages/:message/threads "+channelID, thread, &created)
	return &created, err
}

// EditThread edits a thread channel.
func (c *Client) EditThread(ctx context.Context, threadID string, edit ThreadEdit) (*Channel, error) {
	if err := validSnowflake("thread ID", threadID); err != nil {
		return nil, err
	}
	if edit.Name != nil {
		if err := validThreadName(*edit.Name); err != nil {
			return nil, err
		}
	}
	var thread Channel
	err := c.request(ctx, http.MethodPatch, "/channels/"+threadID, "PATCH /channels/:channel "+threadID, edit, &thread)
	return &thread, err
}

// JoinThread adds the bot to a thread.
func (c *Client) JoinThread(ctx context.Context, threadID string) error {
	if err := validSnowflake("thread ID", threadID); err != nil {
		return err
	}
	return c.request(ctx, http.MethodPut, "/channels/"+threadID+"/thread-members/@me", "PUT /channels/:channel/thread-members/@me "+threadID, nil, nil)
}

// LeaveThread removes the bot from a thread.
func (c *Client) LeaveThread(ctx context.Context, threadID string) error {
	if err := validSnowflake("thread ID", threadID); err != nil {
		return err
	}
	return c.request(ctx, http.MethodDelete, "/channels/"+threadID+"/thread-members/@me", "DELETE /channels/:channel/thread-members/@me "+threadID, nil, nil)
}

func safeMessage(message MessageCreate) MessageCreate {
	message.AllowedMentions = normalizedAllowedMentions(message.AllowedMentions)
	return message
}

func normalizedAllowedMentions(mentions *AllowedMentions) *AllowedMentions {
	if mentions == nil {
		return &AllowedMentions{Parse: []string{}}
	}
	result := *mentions
	if result.Parse == nil {
		result.Parse = []string{}
	}
	return &result
}

func validMessageLocation(channelID, messageID string) error {
	if err := validSnowflake("channel ID", channelID); err != nil {
		return err
	}
	return validSnowflake("message ID", messageID)
}

func validSnowflake(name, value string) error {
	if value == "" {
		return fmt.Errorf("discord %s must be non-empty", name)
	}
	if len(value) > 20 {
		return fmt.Errorf("discord %s is outside the snowflake range", name)
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return fmt.Errorf("discord %s must contain only decimal digits", name)
		}
	}
	if _, err := strconv.ParseUint(value, 10, 64); err != nil {
		return fmt.Errorf("discord %s is outside the snowflake range", name)
	}
	return nil
}

func validThreadName(name string) error {
	length := len([]rune(name))
	if length < 1 || length > 100 {
		return errors.New("discord thread name must contain between 1 and 100 characters")
	}
	return nil
}
