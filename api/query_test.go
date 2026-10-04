package api

import (
	"net/url"
	"testing"
)

// TestParseEventQuery pins the validation rules for the two query parameters.
// It needs no database and no HTTP server, which is the point of keeping the
// parsing separate from the handler.
func TestParseEventQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		raw       string
		wantLimit int
		wantType  string
		wantErr   bool
	}{
		{name: "no parameters uses defaults", raw: "", wantLimit: defaultLimit},
		{name: "explicit limit", raw: "limit=50", wantLimit: 50},
		{name: "limit at the maximum", raw: "limit=1000", wantLimit: 1000},
		{name: "limit of one", raw: "limit=1", wantLimit: 1},
		{name: "limit above the maximum is rejected", raw: "limit=1001", wantErr: true},
		{name: "limit of zero is rejected", raw: "limit=0", wantErr: true},
		{name: "negative limit is rejected", raw: "limit=-5", wantErr: true},
		{name: "non-numeric limit is rejected", raw: "limit=abc", wantErr: true},
		{name: "known type is accepted", raw: "type=EXECVE", wantLimit: defaultLimit, wantType: "EXECVE"},
		{name: "type is upper-cased", raw: "type=execve", wantLimit: defaultLimit, wantType: "EXECVE"},
		{name: "every contract type is allowed", raw: "type=CAPS", wantLimit: defaultLimit, wantType: "CAPS"},
		{name: "empty type means all types", raw: "type=", wantLimit: defaultLimit, wantType: ""},
		{name: "unknown type is rejected", raw: "type=BOGUS", wantErr: true},
		// Not sanitised, rejected: the type is matched against an allow-list.
		{name: "sql-looking type is rejected", raw: "type=EXECVE%27%3B+DROP+TABLE+events%3B--", wantErr: true},
		{name: "both parameters together", raw: "limit=7&type=EXIT", wantLimit: 7, wantType: "EXIT"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			values, err := url.ParseQuery(tc.raw)
			if err != nil {
				t.Fatalf("bad test input %q: %v", tc.raw, err)
			}

			got, err := parseEventQuery(values)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseEventQuery(%q) = %+v, want an error", tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseEventQuery(%q): unexpected error: %v", tc.raw, err)
			}
			if got.Limit != tc.wantLimit {
				t.Errorf("Limit = %d, want %d", got.Limit, tc.wantLimit)
			}
			if got.Type != tc.wantType {
				t.Errorf("Type = %q, want %q", got.Type, tc.wantType)
			}
		})
	}
}
