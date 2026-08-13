package http

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	stdhttp "net/http"
	"net/url"
	"strconv"
	"strings"
)

// DecodeError is a client-safe JSON request decoding failure.
type DecodeError struct {
	Status int
	Detail string
	err    error
}

func (decode *DecodeError) Error() string { return decode.Detail }
func (decode *DecodeError) Unwrap() error { return decode.err }

// Problem returns an RFC 9457 response for the decoding failure.
func (decode *DecodeError) Problem() Problem {
	return Problem{Status: decode.Status, Detail: decode.Detail}
}

// DecodeJSON decodes one strict JSON request value. It rejects unsupported
// media types, unknown object fields, trailing values, and bodies above
// maxBytes.
func DecodeJSON(request *stdhttp.Request, destination any, maxBytes int64) error {
	if request == nil || request.Body == nil || destination == nil || maxBytes < 1 || maxBytes == math.MaxInt64 {
		return errors.New("http: invalid JSON decoder configuration")
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" && !strings.HasSuffix(mediaType, "+json") {
		return &DecodeError{Status: stdhttp.StatusUnsupportedMediaType, Detail: "Content-Type must be application/json"}
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, maxBytes+1))
	if err != nil {
		return &DecodeError{Status: stdhttp.StatusBadRequest, Detail: "JSON request body could not be read", err: err}
	}
	if int64(len(body)) > maxBytes {
		return &DecodeError{Status: stdhttp.StatusRequestEntityTooLarge, Detail: "JSON request body is too large"}
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		var invalid *json.InvalidUnmarshalError
		if errors.As(err, &invalid) {
			return fmt.Errorf("http: invalid JSON destination: %w", err)
		}
		return &DecodeError{Status: stdhttp.StatusBadRequest, Detail: "JSON request body is invalid", err: err}
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return &DecodeError{Status: stdhttp.StatusBadRequest, Detail: "JSON request body must contain one value", err: err}
	}
	return nil
}

// Problem is an RFC 9457 problem detail response. Extension values are written
// as additional top-level members.
type Problem struct {
	Type       string         `json:"type,omitempty"`
	Title      string         `json:"title"`
	Status     int            `json:"status"`
	Detail     string         `json:"detail,omitempty"`
	Instance   string         `json:"instance,omitempty"`
	RequestID  string         `json:"request_id,omitempty"`
	Extensions map[string]any `json:"-"`
}

// WriteProblem writes an RFC 9457 application/problem+json response.
func WriteProblem(w stdhttp.ResponseWriter, problem Problem) error {
	if problem.Status < 400 || problem.Status > 599 {
		return fmt.Errorf("http: problem status must be between 400 and 599")
	}
	if problem.Title == "" {
		problem.Title = stdhttp.StatusText(problem.Status)
	}
	if problem.Type == "" {
		problem.Type = "about:blank"
	}
	value := make(map[string]any, len(problem.Extensions)+6)
	value["type"] = problem.Type
	value["title"] = problem.Title
	value["status"] = problem.Status
	if problem.Detail != "" {
		value["detail"] = problem.Detail
	}
	if problem.Instance != "" {
		value["instance"] = problem.Instance
	}
	if problem.RequestID != "" {
		value["request_id"] = problem.RequestID
	}
	for name, extension := range problem.Extensions {
		if reservedProblemMember(name) {
			return fmt.Errorf("http: problem extension %q is reserved", name)
		}
		value[name] = extension
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("http: encode problem: %w", err)
	}
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(problem.Status)
	_, err = w.Write(append(encoded, '\n'))
	return err
}

func reservedProblemMember(name string) bool {
	switch name {
	case "type", "title", "status", "detail", "instance", "request_id":
		return true
	default:
		return false
	}
}

// Page is the standard response envelope for cursor-paginated collections.
type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// Pagination is a validated cursor pagination request.
type Pagination struct {
	Limit  int
	Cursor string
}

// ParsePagination reads limit and cursor query parameters. It rejects duplicate
// values, non-positive limits, and limits above maxLimit.
func ParsePagination(request *stdhttp.Request, defaultLimit, maxLimit int) (Pagination, error) {
	if request == nil || defaultLimit < 1 || maxLimit < defaultLimit {
		return Pagination{}, errors.New("http: invalid pagination configuration")
	}
	query, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil {
		return Pagination{}, errors.New("http: pagination query is invalid")
	}
	limits := query["limit"]
	cursors := query["cursor"]
	if len(limits) > 1 || len(cursors) > 1 {
		return Pagination{}, errors.New("http: pagination parameters must not be repeated")
	}
	result := Pagination{Limit: defaultLimit}
	if len(limits) == 1 {
		limit, err := strconv.Atoi(limits[0])
		if err != nil || limit < 1 || limit > maxLimit {
			return Pagination{}, fmt.Errorf("http: limit must be between 1 and %d", maxLimit)
		}
		result.Limit = limit
	}
	if len(cursors) == 1 {
		if cursors[0] == "" || len(cursors[0]) > 4096 {
			return Pagination{}, errors.New("http: cursor is invalid")
		}
		result.Cursor = cursors[0]
	}
	return result, nil
}

// EncodeCursor serializes value into an opaque URL-safe cursor. Cursors are not
// encrypted or signed and must not contain secrets.
func EncodeCursor(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("http: encode cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

// DecodeCursor decodes an opaque cursor into destination. Destination must be
// a non-nil pointer. Unknown object fields are rejected.
func DecodeCursor(cursor string, destination any) error {
	if cursor == "" {
		return errors.New("http: cursor must not be empty")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return errors.New("http: cursor is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return errors.New("http: cursor is invalid")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("http: cursor is invalid")
	}
	return nil
}
