package tools

import "testing"

func TestParseOmniRouteSearch_ResponseShape(t *testing.T) {
	payload := []byte(`{
	  "id":"search-1",
	  "provider":"searxng-search",
	  "query":"golang context cancellation",
	  "results":[
	    {"title":"Canceling in-progress operations - The Go Programming Language","url":"https://go.dev/doc/database/cancel-operations","snippet":"You can manage in-progress operations by using Go context.Context."},
	    {"title":"context package - context - Go Packages","url":"https://pkg.go.dev/context","snippet":"The WithCancel, WithDeadline, and WithTimeout functions."}
	  ]
	}`)

	results, err := parseOmniRouteSearch(payload)
	if err != nil {
		t.Fatalf("parseOmniRouteSearch returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if results[0].URL != "https://go.dev/doc/database/cancel-operations" {
		t.Fatalf("results[0].URL = %q", results[0].URL)
	}
	if results[0].Description == "" {
		t.Fatal("results[0].Description should not be empty")
	}
}
