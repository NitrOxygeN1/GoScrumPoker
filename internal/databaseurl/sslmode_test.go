package databaseurl

import "testing"

func TestRequireSSL_postgresURL(t *testing.T) {
	got := RequireSSL("postgres://user:pass@host:5432/db?sslmode=disable")
	want := "postgres://user:pass@host:5432/db?sslmode=require"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRequireSSL_postgresURLNoQuery(t *testing.T) {
	got := RequireSSL("postgres://user:pass@host:5432/db")
	if got != "postgres://user:pass@host:5432/db?sslmode=require" {
		t.Fatalf("got %q", got)
	}
}

func TestRequireSSL_keyword(t *testing.T) {
	got := RequireSSL("host=localhost user=u dbname=d sslmode=prefer")
	if got != "host=localhost user=u dbname=d sslmode=require" {
		t.Fatalf("got %q", got)
	}
}
