package releasefeed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCheckFiltersChannelsAndComparesVersions(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
          {"tag_name":"v2.1.0-beta.2","name":"Beta","html_url":"https://example.test/beta","body":"Beta notes","prerelease":true,"draft":false,"published_at":"2026-10-11T10:00:00Z"},
          {"tag_name":"v2.0.1","name":"Stable","html_url":"https://example.test/stable","body":"Stable notes","prerelease":false,"draft":false,"published_at":"2026-10-10T10:00:00Z"},
          {"tag_name":"v9.0.0","draft":true,"prerelease":false}
        ]`))
	}))
	defer server.Close()

	client := &http.Client{Timeout: time.Second}
	stable, err := Check(context.Background(), client, server.URL, "2.0.0", "stable")
	if err != nil {
		t.Fatal(err)
	}
	if stable.Latest != "2.0.1" || !stable.UpdateAvailable {
		t.Fatalf("unexpected stable result: %+v", stable)
	}
	beta, err := Check(context.Background(), client, server.URL, "2.0.1", "beta")
	if err != nil {
		t.Fatal(err)
	}
	if beta.Latest != "2.1.0-beta.2" || !beta.UpdateAvailable {
		t.Fatalf("unexpected beta result: %+v", beta)
	}
}

func TestCheckRejectsInvalidChannelAndOversizedFeed(t *testing.T) {
	t.Parallel()
	if _, err := Check(context.Background(), http.DefaultClient, "", "1.0.0", "nightly"); err == nil {
		t.Fatal("expected invalid channel error")
	}
}

func TestCompareVersions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		left, right string
		want        int
	}{
		{"v1.6.1", "1.6.0", 1},
		{"1.7.0-beta.2", "1.7.0-beta.1", 1},
		{"1.7.0", "1.7.0-beta.9", 1},
		{"1.6.0", "1.6.0", 0},
	}
	for _, test := range cases {
		if got := compareVersions(test.left, test.right); got != test.want {
			t.Errorf("compareVersions(%q, %q)=%d, want %d", test.left, test.right, got, test.want)
		}
	}
}
