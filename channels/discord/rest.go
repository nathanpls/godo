package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxResponseSize = 4 << 20

// APIError is an error response returned by Discord's REST API.
type APIError struct {
	Status  int
	Code    int
	Message string
}

func (e *APIError) Error() string {
	if e.Code != 0 {
		return fmt.Sprintf("discord API returned status %d, code %d: %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("discord API returned status %d: %s", e.Status, e.Message)
}

type rateLimitResponse struct {
	RetryAfter float64 `json:"retry_after"`
	Global     bool    `json:"global"`
}

func (c *Client) request(ctx context.Context, method, path, route string, input, output any) error {
	return c.requestWithAuth(ctx, method, path, route, input, output, true)
}

func (c *Client) requestWithoutAuth(ctx context.Context, method, path, route string, input, output any) error {
	return c.requestWithAuth(ctx, method, path, route, input, output, false)
}

func (c *Client) requestWithAuth(ctx context.Context, method, path, route string, input, output any, authenticate bool) error {
	var body []byte
	var err error
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil {
			return fmt.Errorf("encode Discord request: %w", err)
		}
	}
	limiter := c.interactionLimits
	if authenticate {
		limiter = c.limits
	}

	for {
		bucket, err := limiter.lock(ctx, route)
		if err != nil {
			return err
		}
		response, payload, err := c.send(ctx, method, path, body, authenticate)
		if err != nil {
			bucket.unlock()
			return err
		}
		limiter.observe(route, bucket, response.Header)
		if response.StatusCode == http.StatusTooManyRequests {
			var limited rateLimitResponse
			if err := json.Unmarshal(payload, &limited); err != nil || limited.RetryAfter < 0 {
				bucket.unlock()
				return errors.New("discord API returned an invalid rate-limit response")
			}
			retry := time.Duration(limited.RetryAfter * float64(time.Second))
			if retry == 0 {
				retry, _ = secondsHeader(response.Header.Get("Retry-After"))
			}
			if retry <= 0 {
				bucket.unlock()
				return errors.New("discord API returned an invalid retry delay")
			}
			limiter.limited(bucket, retry, limited.Global)
			bucket.unlock()
			if err := waitUntil(ctx, time.Now().Add(retry)); err != nil {
				return err
			}
			continue
		}
		bucket.unlock()

		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return decodeAPIError(response.StatusCode, payload)
		}
		if output == nil || len(payload) == 0 {
			return nil
		}
		if err := json.Unmarshal(payload, output); err != nil {
			return fmt.Errorf("decode Discord response: %w", err)
		}
		return nil
	}
}

func (c *Client) send(ctx context.Context, method, path string, body []byte, authenticate bool) (*http.Response, []byte, error) {
	request, err := http.NewRequestWithContext(ctx, method, c.apiURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, nil, errors.New("create Discord request")
	}
	if authenticate {
		request.Header.Set("Authorization", "Bot "+c.token)
	}
	request.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, errors.New("send Discord request")
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxResponseSize+1))
	if err != nil {
		return nil, nil, fmt.Errorf("read Discord response: %w", err)
	}
	if len(payload) > maxResponseSize {
		return nil, nil, errors.New("discord response exceeds size limit")
	}
	return response, payload, nil
}

func decodeAPIError(status int, payload []byte) error {
	response := struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Errors  json.RawMessage `json:"errors"`
	}{}
	_ = json.Unmarshal(payload, &response)
	message := strings.TrimSpace(response.Message)
	if message == "" {
		message = http.StatusText(status)
	}
	return &APIError{Status: status, Code: response.Code, Message: message}
}
