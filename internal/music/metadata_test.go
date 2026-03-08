package music

import (
	"net/http"
	"net/url"
	"testing"
)

func TestResolveDirectTitlePrefersContentDispositionFilename(t *testing.T) {
	resolvedURL, err := url.Parse("https://example.com/audio")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}

	header := http.Header{}
	header.Set("Content-Disposition", "attachment; filename*=UTF-8''hello%20world.opus")

	if got := resolveDirectTitle("https://example.com/audio", resolvedURL, header); got != "hello world.opus" {
		t.Fatalf("resolveDirectTitle() = %q, want %q", got, "hello world.opus")
	}
}

func TestResolveDirectTitleFallsBackToURLBasename(t *testing.T) {
	resolvedURL, err := url.Parse("https://example.com/%E3%83%86%E3%82%B9%E3%83%88.mp3")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}

	if got := resolveDirectTitle(resolvedURL.String(), resolvedURL, http.Header{}); got != "テスト.mp3" {
		t.Fatalf("resolveDirectTitle() = %q, want %q", got, "テスト.mp3")
	}
}

func TestResolveDirectTitleFallsBackToOriginalURL(t *testing.T) {
	rawURL := "https://example.com"
	resolvedURL, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}

	if got := resolveDirectTitle(rawURL, resolvedURL, http.Header{}); got != rawURL {
		t.Fatalf("resolveDirectTitle() = %q, want %q", got, rawURL)
	}
}
