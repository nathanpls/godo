package discord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	godohttp "github.com/nathanpls/godo/core/http"
)

func TestRunIdentifiesAndResumesGatewaySession(t *testing.T) {
	var gatewayURL string
	var connections atomic.Int32
	resumed := make(chan gatewayPayload, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/gateway/bot", func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"url":%q,"shards":1,"session_start_limit":{"remaining":1000,"reset_after":0}}`, gatewayURL)
	})
	mux.HandleFunc("/gateway", func(w http.ResponseWriter, request *http.Request) {
		conn, err := godohttp.Upgrade(w, request)
		if err != nil {
			return
		}
		defer conn.Close()
		if err := conn.WriteJSON(map[string]any{"op": opHello, "d": map[string]any{"heartbeat_interval": 60000}}); err != nil {
			return
		}
		var opening gatewayPayload
		if err := conn.ReadJSON(&opening); err != nil {
			return
		}
		if connections.Add(1) == 1 {
			if opening.Op != opIdentify || !json.Valid(opening.Data) {
				t.Errorf("opening payload = %+v", opening)
				return
			}
			sequence := int64(1)
			ready := map[string]any{
				"session_id": "session", "resume_gateway_url": gatewayURL,
				"application": map[string]string{"id": "100"}, "user": map[string]string{"id": "200"},
			}
			if err := conn.WriteJSON(gatewayPayload{Op: opDispatch, Data: mustJSON(t, ready), Sequence: &sequence, Type: EventReady}); err != nil {
				return
			}
			_ = conn.WriteJSON(gatewayPayload{Op: opReconnect})
			return
		}
		resumed <- opening
		sequence := int64(2)
		message := map[string]any{"id": "300", "channel_id": "400", "guild_id": "500", "content": "hello", "author": map[string]string{"id": "600"}}
		_ = conn.WriteJSON(gatewayPayload{Op: opDispatch, Data: mustJSON(t, message), Sequence: &sequence, Type: EventMessageCreate})
		<-request.Context().Done()
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	gatewayURL = "ws" + server.URL[len("http"):] + "/gateway"

	client := testClient(t, server)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	received := make(chan struct{})
	if err := client.OnMessage(func(context.Context, *MessageEvent) error {
		close(received)
		cancel()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	err := client.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v", err)
	}
	select {
	case <-received:
	default:
		t.Fatal("message handler was not called")
	}
	opening := <-resumed
	if opening.Op != opResume {
		t.Fatalf("reconnect opcode = %d, want resume", opening.Op)
	}
	var data struct {
		SessionID string `json:"session_id"`
		Sequence  int64  `json:"seq"`
	}
	if err := json.Unmarshal(opening.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.SessionID != "session" || data.Sequence != 1 {
		t.Fatalf("resume data = %+v", data)
	}
}

func TestRunRejectsExhaustedSessionStartLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		fmt.Fprint(w, `{"url":"ws://localhost/gateway","shards":1,"session_start_limit":{"remaining":0,"reset_after":60000}}`)
	}))
	defer server.Close()
	client := testClient(t, server)
	if err := client.Run(context.Background()); err == nil {
		t.Fatal("Run accepted an exhausted session start limit")
	}
}

func TestRunCanOnlyStartOnce(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		fmt.Fprint(w, `{"url":"","shards":1,"session_start_limit":{"remaining":1,"reset_after":0}}`)
	}))
	defer server.Close()
	client := testClient(t, server)
	if err := client.Run(context.Background()); err == nil {
		t.Fatal("first Run unexpectedly succeeded")
	}
	if err := client.Run(context.Background()); err == nil {
		t.Fatal("second Run was accepted")
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}
