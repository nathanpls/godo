package discord

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	godohttp "github.com/nathanpls/godo/core/http"
)

const (
	opDispatch       = 0
	opHeartbeat      = 1
	opIdentify       = 2
	opResume         = 6
	opReconnect      = 7
	opInvalidSession = 9
	opHello          = 10
	opHeartbeatACK   = 11

	dispatchQueueSize  = 128
	dispatchWorkers    = 1
	gatewayWriteLimit  = 110
	gatewayWriteWindow = time.Minute
)

type gatewayPayload struct {
	Op       int             `json:"op"`
	Data     json.RawMessage `json:"d"`
	Sequence *int64          `json:"s"`
	Type     EventType       `json:"t"`
}

type gatewayInfo struct {
	URL               string             `json:"url"`
	Shards            int                `json:"shards"`
	SessionStartLimit *sessionStartLimit `json:"session_start_limit"`
}

type sessionStartLimit struct {
	Remaining  int `json:"remaining"`
	ResetAfter int `json:"reset_after"`
	resetAt    time.Time
}

type gatewaySession struct {
	id          string
	resumeURL   string
	sequence    atomic.Int64
	hasSeq      atomic.Bool
	established bool
}

type gatewayResult struct {
	resume     bool
	fatal      error
	immediate  bool
	identified bool
	delay      time.Duration
}

type gatewayWriter struct {
	conn   *godohttp.Conn
	mu     sync.Mutex
	window time.Time
	count  int
}

func (w *gatewayWriter) write(ctx context.Context, value any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now()
	if w.window.IsZero() || now.Sub(w.window) >= gatewayWriteWindow {
		w.window = now
		w.count = 0
	}
	if w.count >= gatewayWriteLimit {
		if err := waitContext(ctx, time.Until(w.window.Add(gatewayWriteWindow))); err != nil {
			return err
		}
		w.window = time.Now()
		w.count = 0
	}
	if err := w.conn.WriteJSON(value); err != nil {
		return err
	}
	w.count++
	return nil
}

// Run connects to Discord's Gateway and handles events until ctx is canceled
// or Discord reports a fatal authentication, sharding, or intents error.
func (c *Client) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("discord Run requires a context")
	}
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return errors.New("discord client can only run once")
	}
	c.running = true
	c.started = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.running = false
		c.mu.Unlock()
	}()

	var info gatewayInfo
	if err := c.request(ctx, http.MethodGet, "/gateway/bot", "GET /gateway/bot", nil, &info); err != nil {
		return fmt.Errorf("get Discord Gateway: %w", err)
	}
	if info.URL == "" {
		return errors.New("discord returned an empty Gateway URL")
	}
	if info.Shards > 1 {
		return fmt.Errorf("discord recommends %d Gateway shards; godo/channels/discord supports one shard", info.Shards)
	}
	if info.SessionStartLimit == nil {
		return errors.New("discord returned no Gateway session start limit")
	}
	info.SessionStartLimit.resetAt = time.Now().Add(time.Duration(info.SessionStartLimit.ResetAfter) * time.Millisecond)

	dispatchCtx, stopDispatch := context.WithCancel(ctx)
	defer stopDispatch()
	queue := make(chan gatewayPayload, dispatchQueueSize)
	for range dispatchWorkers {
		go func() {
			for {
				select {
				case <-dispatchCtx.Done():
					return
				case event := <-queue:
					c.dispatch(dispatchCtx, event)
				}
			}
		}()
	}

	session := &gatewaySession{}
	resume := false
	backoff := time.Second
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		target := info.URL
		if resume && session.resumeURL != "" {
			target = session.resumeURL
		}
		if !resume {
			if !info.SessionStartLimit.resetAt.IsZero() && !time.Now().Before(info.SessionStartLimit.resetAt) {
				if err := c.request(ctx, http.MethodGet, "/gateway/bot", "GET /gateway/bot", nil, &info); err != nil {
					return fmt.Errorf("refresh Discord Gateway: %w", err)
				}
				if info.SessionStartLimit == nil {
					return errors.New("discord returned no Gateway session start limit")
				}
				info.SessionStartLimit.resetAt = time.Now().Add(time.Duration(info.SessionStartLimit.ResetAfter) * time.Millisecond)
			}
			if info.SessionStartLimit.Remaining <= 0 {
				return fmt.Errorf("discord Gateway identify limit is exhausted; retry after %s", time.Duration(info.SessionStartLimit.ResetAfter)*time.Millisecond)
			}
		}
		result := c.runGatewaySession(ctx, gatewayURL(target), session, resume, queue)
		if result.identified {
			info.SessionStartLimit.Remaining--
		}
		if result.fatal != nil {
			return result.fatal
		}
		resume = result.resume && session.id != "" && session.hasSeq.Load()
		if !resume {
			session.id = ""
			session.resumeURL = ""
			session.hasSeq.Store(false)
		}
		if session.established {
			backoff = time.Second
		}
		if result.immediate {
			continue
		}
		if result.delay > 0 {
			if err := waitContext(ctx, result.delay); err != nil {
				return err
			}
			continue
		}
		if err := waitContext(ctx, jitter(backoff)); err != nil {
			return err
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (c *Client) runGatewaySession(ctx context.Context, target string, session *gatewaySession, resume bool, queue chan<- gatewayPayload) gatewayResult {
	session.established = false
	conn, err := godohttp.DialWebSocket(ctx, target, nil)
	if err != nil {
		if ctx.Err() != nil {
			return gatewayResult{fatal: ctx.Err()}
		}
		c.report(err)
		return gatewayResult{resume: resume}
	}
	stopContext := context.AfterFunc(ctx, func() { _ = conn.Abort() })
	defer stopContext()

	var hello gatewayPayload
	if err := conn.ReadJSON(&hello); err != nil {
		_ = conn.Abort()
		return c.gatewayReadResult(ctx, err, resume)
	}
	if hello.Op != opHello {
		if hello.Op == opReconnect {
			_ = conn.Abort()
			return gatewayResult{resume: true, immediate: true}
		}
		_ = conn.Close()
		return gatewayResult{fatal: errors.New("discord Gateway did not begin with Hello")}
	}
	var helloData struct {
		HeartbeatInterval float64 `json:"heartbeat_interval"`
	}
	if err := json.Unmarshal(hello.Data, &helloData); err != nil || helloData.HeartbeatInterval <= 0 {
		_ = conn.Close()
		return gatewayResult{fatal: errors.New("discord Gateway returned an invalid heartbeat interval")}
	}
	interval := time.Duration(helloData.HeartbeatInterval * float64(time.Millisecond))
	writer := &gatewayWriter{conn: conn}

	sessionCtx, stopSession := context.WithCancel(ctx)
	defer stopSession()
	heartbeatErr := make(chan error, 1)
	acked := &atomic.Bool{}
	acked.Store(true)
	go c.heartbeat(sessionCtx, conn, writer, interval, session, acked, heartbeatErr)
	if resume {
		err = writer.write(ctx, map[string]any{"op": opResume, "d": map[string]any{"token": c.token, "session_id": session.id, "seq": session.sequence.Load()}})
	} else {
		err = writer.write(ctx, map[string]any{"op": opIdentify, "d": map[string]any{
			"token":      c.token,
			"intents":    uint64(c.intents),
			"properties": map[string]string{"os": runtime.GOOS, "browser": "godo", "device": "godo"},
		}})
	}
	if err != nil {
		_ = conn.Abort()
		return gatewayResult{resume: resume}
	}
	identified := !resume

	for {
		select {
		case err := <-heartbeatErr:
			_ = conn.Abort()
			c.report(err)
			return gatewayResult{resume: session.id != "", identified: identified}
		default:
		}
		var payload gatewayPayload
		if err := conn.ReadJSON(&payload); err != nil {
			_ = conn.Abort()
			select {
			case heartbeatFailure := <-heartbeatErr:
				c.report(heartbeatFailure)
				return gatewayResult{resume: session.id != "", identified: identified}
			default:
			}
			result := c.gatewayReadResult(ctx, err, session.id != "")
			result.identified = identified
			return result
		}
		switch payload.Op {
		case opDispatch:
			received := time.Now()
			if payload.Type == EventReady {
				if err := c.acceptReady(payload.Data, session); err != nil {
					_ = conn.Close()
					return gatewayResult{fatal: err, identified: identified}
				}
			}
			if payload.Type == EventReady || payload.Type == EventResumed {
				session.established = true
			}
			if payload.Type == EventInteractionCreate {
				claimed := c.claimInteraction(payload.Data)
				if claimed {
					go c.dispatchInteractionAt(ctx, payload.Data, received)
				}
				select {
				case queue <- payload:
					if payload.Sequence != nil {
						session.sequence.Store(*payload.Sequence)
						session.hasSeq.Store(true)
					}
				default:
					_ = conn.Abort()
					return gatewayResult{resume: session.id != "", immediate: true, identified: identified}
				}
				continue
			}
			select {
			case queue <- payload:
				if payload.Sequence != nil {
					session.sequence.Store(*payload.Sequence)
					session.hasSeq.Store(true)
				}
			default:
				_ = conn.Abort()
				return gatewayResult{resume: session.id != "", identified: identified}
			}
		case opHeartbeat:
			acked.Store(false)
			if err := c.writeHeartbeat(ctx, writer, session); err != nil {
				_ = conn.Abort()
				return gatewayResult{resume: session.id != "", identified: identified}
			}
		case opHeartbeatACK:
			acked.Store(true)
		case opReconnect:
			_ = conn.Abort()
			return gatewayResult{resume: true, immediate: true, identified: identified}
		case opInvalidSession:
			var canResume bool
			_ = json.Unmarshal(payload.Data, &canResume)
			_ = conn.Abort()
			return gatewayResult{resume: canResume, identified: identified, delay: time.Second + jitter(4*time.Second)}
		}
	}
}

func (c *Client) heartbeat(ctx context.Context, conn *godohttp.Conn, writer *gatewayWriter, interval time.Duration, session *gatewaySession, acked *atomic.Bool, result chan<- error) {
	timer := time.NewTimer(jitter(interval))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if !acked.Swap(false) {
				err := errors.New("discord Gateway heartbeat was not acknowledged")
				_ = conn.Abort()
				result <- err
				return
			}
			if err := c.writeHeartbeat(ctx, writer, session); err != nil {
				result <- err
				return
			}
			timer.Reset(interval)
		}
	}
}

func (c *Client) writeHeartbeat(ctx context.Context, writer *gatewayWriter, session *gatewaySession) error {
	var sequence any
	if session.hasSeq.Load() {
		sequence = session.sequence.Load()
	}
	return writer.write(ctx, map[string]any{"op": opHeartbeat, "d": sequence})
}

func (c *Client) acceptReady(data json.RawMessage, session *gatewaySession) error {
	var ready struct {
		SessionID        string `json:"session_id"`
		ResumeGatewayURL string `json:"resume_gateway_url"`
		Application      struct {
			ID string `json:"id"`
		} `json:"application"`
		User User `json:"user"`
	}
	if err := json.Unmarshal(data, &ready); err != nil || ready.SessionID == "" || ready.ResumeGatewayURL == "" || ready.Application.ID == "" || ready.User.ID == "" {
		return errors.New("discord Gateway returned an invalid Ready event")
	}
	session.id = ready.SessionID
	session.resumeURL = ready.ResumeGatewayURL
	c.mu.Lock()
	c.applicationID = ready.Application.ID
	c.userID = ready.User.ID
	c.mu.Unlock()
	return nil
}

func (c *Client) gatewayReadResult(ctx context.Context, err error, resumable bool) gatewayResult {
	if ctx.Err() != nil {
		return gatewayResult{fatal: ctx.Err()}
	}
	var closed *godohttp.CloseError
	if !errors.As(err, &closed) {
		c.report(err)
		return gatewayResult{resume: resumable}
	}
	switch closed.Code {
	case 4004, 4010, 4011, 4012, 4013, 4014:
		return gatewayResult{fatal: fmt.Errorf("discord Gateway closed with fatal code %d: %s", closed.Code, closed.Reason)}
	case 4003, 4005, 4007, 4009:
		return gatewayResult{resume: false}
	default:
		return gatewayResult{resume: resumable}
	}
}

func gatewayURL(target string) string {
	parsed, err := url.Parse(target)
	if err != nil {
		return target
	}
	query := parsed.Query()
	query.Set("v", "10")
	query.Set("encoding", "json")
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func jitter(maximum time.Duration) time.Duration {
	if maximum <= 0 {
		return 0
	}
	var buffer [8]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return maximum / 2
	}
	return time.Duration(binary.BigEndian.Uint64(buffer[:]) % uint64(maximum))
}

func waitContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
