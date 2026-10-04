package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Rom204/argus/storage"
)

// fakeStore is a hand-written stand-in for the real database (CLAUDE.md §14.2
// prefers a fake over a mock framework). It records the query it was handed so
// a test can assert the parsing actually reached the store.
type fakeStore struct {
	rows     []storage.EventRow
	err      error
	gotQuery storage.EventQuery
}

func (f *fakeStore) RecentEvents(_ context.Context, query storage.EventQuery) ([]storage.EventRow, error) {
	f.gotQuery = query
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

func sampleRows() []storage.EventRow {
	when := time.Date(2026, 10, 4, 15, 2, 11, 482000000, time.UTC)
	return []storage.EventRow{
		{Time: when, Type: "EXECVE", PID: 4821, PPID: 4820, UID: 1000, GID: 1000, Comm: "ls", Caps: 0},
		{Time: when.Add(12 * time.Millisecond), Type: "EXIT", PID: 4821, PPID: 4820, UID: 1000, GID: 1000, Comm: "ls", Caps: 0x2000},
	}
}

func doRequest(t *testing.T, store EventStore, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	NewServer(store, nil).Routes().ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestGetEvents_ReturnsJSONEnvelope(t *testing.T) {
	t.Parallel()
	store := &fakeStore{rows: sampleRows()}

	rec := doRequest(t, store, http.MethodGet, "/api/events")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body struct {
		Count  int `json:"count"`
		Events []struct {
			Time string `json:"time"`
			Type string `json:"type"`
			PID  int32  `json:"pid"`
			PPID int32  `json:"ppid"`
			UID  int64  `json:"uid"`
			GID  int64  `json:"gid"`
			Comm string `json:"comm"`
			Caps string `json:"caps"`
		} `json:"events"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v\n%s", err, rec.Body.String())
	}

	if body.Count != 2 || len(body.Events) != 2 {
		t.Fatalf("count = %d, len(events) = %d, want 2 and 2", body.Count, len(body.Events))
	}
	first := body.Events[0]
	if first.Type != "EXECVE" || first.Comm != "ls" || first.PID != 4821 {
		t.Errorf("first event = %+v, want EXECVE/ls/4821", first)
	}
	if first.Time != "2026-10-04T15:02:11.482Z" {
		t.Errorf("time = %q, want 2026-10-04T15:02:11.482Z", first.Time)
	}
	// Capabilities are a bitmask, so they travel as hex — 8192 tells a reader
	// nothing, 0x2000 says "bit 13".
	if body.Events[1].Caps != "0x2000" {
		t.Errorf("caps = %q, want 0x2000", body.Events[1].Caps)
	}
}

func TestGetEvents_EmptyResultIsAnArrayNotNull(t *testing.T) {
	t.Parallel()
	// The page calls .map() on this array, so `null` would break it.
	store := &fakeStore{rows: []storage.EventRow{}}

	rec := doRequest(t, store, http.MethodGet, "/api/events")

	var body struct {
		Count  int             `json:"count"`
		Events json.RawMessage `json:"events"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Count != 0 {
		t.Errorf("count = %d, want 0", body.Count)
	}
	if string(body.Events) != "[]" {
		t.Errorf("events = %s, want []", body.Events)
	}
}

func TestGetEvents_PassesParsedQueryToTheStore(t *testing.T) {
	t.Parallel()
	store := &fakeStore{rows: sampleRows()}

	doRequest(t, store, http.MethodGet, "/api/events?limit=7&type=exit")

	if store.gotQuery.Limit != 7 {
		t.Errorf("store got Limit = %d, want 7", store.gotQuery.Limit)
	}
	if store.gotQuery.Type != "EXIT" {
		t.Errorf("store got Type = %q, want EXIT", store.gotQuery.Type)
	}
}

func TestGetEvents_BadParameterIsRejected(t *testing.T) {
	t.Parallel()
	store := &fakeStore{rows: sampleRows()}

	rec := doRequest(t, store, http.MethodGet, "/api/events?limit=0")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("error response is not JSON: %v", err)
	}
	if body.Error == "" {
		t.Error("error field is empty, want an explanation of the rule")
	}
	if store.gotQuery.Limit != 0 {
		t.Error("store was queried despite invalid input")
	}
}

func TestGetEvents_StoreFailureIsNotLeakedToTheClient(t *testing.T) {
	t.Parallel()
	secret := "pq: relation \"events\" does not exist in schema argus_internal"
	store := &fakeStore{err: errors.New(secret)}

	rec := doRequest(t, store, http.MethodGet, "/api/events")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if got := rec.Body.String(); got == "" {
		t.Error("empty body, want a generic JSON error")
	}
	// The detail belongs in the server log, not in the response.
	if body := rec.Body.String(); strings.Contains(body, "argus_internal") {
		t.Errorf("response leaked internal detail: %s", body)
	}
}

func TestEvents_WrongMethodIsRejected(t *testing.T) {
	t.Parallel()
	store := &fakeStore{rows: sampleRows()}

	rec := doRequest(t, store, http.MethodPost, "/api/events")

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
