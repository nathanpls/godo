package discord

import (
	"context"
	"encoding/json"
	"testing"
)

func TestDispatchFiltersTypedMessagesButNotRawEvents(t *testing.T) {
	client, err := New(Config{Token: "token", Access: AllowUsers("123")})
	if err != nil {
		t.Fatal(err)
	}
	var raw, typed int
	if err := client.On(EventMessageCreate, func(context.Context, json.RawMessage) error {
		raw++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := client.OnMessage(func(context.Context, *MessageEvent) error {
		typed++
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	client.dispatch(context.Background(), gatewayPayload{Type: EventMessageCreate, Data: json.RawMessage(`{"id":"1","channel_id":"2","author":{"id":"999"}}`)})
	client.dispatch(context.Background(), gatewayPayload{Type: EventMessageCreate, Data: json.RawMessage(`{"id":"1","channel_id":"2","author":{"id":"123","bot":true}}`)})
	client.dispatch(context.Background(), gatewayPayload{Type: EventMessageCreate, Data: json.RawMessage(`{"id":"1","channel_id":"2","author":{"id":"123"}}`)})

	if raw != 3 || typed != 1 {
		t.Fatalf("raw = %d, typed = %d", raw, typed)
	}
}

func TestHandlerPanicIsReported(t *testing.T) {
	var reported bool
	client, err := New(Config{Token: "token", OnError: func(error) { reported = true }})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.OnMessage(func(context.Context, *MessageEvent) error { panic("boom") }); err != nil {
		t.Fatal(err)
	}
	client.dispatch(context.Background(), gatewayPayload{Type: EventMessageCreate, Data: json.RawMessage(`{"id":"1","channel_id":"2","author":{"id":"3"}}`)})
	if !reported {
		t.Fatal("handler panic was not reported")
	}
}
