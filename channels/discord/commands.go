package discord

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"sync"
	"time"
	"unicode"
)

const (
	commandChatInput = 1

	optionString  = 3
	optionInteger = 4
	optionBoolean = 5
	optionUser    = 6
	optionChannel = 7

	interactionPing               = 1
	interactionApplicationCommand = 2

	responsePong             = 1
	responseChannelMessage   = 4
	responseDeferredMessage  = 5
	interactionEphemeralFlag = 1 << 6
)

// OptionFlag configures a command option.
type OptionFlag bool

// Required marks a command option as required.
const Required OptionFlag = true

// Command describes a global or guild slash command.
type Command struct {
	Type                     int             `json:"type,omitempty"`
	Name                     string          `json:"name"`
	Description              string          `json:"description"`
	Options                  []CommandOption `json:"options,omitempty"`
	DefaultMemberPermissions *string         `json:"default_member_permissions,omitempty"`
	DMPermission             *bool           `json:"dm_permission,omitempty"`
	NSFW                     bool            `json:"nsfw,omitempty"`
	// Ephemeral makes automatic deferral private. If auto-defer occurs, Reply
	// must use the same visibility.
	Ephemeral bool `json:"-"`
}

// CommandOption describes a supported slash command option.
type CommandOption struct {
	Type        int    `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required,omitempty"`
}

// StringOption creates a string command option.
func StringOption(name, description string, flags ...OptionFlag) CommandOption {
	return commandOption(optionString, name, description, flags)
}

// IntegerOption creates an integer command option.
func IntegerOption(name, description string, flags ...OptionFlag) CommandOption {
	return commandOption(optionInteger, name, description, flags)
}

// BooleanOption creates a boolean command option.
func BooleanOption(name, description string, flags ...OptionFlag) CommandOption {
	return commandOption(optionBoolean, name, description, flags)
}

// UserOption creates a user command option.
func UserOption(name, description string, flags ...OptionFlag) CommandOption {
	return commandOption(optionUser, name, description, flags)
}

// ChannelOption creates a channel command option.
func ChannelOption(name, description string, flags ...OptionFlag) CommandOption {
	return commandOption(optionChannel, name, description, flags)
}

func commandOption(optionType int, name, description string, flags []OptionFlag) CommandOption {
	option := CommandOption{Type: optionType, Name: name, Description: description}
	for _, flag := range flags {
		option.Required = option.Required || bool(flag)
	}
	return option
}

// CommandHandler handles a slash command interaction.
type CommandHandler func(context.Context, *CommandEvent) error

type registeredCommand struct {
	command Command
	handler CommandHandler
}

// InteractionMessage describes a slash command response.
type InteractionMessage struct {
	Content         string
	Ephemeral       bool
	AllowedMentions *AllowedMentions
}

type interactionOption struct {
	Name  string          `json:"name"`
	Type  int             `json:"type"`
	Value json.RawMessage `json:"value"`
}

// CommandEvent is a slash command invocation.
type CommandEvent struct {
	Name      string
	GuildID   string
	ChannelID string
	User      User
	Raw       json.RawMessage

	client        *Client
	interactionID string
	applicationID string
	token         string
	options       map[string]interactionOption
	mu            sync.Mutex
	response      interactionResponseState
	ephemeral     bool
}

type interactionResponseState byte

const (
	interactionInitial interactionResponseState = iota
	interactionDeferred
	interactionReplied
)

// String returns a required string option.
func (e *CommandEvent) String(name string) (string, error) {
	option, err := e.option(name, optionString)
	if err != nil {
		return "", err
	}
	var value string
	if err := json.Unmarshal(option.Value, &value); err != nil {
		return "", fmt.Errorf("discord command option %q is not a string", name)
	}
	return value, nil
}

// Integer returns a required integer option.
func (e *CommandEvent) Integer(name string) (int64, error) {
	option, err := e.option(name, optionInteger)
	if err != nil {
		return 0, err
	}
	var value int64
	if err := json.Unmarshal(option.Value, &value); err != nil {
		return 0, fmt.Errorf("discord command option %q is not an integer", name)
	}
	return value, nil
}

// Boolean returns a required boolean option.
func (e *CommandEvent) Boolean(name string) (bool, error) {
	option, err := e.option(name, optionBoolean)
	if err != nil {
		return false, err
	}
	var value bool
	if err := json.Unmarshal(option.Value, &value); err != nil {
		return false, fmt.Errorf("discord command option %q is not a boolean", name)
	}
	return value, nil
}

// UserID returns a required user option's Discord ID.
func (e *CommandEvent) UserID(name string) (string, error) {
	return e.snowflakeOption(name, optionUser)
}

// ChannelIDOption returns a required channel option's Discord ID.
func (e *CommandEvent) ChannelIDOption(name string) (string, error) {
	return e.snowflakeOption(name, optionChannel)
}

func (e *CommandEvent) snowflakeOption(name string, optionType int) (string, error) {
	option, err := e.option(name, optionType)
	if err != nil {
		return "", err
	}
	var value string
	if err := json.Unmarshal(option.Value, &value); err != nil || validSnowflake("command option "+name, value) != nil {
		return "", fmt.Errorf("discord command option %q is not a snowflake", name)
	}
	return value, nil
}

func (e *CommandEvent) option(name string, optionType int) (interactionOption, error) {
	option, ok := e.options[name]
	if !ok {
		return interactionOption{}, fmt.Errorf("discord command option %q was not provided", name)
	}
	if option.Type != optionType {
		return interactionOption{}, fmt.Errorf("discord command option %q has an unexpected type", name)
	}
	return option, nil
}

// Reply sends the initial interaction response. If the interaction was
// automatically deferred, Reply edits the original deferred response.
func (e *CommandEvent) Reply(ctx context.Context, message InteractionMessage) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.response == interactionReplied {
		return errors.New("discord interaction has already been replied to")
	}
	data := interactionMessageData(message)
	if e.response == interactionDeferred {
		if message.Ephemeral != e.ephemeral {
			return errors.New("discord reply visibility differs from the deferred response")
		}
		path := fmt.Sprintf("/webhooks/%s/%s/messages/@original", e.applicationID, e.token)
		delete(data, "flags")
		if err := e.client.requestWithoutAuth(ctx, http.MethodPatch, path, interactionRoute("PATCH original", e.applicationID, e.token), data, nil); err != nil {
			return err
		}
		e.response = interactionReplied
		return nil
	}
	path := fmt.Sprintf("/interactions/%s/%s/callback", e.interactionID, e.token)
	response := map[string]any{"type": responseChannelMessage, "data": data}
	if err := e.client.requestWithoutAuth(ctx, http.MethodPost, path, "POST /interactions/:interaction/:token/callback "+e.interactionID, response, nil); err != nil {
		return err
	}
	e.response = interactionReplied
	return nil
}

// Followup sends another message after Reply.
func (e *CommandEvent) Followup(ctx context.Context, message InteractionMessage) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.response != interactionReplied {
		return errors.New("discord interaction requires Reply before Followup")
	}
	path := fmt.Sprintf("/webhooks/%s/%s", e.applicationID, e.token)
	return e.client.requestWithoutAuth(ctx, http.MethodPost, path, interactionRoute("POST followup", e.applicationID, e.token), interactionMessageData(message), nil)
}

func (e *CommandEvent) deferResponse(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.response != interactionInitial {
		return nil
	}
	path := fmt.Sprintf("/interactions/%s/%s/callback", e.interactionID, e.token)
	response := map[string]any{"type": responseDeferredMessage}
	if e.ephemeral {
		response["data"] = map[string]any{"flags": interactionEphemeralFlag}
	}
	if err := e.client.requestWithoutAuth(ctx, http.MethodPost, path, "POST /interactions/:interaction/:token/callback "+e.interactionID, response, nil); err != nil {
		return err
	}
	e.response = interactionDeferred
	return nil
}

func (e *CommandEvent) finish(ctx context.Context, content string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	data := interactionMessageData(InteractionMessage{Content: content, Ephemeral: true})
	switch e.response {
	case interactionReplied:
		return nil
	case interactionDeferred:
		delete(data, "flags")
		path := fmt.Sprintf("/webhooks/%s/%s/messages/@original", e.applicationID, e.token)
		if err := e.client.requestWithoutAuth(ctx, http.MethodPatch, path, interactionRoute("PATCH original", e.applicationID, e.token), data, nil); err != nil {
			return err
		}
	default:
		path := fmt.Sprintf("/interactions/%s/%s/callback", e.interactionID, e.token)
		response := map[string]any{"type": responseChannelMessage, "data": data}
		if err := e.client.requestWithoutAuth(ctx, http.MethodPost, path, "POST /interactions/:interaction/:token/callback "+e.interactionID, response, nil); err != nil {
			return err
		}
	}
	e.response = interactionReplied
	return nil
}

func interactionMessageData(message InteractionMessage) map[string]any {
	mentions := normalizedAllowedMentions(message.AllowedMentions)
	data := map[string]any{"content": message.Content, "allowed_mentions": mentions}
	if message.Ephemeral {
		data["flags"] = interactionEphemeralFlag
	}
	return data
}

func interactionRoute(action, applicationID, token string) string {
	digest := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%s %s:%x", action, applicationID, digest[:8])
}

// Command registers a slash command definition and handler. Registration does
// not update Discord; call SyncGuildCommands or SyncGlobalCommands explicitly.
func (c *Client) Command(command Command, handler CommandHandler) error {
	if handler == nil {
		return errors.New("discord command handler is required")
	}
	command = normalizeCommand(command)
	if err := validateCommand(command); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return errors.New("discord commands cannot be registered after Run starts")
	}
	if _, exists := c.commands[command.Name]; exists {
		return fmt.Errorf("discord command %q is already registered", command.Name)
	}
	c.commands[command.Name] = registeredCommand{command: command, handler: handler}
	return nil
}

// SyncGuildCommands replaces a guild's slash commands when the registered
// definitions differ. Guild command changes are available immediately.
func (c *Client) SyncGuildCommands(ctx context.Context, guildID string) error {
	if err := validSnowflake("guild ID", guildID); err != nil {
		return err
	}
	applicationID, err := c.getApplicationID(ctx)
	if err != nil {
		return err
	}
	return c.syncCommands(ctx, fmt.Sprintf("/applications/%s/guilds/%s/commands", applicationID, guildID), "applications/:application/guilds/:guild/commands "+applicationID+":"+guildID)
}

// SyncGlobalCommands replaces global slash commands when the registered
// definitions differ. Discord may take time to propagate global changes.
func (c *Client) SyncGlobalCommands(ctx context.Context) error {
	applicationID, err := c.getApplicationID(ctx)
	if err != nil {
		return err
	}
	return c.syncCommands(ctx, "/applications/"+applicationID+"/commands", "applications/:application/commands "+applicationID)
}

func (c *Client) syncCommands(ctx context.Context, path, route string) error {
	desired := c.commandDefinitions()
	var current []Command
	if err := c.request(ctx, http.MethodGet, path, "GET /"+route, nil, &current); err != nil {
		return err
	}
	for index := range current {
		current[index] = normalizeCommand(current[index])
	}
	sort.Slice(current, func(i, j int) bool { return current[i].Name < current[j].Name })
	if reflect.DeepEqual(current, desired) {
		return nil
	}
	return c.request(ctx, http.MethodPut, path, "PUT /"+route, desired, nil)
}

func (c *Client) commandDefinitions() []Command {
	c.mu.Lock()
	defer c.mu.Unlock()
	commands := make([]Command, 0, len(c.commands))
	for _, registered := range c.commands {
		command := registered.command
		command.Ephemeral = false
		commands = append(commands, command)
	}
	sort.Slice(commands, func(i, j int) bool { return commands[i].Name < commands[j].Name })
	return commands
}

func (c *Client) getApplicationID(ctx context.Context) (string, error) {
	c.mu.Lock()
	applicationID := c.applicationID
	c.mu.Unlock()
	if applicationID != "" {
		return applicationID, nil
	}
	var application struct {
		ID string `json:"id"`
	}
	if err := c.request(ctx, http.MethodGet, "/oauth2/applications/@me", "GET /oauth2/applications/@me", nil, &application); err != nil {
		return "", err
	}
	if err := validSnowflake("application ID", application.ID); err != nil {
		return "", errors.New("discord returned an invalid application ID")
	}
	c.mu.Lock()
	c.applicationID = application.ID
	c.mu.Unlock()
	return application.ID, nil
}

func (c *Client) dispatchInteraction(ctx context.Context, raw json.RawMessage) {
	if !c.claimInteraction(raw) {
		return
	}
	c.dispatchInteractionAt(ctx, raw, time.Now())
}

func (c *Client) dispatchInteractionAt(ctx context.Context, raw json.RawMessage, received time.Time) {
	deadline := received.Add(interactionTimeout)
	var interaction struct {
		ID            string `json:"id"`
		ApplicationID string `json:"application_id"`
		Type          int    `json:"type"`
		Token         string `json:"token"`
		GuildID       string `json:"guild_id"`
		ChannelID     string `json:"channel_id"`
		Member        struct {
			User User `json:"user"`
		} `json:"member"`
		User User `json:"user"`
		Data struct {
			Name    string              `json:"name"`
			Options []interactionOption `json:"options"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &interaction); err != nil {
		c.report(fmt.Errorf("decode Discord interaction: %w", err))
		return
	}
	if interaction.Type == interactionPing {
		path := fmt.Sprintf("/interactions/%s/%s/callback", interaction.ID, interaction.Token)
		c.report(c.requestWithoutAuth(ctx, http.MethodPost, path, "POST /interactions/:interaction/:token/callback "+interaction.ID, map[string]int{"type": responsePong}, nil))
		return
	}
	if interaction.Type != interactionApplicationCommand {
		return
	}
	user := interaction.Member.User
	if user.ID == "" {
		user = interaction.User
	}
	event := &CommandEvent{
		Name: interaction.Data.Name, GuildID: interaction.GuildID, ChannelID: interaction.ChannelID, User: user, Raw: raw,
		client: c, interactionID: interaction.ID, applicationID: interaction.ApplicationID, token: interaction.Token,
		options: make(map[string]interactionOption, len(interaction.Data.Options)),
	}
	for _, option := range interaction.Data.Options {
		event.options[option.Name] = option
	}
	if !c.allowedBefore(ctx, Actor{UserID: user.ID, GuildID: interaction.GuildID, ChannelID: interaction.ChannelID}, deadline) {
		c.report(event.Reply(ctx, InteractionMessage{Content: "You are not allowed to use this command.", Ephemeral: true}))
		return
	}
	c.mu.Lock()
	registered, exists := c.commands[interaction.Data.Name]
	c.mu.Unlock()
	if !exists {
		c.report(event.Reply(ctx, InteractionMessage{Content: "This command is not available.", Ephemeral: true}))
		return
	}
	event.ephemeral = registered.command.Ephemeral
	select {
	case c.commandSlots <- struct{}{}:
	default:
		c.report(event.Reply(ctx, InteractionMessage{Content: "The bot is busy. Try again shortly.", Ephemeral: true}))
		return
	}
	deferAt := received.Add(c.autoDeferAfter)
	done := make(chan struct{})
	if c.autoDeferAfter > 0 {
		go func() {
			delay := time.Until(deferAt)
			if delay < 0 {
				delay = 0
			}
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
			case <-done:
			case <-timer.C:
				deferCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				c.report(event.deferResponse(deferCtx))
			}
		}()
	}
	go func() {
		defer func() { <-c.commandSlots }()
		handlerErr := c.call(func() error { return registered.handler(ctx, event) })
		if handlerErr != nil {
			c.report(handlerErr)
		}
		close(done)
		message := "The command completed without a response."
		if handlerErr != nil {
			message = "The command could not be completed."
		}
		fallbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		c.report(event.finish(fallbackCtx, message))
	}()
}

func normalizeCommand(command Command) Command {
	if command.Type == 0 {
		command.Type = commandChatInput
	}
	if len(command.Options) == 0 {
		command.Options = nil
	}
	if command.DMPermission != nil && *command.DMPermission {
		command.DMPermission = nil
	}
	return command
}

func validateCommand(command Command) error {
	if command.Type != commandChatInput {
		return errors.New("discord only supports chat input commands")
	}
	if err := validCommandName(command.Name); err != nil {
		return err
	}
	if length := len([]rune(command.Description)); length < 1 || length > 100 {
		return fmt.Errorf("discord command %q description must contain between 1 and 100 characters", command.Name)
	}
	if len(command.Options) > 25 {
		return fmt.Errorf("discord command %q has more than 25 options", command.Name)
	}
	names := make(map[string]struct{}, len(command.Options))
	optionalSeen := false
	for _, option := range command.Options {
		if option.Type < optionString || option.Type > optionChannel {
			return fmt.Errorf("discord command %q has an unsupported option type", command.Name)
		}
		if err := validCommandName(option.Name); err != nil {
			return fmt.Errorf("discord command %q option: %w", command.Name, err)
		}
		if _, exists := names[option.Name]; exists {
			return fmt.Errorf("discord command %q repeats option %q", command.Name, option.Name)
		}
		names[option.Name] = struct{}{}
		if length := len([]rune(option.Description)); length < 1 || length > 100 {
			return fmt.Errorf("discord command %q option %q description must contain between 1 and 100 characters", command.Name, option.Name)
		}
		if !option.Required {
			optionalSeen = true
		} else if optionalSeen {
			return fmt.Errorf("discord command %q required options must precede optional options", command.Name)
		}
	}
	return nil
}

func validCommandName(name string) error {
	characters := []rune(name)
	if len(characters) < 1 || len(characters) > 32 {
		return errors.New("command name must contain between 1 and 32 characters")
	}
	for _, character := range characters {
		if character == '-' || character == '_' || unicode.IsNumber(character) || unicode.IsLetter(character) && !unicode.IsUpper(character) {
			continue
		}
		return errors.New("command name must contain lowercase letters, numbers, hyphens, or underscores")
	}
	return nil
}
