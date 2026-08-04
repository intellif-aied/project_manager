package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVersionGreaterDoesNotDowngrade(t *testing.T) {
	for _, test := range []struct {
		candidate string
		current   string
		want      bool
	}{
		{"0.1.5", "0.1.4", true},
		{"0.2.0", "0.1.99", true},
		{"0.1.4", "0.1.4", false},
		{"0.1.3", "0.1.4", false},
	} {
		if got := versionGreater(test.candidate, test.current); got != test.want {
			t.Fatalf("versionGreater(%q,%q)=%t want %t", test.candidate, test.current, got, test.want)
		}
	}
}

func TestChecksumForFile(t *testing.T) {
	hash := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	value, err := checksumForFile([]byte(hash+"  aida-linux-amd64\n"), "aida-linux-amd64")
	if err != nil || value != hash {
		t.Fatalf("value=%q err=%v", value, err)
	}
}

func TestQuoteWindowsBatchArg(t *testing.T) {
	if got := quoteWindowsBatchArg(`upload --project 100%`); got != `"upload --project 100%%"` {
		t.Fatalf("quoteWindowsBatchArg()=%q", got)
	}
}

func TestMaybeAutoUpdateCachesSuccessfulImmediateCheck(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	oldVersion := Version
	Version = "0.1.14"
	t.Cleanup(func() { Version = oldVersion })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("0.1.14\n"))
	}))
	defer server.Close()

	cfg := &Config{ReleaseURL: server.URL}
	if err := maybeAutoUpdate(cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.LastKnownVersion != "0.1.14" || cfg.LastUpdateCheck == "" {
		t.Fatalf("update cache not written: %+v", cfg)
	}
}

func TestMaybeAutoUpdateAllowsUnknownCheckFailure(t *testing.T) {
	oldVersion := Version
	Version = "0.1.14"
	t.Cleanup(func() { Version = oldVersion })
	if err := maybeAutoUpdate(&Config{ReleaseURL: "http://127.0.0.1:1"}); err != nil {
		t.Fatalf("unknown check failure should be fail-open: %v", err)
	}
}

func TestMaybeAutoUpdateBlocksKnownRequiredVersionOnFailure(t *testing.T) {
	oldVersion := Version
	Version = "0.1.14"
	t.Cleanup(func() { Version = oldVersion })
	cfg := &Config{ReleaseURL: "http://127.0.0.1:1", LastKnownVersion: "0.1.15"}
	if err := maybeAutoUpdate(cfg); err == nil {
		t.Fatal("known required update unexpectedly failed open")
	}
}
