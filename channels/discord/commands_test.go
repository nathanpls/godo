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
)

func TestCommandValidatesDefinition(t *testing.T) {
	client, err := New(Config{Token: "token"})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Command(Command{Name: "Ask", Description: "Ask a question"}, func(context.Context, *CommandEvent) error { return nil }); err == nil {
		t.Fatal("uppercase command name was accepted")
	}
	if err := client.Command(Command{
		Name: "ask", Description: "Ask a question",
		Options: []CommandOption{
			StringOption("optional", "Optional"),
			StringOption("required", "Required", Required),
		},
	}, func(context.Context, *CommandEvent) error { return nil }); err == nil {
		t.Fatal("required option after optional option was accepted")
	}
}

func TestSyncGuildCommandsSkipsUnchangedDefinitions(t *testing.T) {
	var puts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/oauth2/applications/@me":
			fmt.Fprint(w, `{"id":"100"}`)
		case "/applications/100/guilds/200/commands":
			if request.Method == http.MethodPut {
				puts.Add(1)
			}
			fmt.Fprint(w, `[{"id":"300","application_id":"100","guild_id":"200","version":"400","type":1,"name":"ask","description":"Ask a question","dm_permission":true}]`)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	client := testClient(t, server)
	if err := client.Command(Command{Name: "ask", Description: "Ask a question"}, func(context.Context, *CommandEvent) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := client.SyncGuildCommands(context.Background(), "200"); err != nil {
		t.Fatal(err)
	}
	if puts.Load() != 0 {
		t.Fatalf("unchanged definitions caused %d PUT requests", puts.Load())
	}
}

func TestDeniedInteractionGetsEphemeralResponse(t *testing.T) {
	var response struct {
		Type int `json:"type"`
		Data struct {
			Content string `json:"content"`
			Flags   int    `json:"flags"`
		} `json:"data"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "" {
			t.Fatal("interaction response included bot authorization")
		}
		if err := json.NewDecoder(request.Body).Decode(&response); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := testClient(t, server)
	client.access = AllowUsers("123")
	client.dispatchInteraction(context.Background(), json.RawMessage(`{
		"id":"1","application_id":"2","type":2,"token":"interaction-token","guild_id":"3","channel_id":"4",
		"member":{"user":{"id":"999"}},"data":{"name":"ask"}
	}`))
	if response.Type != responseChannelMessage || response.Data.Flags != interactionEphemeralFlag || response.Data.Content == "" {
		t.Fatalf("response = %+v", response)
	}
}

func TestCommandEventOptions(t *testing.T) {
	event := &CommandEvent{options: map[string]interactionOption{
		"text":    {Name: "text", Type: optionString, Value: json.RawMessage(`"hello"`)},
		"integer": {Name: "integer", Type: optionInteger, Value: json.RawMessage(`42`)},
		"boolean": {Name: "boolean", Type: optionBoolean, Value: json.RawMessage(`true`)},
		"user":    {Name: "user", Type: optionUser, Value: json.RawMessage(`"123"`)},
	}}
	if value, err := event.String("text"); err != nil || value != "hello" {
		t.Fatalf("String = %q, %v", value, err)
	}
	if value, err := event.Integer("integer"); err != nil || value != 42 {
		t.Fatalf("Integer = %d, %v", value, err)
	}
	if value, err := event.Boolean("boolean"); err != nil || !value {
		t.Fatalf("Boolean = %t, %v", value, err)
	}
	if value, err := event.UserID("user"); err != nil || value != "123" {
		t.Fatalf("UserID = %q, %v", value, err)
	}
}

func TestEphemeralDeferredReply(t *testing.T) {
	requests := make(chan map[string]any, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		body["method"] = request.Method
		requests <- body
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := testClient(t, server)
	event := &CommandEvent{client: client, interactionID: "1", applicationID: "2", token: "secret-interaction-token", ephemeral: true}
	if err := event.deferResponse(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := event.Reply(context.Background(), InteractionMessage{Content: "private", Ephemeral: true}); err != nil {
		t.Fatal(err)
	}

	deferred := <-requests
	if deferred["type"] != float64(responseDeferredMessage) {
		t.Fatalf("deferred response = %#v", deferred)
	}
	data := deferred["data"].(map[string]any)
	if data["flags"] != float64(interactionEphemeralFlag) {
		t.Fatalf("deferred data = %#v", data)
	}
	edited := <-requests
	if edited["method"] != http.MethodPatch || edited["flags"] != nil {
		t.Fatalf("edited response = %#v", edited)
	}
}

func TestDeferredReplyRejectsVisibilityChange(t *testing.T) {
	client, err := New(Config{Token: "token"})
	if err != nil {
		t.Fatal(err)
	}
	event := &CommandEvent{client: client, response: interactionDeferred, ephemeral: true}
	if err := event.Reply(context.Background(), InteractionMessage{Content: "public"}); err == nil {
		t.Fatal("deferred reply accepted a visibility change")
	}
}

func TestInteractionIgnoresBotGlobalRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := testClient(t, server)
	client.limits.global = time.Now().Add(time.Minute)
	event := &CommandEvent{client: client, interactionID: "1", applicationID: "2", token: "interaction-token"}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := event.Reply(ctx, InteractionMessage{Content: "ready"}); err != nil {
		t.Fatal(err)
	}
}

func TestCommandHandlerErrorGetsFallbackResponse(t *testing.T) {
	response := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		response <- body
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := testClient(t, server)
	if err := client.Command(Command{Name: "fail", Description: "Fail"}, func(context.Context, *CommandEvent) error {
		return errors.New("failed")
	}); err != nil {
		t.Fatal(err)
	}
	client.dispatchInteraction(context.Background(), json.RawMessage(`{
		"id":"1","application_id":"2","type":2,"token":"interaction-token","guild_id":"3","channel_id":"4",
		"member":{"user":{"id":"5"}},"data":{"name":"fail"}
	}`))
	select {
	case body := <-response:
		if body["type"] != float64(responseChannelMessage) {
			t.Fatalf("response = %#v", body)
		}
	case <-time.After(time.Second):
		t.Fatal("handler error did not receive a fallback response")
	}
}

func TestSlowAccessPolicyGetsDeniedBeforeDeadline(t *testing.T) {
	response := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		response <- struct{}{}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := testClient(t, server)
	client.access = accessPolicyFunc(func(ctx context.Context, actor Actor) bool {
		<-ctx.Done()
		return false
	})
	started := time.Now()
	client.dispatchInteractionAt(context.Background(), json.RawMessage(`{
		"id":"1","application_id":"2","type":2,"token":"interaction-token","guild_id":"3","channel_id":"4",
		"member":{"user":{"id":"5"}},"data":{"name":"missing"}
	}`), started)
	if time.Since(started) > 1500*time.Millisecond {
		t.Fatal("access policy exceeded its bounded evaluation time")
	}
	select {
	case <-response:
	default:
		t.Fatal("slow access policy did not receive a denial response")
	}
}

func TestInteractionIsClaimedOnce(t *testing.T) {
	client, err := New(Config{Token: "token"})
	if err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(`{"id":"123"}`)
	if !client.claimInteraction(raw) {
		t.Fatal("first interaction claim was rejected")
	}
	if client.claimInteraction(raw) {
		t.Fatal("duplicate interaction claim was accepted")
	}
}
