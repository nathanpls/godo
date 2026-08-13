# Discord

`github.com/nathanpls/godo/channels/discord` is a dependency-light Discord API v10 client
for bots. It combines REST operations with one Gateway connection and provides
typed handlers for messages, reactions, threads, and slash commands.

Version one supports one bot token and one Gateway shard. It deliberately does
not include voice, state caches, sharding, Gateway compression, components,
modals, file uploads, or OAuth user installations.

## Create a bot

Create an application in the [Discord Developer Portal](https://discord.com/developers/applications),
add a bot, and keep its token outside source control. Invite it with the bot and
`applications.commands` scopes plus only the permissions it needs.

Install the package:

```sh
godo add discord
```

Or generate a starter in an empty directory:

```sh
godo init my-bot --module github.com/example/my-bot --template discord
cd my-bot
godo add discord
```

The template reads `DISCORD_BOT_TOKEN`, handles process signals, and replies
`pong` to `ping`. It does not load `.env` files; export the variable through the
shell or service manager.

## Listen for messages

```go
bot, err := discord.New(discord.Config{
    Token: os.Getenv("DISCORD_BOT_TOKEN"),
    Intents: discord.IntentsGuilds |
        discord.IntentsGuildMessages |
        discord.IntentsGuildMessageReactions |
        discord.IntentsMessageContent,
    Access: discord.AllowUsers("182736451827364518"),
    OnError: func(err error) {
        slog.Error("discord", "error", err)
    },
})
if err != nil {
    return err
}

if err := bot.OnMessage(func(ctx context.Context, event *discord.MessageEvent) error {
    if strings.Contains(event.Message.Content, "ship it") {
        return event.Reply(ctx, discord.MessageCreate{Content: "Working on it."})
    }
    return nil
}); err != nil {
    return err
}

return bot.Run(ctx)
```

`Run` fetches Discord's Gateway URL, identifies the bot, maintains heartbeats,
and resumes interrupted sessions when Discord permits it. It stops when the
context is canceled or Discord reports a fatal token, intents, or sharding
error. Discord may duplicate or omit events around failures, so handlers should
be idempotent.

Register all handlers and commands before calling `Run`. A client can run only
once; registrations are frozen after its first run starts.

Message, reaction, thread, and raw handlers run in Gateway order. A blocked
handler applies backpressure; if the bounded queue fills, the client reconnects
and resumes rather than silently dropping accepted events. Slash command
handling uses a separate bounded path so ordinary handlers cannot consume the
three-second interaction response window. Raw interaction handlers remain on
the ordered path.

### Message content

`IntentsMessageContent` is a privileged intent. Enable **Message Content Intent**
on the bot page in the Developer Portal and include it in `Config.Intents` when
handlers need unrestricted message content. Without it, Discord generally omits
message content outside DMs, mentions, and documented exceptions. Prefer slash
commands when message content is unnecessary.

Bot and webhook messages are ignored by typed message handlers to prevent
loops. Typed reaction handlers ignore the connected bot's own reactions and bot
members when Discord includes member data.

## Access policy

`AllowUsers(ids...)` permits typed user events and commands only for listed
Discord user IDs. Calling it with no IDs allows nobody. A nil policy allows
everyone.

Implement `AccessPolicy` for application-specific checks:

```go
type policy struct{}

func (policy) Allow(ctx context.Context, actor discord.Actor) bool {
    return actor.GuildID == allowedGuild
}
```

Denied messages and reactions are ignored. Denied slash commands receive an
ephemeral response. Interaction access checks are bounded so a slow policy
cannot consume Discord's response window. `On(EventType, RawHandler)` intentionally bypasses access
filtering and receives the protocol payload, so raw handlers must apply any
authorization they require.

## Messages and reactions

```go
message, err := bot.Send(ctx, channelID, discord.MessageCreate{
    Content: "Deployment finished.",
})
if err != nil {
    return err
}

if err := bot.React(ctx, channelID, message.ID, "ok-hand"); err != nil {
    return err
}
```

Use `Guild`, `GuildChannels`, `Channel`, and `Messages` to read resources. Use
`Send`, `Reply`, `EditMessage`, `DeleteMessage`, `React`, and `RemoveReaction`
for message mutations. Emoji values may be Unicode or Discord's `name:id` custom
emoji form.

Outbound messages and interaction responses default to
`allowed_mentions: {"parse":[]}`. Content therefore cannot ping users, roles,
or everyone unless `AllowedMentions` explicitly opts in. An explicit empty or
nil `Parse` also serializes as an empty list.

## Threads

Threads are `Channel` values with `ChannelAnnouncementThread`,
`ChannelPublicThread`, or `ChannelPrivateThread` type.

```go
thread, err := bot.StartThreadFromMessage(ctx, channelID, messageID, discord.ThreadFromMessage{
    Name: "deployment follow-up",
    AutoArchiveDuration: 1440,
})
if err != nil {
    return err
}

if err := bot.JoinThread(ctx, thread.ID); err != nil {
    return err
}
```

The package also provides `StartThread`, `EditThread`, and `LeaveThread`.

## Slash commands

Register definitions in the client, then explicitly synchronize them. Guild
commands update immediately and are the recommended development path. Global
command propagation may take time.

```go
err := bot.Command(discord.Command{
    Name: "ask",
    Description: "Ask the bot a question",
    Ephemeral: true,
    Options: []discord.CommandOption{
        discord.StringOption("question", "What to ask", discord.Required),
    },
}, func(ctx context.Context, command *discord.CommandEvent) error {
    question, err := command.String("question")
    if err != nil {
        return err
    }
    return command.Reply(ctx, discord.InteractionMessage{
        Content: answer(question),
        Ephemeral: true,
    })
})
if err != nil {
    return err
}

if err := bot.SyncGuildCommands(ctx, guildID); err != nil {
    return err
}
```

`StringOption`, `IntegerOption`, `BooleanOption`, `UserOption`, and
`ChannelOption` are supported. Required options must precede optional options.
Read values through `CommandEvent.String`, `Integer`, `Boolean`, `UserID`, and
`ChannelIDOption`. Set `Command.Ephemeral` when automatic deferral and the final
reply should be private.

Discord requires an initial response within three seconds. If a command handler
has not replied after two seconds, the package defers it automatically. A later
`Reply` edits that deferred response. Set `Config.AutoDeferAfter` to another
duration up to two seconds. If auto-defer occurs, the eventual reply must match
the command's configured visibility. Handlers that fail or return without a
reply receive a generic fallback response. Use `Followup` after a successful
reply.

Command synchronization first compares normalized existing definitions and
skips an unchanged bulk overwrite. Registration is never performed implicitly
by `Run`.

## REST behavior

The REST client learns per-route and global limits from Discord response headers
and 429 bodies. Rate-limit waits honor the request context. It retries only 429
responses; network failures and 5xx mutations are returned rather than retried,
avoiding accidental duplicate messages.

Discord errors use `APIError`, containing HTTP status, Discord code, and message.
Response and Gateway bodies are bounded. Bot and interaction tokens are never
included in package-generated errors.

All Discord snowflakes remain decimal strings. Public models are intentionally
partial instead of mirroring every Discord field; raw Gateway event data remains
available where forward-compatible protocol access is needed.
