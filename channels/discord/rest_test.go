package discord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSendUsesSafeMentionsAndAuthentication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/channels/123/messages" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bot secret-token" {
			t.Fatalf("Authorization = %q", request.Header.Get("Authorization"))
		}
		if !strings.HasPrefix(request.Header.Get("User-Agent"), "DiscordBot (") {
			t.Fatalf("User-Agent = %q", request.Header.Get("User-Agent"))
		}
		var body struct {
			Content         string          `json:"content"`
			AllowedMentions AllowedMentions `json:"allowed_mentions"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Content != "hello <@123>" || body.AllowedMentions.Parse == nil || len(body.AllowedMentions.Parse) != 0 {
			t.Fatalf("body = %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"456","channel_id":"123","content":"hello <@123>"}`)
	}))
	defer server.Close()

	client := testClient(t, server)
	message, err := client.Send(context.Background(), "123", MessageCreate{Content: "hello <@123>"})
	if err != nil {
		t.Fatal(err)
	}
	if message.ID != "456" {
		t.Fatalf("message = %+v", message)
	}
}

func TestRequestRetriesRateLimit(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"retry_after":0.001,"global":false}`)
			return
		}
		fmt.Fprint(w, `{"id":"123","name":"general","type":0}`)
	}))
	defer server.Close()

	client := testClient(t, server)
	channel, err := client.Channel(context.Background(), "123")
	if err != nil {
		t.Fatal(err)
	}
	if channel.Name != "general" || requests.Load() != 2 {
		t.Fatalf("channel = %+v, requests = %d", channel, requests.Load())
	}
}

func TestRequestRateLimitHonorsContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"retry_after":30,"global":false}`)
	}))
	defer server.Close()

	client := testClient(t, server)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := client.Channel(ctx, "123")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
}

func TestAPIErrorDoesNotExposeToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"code":50013,"message":"Missing Permissions"}`)
	}))
	defer server.Close()

	client := testClient(t, server)
	_, err := client.Channel(context.Background(), "123")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 50013 {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("error exposes token: %v", err)
	}
}

func TestRateBucketAdmissionHonorsContext(t *testing.T) {
	limiter := newRateLimiter()
	bucket, err := limiter.lock(context.Background(), "GET /channels/:channel 123")
	if err != nil {
		t.Fatal(err)
	}
	defer bucket.unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := limiter.lock(ctx, "GET /channels/:channel 123"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
}

func testClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	client, err := New(Config{Token: "secret-token", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	client.apiURL = server.URL
	return client
}
