package discord

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultAPIURL       = "https://discord.com/api/v10"
	defaultUserAgent    = "DiscordBot (https://github.com/nathanpls/godo, 1)"
	commandConcurrency  = 64
	recentInteractions  = 4096
	interactionTimeout  = 2800 * time.Millisecond
	accessPolicyTimeout = time.Second
)

// Actor describes the user and location associated with an inbound event.
type Actor struct {
	UserID    string
	GuildID   string
	ChannelID string
}

// AccessPolicy decides whether typed user events may reach handlers. Raw event
// handlers intentionally bypass this policy.
type AccessPolicy interface {
	Allow(context.Context, Actor) bool
}

type accessPolicyFunc func(context.Context, Actor) bool

func (fn accessPolicyFunc) Allow(ctx context.Context, actor Actor) bool {
	return fn(ctx, actor)
}

// AllowUsers returns an immutable policy that allows only the listed Discord
// user IDs. Calling AllowUsers with no IDs allows nobody.
func AllowUsers(ids ...string) AccessPolicy {
	allowed := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		allowed[id] = struct{}{}
	}
	return accessPolicyFunc(func(_ context.Context, actor Actor) bool {
		_, ok := allowed[actor.UserID]
		return ok
	})
}

// Config configures a Discord bot client.
type Config struct {
	Token          string
	Intents        Intents
	Access         AccessPolicy
	HTTPClient     *http.Client
	AutoDeferAfter time.Duration
	OnError        func(error)
}

// Client is a Discord REST and single-shard Gateway client. Register handlers
// before calling Run.
type Client struct {
	token             string
	intents           Intents
	access            AccessPolicy
	httpClient        *http.Client
	apiURL            string
	userAgent         string
	autoDeferAfter    time.Duration
	onError           func(error)
	limits            *rateLimiter
	interactionLimits *rateLimiter

	mu               sync.Mutex
	running          bool
	started          bool
	applicationID    string
	userID           string
	handlers         handlers
	commands         map[string]registeredCommand
	commandSlots     chan struct{}
	interactions     map[string]struct{}
	interactionOrder []string
}

// New creates a Discord client. It does not make a network request.
func New(config Config) (*Client, error) {
	if config.Token == "" || strings.TrimSpace(config.Token) != config.Token || strings.ContainsAny(config.Token, "\r\n") {
		return nil, errors.New("discord token must be non-empty and contain no surrounding whitespace")
	}
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	autoDefer := config.AutoDeferAfter
	if autoDefer == 0 {
		autoDefer = 2 * time.Second
	}
	if autoDefer < 0 || autoDefer > 2*time.Second {
		return nil, errors.New("discord AutoDeferAfter must be between zero and two seconds")
	}
	return &Client{
		token:             config.Token,
		intents:           config.Intents,
		access:            config.Access,
		httpClient:        httpClient,
		apiURL:            defaultAPIURL,
		userAgent:         defaultUserAgent,
		autoDeferAfter:    autoDefer,
		onError:           config.OnError,
		limits:            newRateLimiter(),
		interactionLimits: newRateLimiter(),
		commands:          make(map[string]registeredCommand),
		commandSlots:      make(chan struct{}, commandConcurrency),
		interactions:      make(map[string]struct{}),
	}, nil
}

func (c *Client) claimInteraction(raw json.RawMessage) bool {
	var interaction struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(raw, &interaction) != nil || interaction.ID == "" {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.interactions[interaction.ID]; exists {
		return false
	}
	if len(c.interactionOrder) == recentInteractions {
		delete(c.interactions, c.interactionOrder[0])
		copy(c.interactionOrder, c.interactionOrder[1:])
		c.interactionOrder[len(c.interactionOrder)-1] = interaction.ID
	} else {
		c.interactionOrder = append(c.interactionOrder, interaction.ID)
	}
	c.interactions[interaction.ID] = struct{}{}
	return true
}

func (c *Client) allowed(ctx context.Context, actor Actor) bool {
	if c.access == nil {
		return true
	}
	allowed := false
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				c.report(errors.New("panic in Discord access policy"))
			}
		}()
		allowed = c.access.Allow(ctx, actor)
	}()
	return allowed
}

func (c *Client) allowedBefore(ctx context.Context, actor Actor, deadline time.Time) bool {
	if c.access == nil {
		return true
	}
	policyDeadline := time.Now().Add(accessPolicyTimeout)
	if deadline.Before(policyDeadline) {
		policyDeadline = deadline
	}
	policyCtx, cancel := context.WithDeadline(ctx, policyDeadline)
	defer cancel()
	result := make(chan bool, 1)
	go func() { result <- c.allowed(policyCtx, actor) }()
	select {
	case allowed := <-result:
		return allowed
	case <-policyCtx.Done():
		c.report(errors.New("discord access policy exceeded the interaction response deadline"))
		return false
	}
}

func (c *Client) report(err error) {
	if err != nil && c.onError != nil {
		func() {
			defer func() { _ = recover() }()
			c.onError(err)
		}()
	}
}
