package skills

import (
	"strings"
	"testing"
)

func TestPipBuildFailHint(t *testing.T) {
	cases := []struct {
		name    string
		pkg     string
		output  string
		wantSub string // substring expected in hint; "" = hint must be ""
	}{
		{
			name:    "pg_config psycopg2",
			pkg:     "psycopg2",
			output:  "Error: pg_config executable not found. Failed building wheel for psycopg2",
			wantSub: "psycopg2-binary",
		},
		{
			name:    "mysql_config mysqlclient",
			pkg:     "mysqlclient",
			output:  "mysql_config not found... Failed building wheel for mysqlclient",
			wantSub: "PyMySQL",
		},
		{
			name:    "psycopg v3 no binary",
			pkg:     "psycopg",
			output:  "Failed building wheel for psycopg",
			wantSub: "psycopg[binary]",
		},
		{
			name:    "pycrypto old",
			pkg:     "pycrypto",
			output:  "Failed building wheel for pycrypto",
			wantSub: "pycryptodome",
		},
		{
			name:    "generic wheel fail",
			pkg:     "somepkg",
			output:  "ERROR: Could not build wheels for somepkg",
			wantSub: "-binary",
		},
		{
			name:    "success has no hint",
			pkg:     "requests",
			output:  "",
			wantSub: "",
		},
		{
			name:    "network error not build",
			pkg:     "foo",
			output:  "ERROR: Could not find a version that satisfies the requirement foo",
			wantSub: "",
		},
		{
			name:    "auth 403 not build",
			pkg:     "internal-pkg",
			output:  "ERROR: HTTP 403 Forbidden",
			wantSub: "",
		},
		{
			name:    "binary pkg already — no psycopg hint loop",
			pkg:     "psycopg2-binary",
			output:  "Failed building wheel for psycopg2-binary",
			wantSub: "-binary",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pipBuildFailHint(tc.pkg, tc.output)
			if tc.wantSub == "" {
				if got != "" {
					t.Errorf("expected empty hint, got %q", got)
				}
				return
			}
			if !strings.Contains(got, tc.wantSub) {
				t.Errorf("hint %q does not contain %q", got, tc.wantSub)
			}
		})
	}
}
