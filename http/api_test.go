package http

import (
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestWriteProblem(t *testing.T) {
	response := httptest.NewRecorder()
	err := WriteProblem(response, Problem{
		Type:      "https://example.com/problems/invalid-input",
		Title:     "Invalid input",
		Status:    stdhttp.StatusBadRequest,
		Detail:    "name is required",
		RequestID: "req_123",
		Extensions: map[string]any{
			"errors": []string{"name"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Code != stdhttp.StatusBadRequest {
		t.Fatalf("status = %d", response.Code)
	}
	if got := response.Header().Get("Content-Type"); got != "application/problem+json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	var problem map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}
	if problem["request_id"] != "req_123" || problem["detail"] != "name is required" {
		t.Fatalf("problem = %+v", problem)
	}
}

func TestWriteProblemDefaultsAndValidation(t *testing.T) {
	response := httptest.NewRecorder()
	if err := WriteProblem(response, Problem{Status: stdhttp.StatusNotFound}); err != nil {
		t.Fatal(err)
	}
	var problem map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}
	if problem["type"] != "about:blank" || problem["title"] != "Not Found" {
		t.Fatalf("problem = %+v", problem)
	}
	if err := WriteProblem(httptest.NewRecorder(), Problem{Status: 200}); err == nil {
		t.Fatal("successful problem status was accepted")
	}
	if err := WriteProblem(httptest.NewRecorder(), Problem{Status: 400, Extensions: map[string]any{"title": "override"}}); err == nil {
		t.Fatal("reserved extension was accepted")
	}
}

func TestCursorRoundTrip(t *testing.T) {
	type cursor struct {
		ID   int64  `json:"id"`
		Sort string `json:"sort"`
	}
	want := cursor{ID: 42, Sort: "created_at"}
	encoded, err := EncodeCursor(want)
	if err != nil {
		t.Fatal(err)
	}
	var got cursor
	if err := DecodeCursor(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cursor = %+v, want %+v", got, want)
	}
	if err := DecodeCursor("invalid!", &got); err == nil {
		t.Fatal("invalid cursor was accepted")
	}
}

func TestParsePagination(t *testing.T) {
	request := httptest.NewRequest(stdhttp.MethodGet, "/items?limit=25&cursor=next", nil)
	pagination, err := ParsePagination(request, 20, 100)
	if err != nil {
		t.Fatal(err)
	}
	if pagination.Limit != 25 || pagination.Cursor != "next" {
		t.Fatalf("pagination = %+v", pagination)
	}
	for _, target := range []string{"/items?limit=0", "/items?limit=101", "/items?limit=1&limit=2", "/items?cursor="} {
		if _, err := ParsePagination(httptest.NewRequest(stdhttp.MethodGet, target, nil), 20, 100); err == nil {
			t.Fatalf("invalid pagination %q was accepted", target)
		}
	}
}
