package music

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestResolverResolveDirectURLByHeadersAndDownloadsTempFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("Content-Disposition", `attachment; filename="song.mp3"`)
		w.Header().Set("Content-Length", "10")
		if r.Method == http.MethodHead {
			return
		}
		_, _ = io.WriteString(w, "0123456789")
	}))
	defer server.Close()

	resolver := NewResolver(ResolverConfig{
		HTTPClient:         server.Client(),
		TempDir:            t.TempDir(),
		MaxDirectFileBytes: 1024,
	})

	kind, err := resolver.Classify(context.Background(), server.URL+"/download")
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	if kind != SourceTypeDirect {
		t.Fatalf("Classify() = %q, want %q", kind, SourceTypeDirect)
	}

	source, err := resolver.Resolve(context.Background(), server.URL+"/download")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	defer source.Cleanup()

	metadata := source.Metadata()
	if metadata.SourceType != SourceTypeDirect {
		t.Fatalf("Metadata().SourceType = %q, want %q", metadata.SourceType, SourceTypeDirect)
	}
	if metadata.Title != "song.mp3" {
		t.Fatalf("Metadata().Title = %q, want %q", metadata.Title, "song.mp3")
	}
	if source.TempFilePath() == "" {
		t.Fatal("TempFilePath() = empty, want downloaded file path")
	}

	reader, err := source.Open(context.Background())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("reader.Close() error = %v", err)
	}
	if string(data) != "0123456789" {
		t.Fatalf("downloaded body = %q, want %q", string(data), "0123456789")
	}

	tempFilePath := source.TempFilePath()
	if err := source.Cleanup(); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if _, err := os.Stat(tempFilePath); !os.IsNotExist(err) {
		t.Fatalf("temp file should be removed on cleanup, stat err = %v", err)
	}
}

func TestResolverResolveDirectURLRejectsWhenHEADContentLengthExceedsLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("Content-Length", "9")
		if r.Method == http.MethodHead {
			return
		}
		_, _ = io.WriteString(w, "123456789")
	}))
	defer server.Close()

	resolver := NewResolver(ResolverConfig{
		HTTPClient:         server.Client(),
		TempDir:            t.TempDir(),
		MaxDirectFileBytes: 8,
	})

	_, err := resolver.Resolve(context.Background(), server.URL+"/stream")
	if err == nil {
		t.Fatal("Resolve() error = nil, want size limit failure")
	}
	if !errors.Is(err, ErrDirectFileTooLarge) {
		t.Fatalf("Resolve() error = %v, want %v", err, ErrDirectFileTooLarge)
	}
}

func TestResolverResolveDirectURLRejectsWhenGETExceedsLimitWithoutContentLength(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = io.WriteString(w, "123456789")
	}))
	defer server.Close()

	resolver := NewResolver(ResolverConfig{
		HTTPClient:         server.Client(),
		TempDir:            t.TempDir(),
		MaxDirectFileBytes: 8,
	})

	_, err := resolver.Resolve(context.Background(), server.URL+"/track.mp3")
	if err == nil {
		t.Fatal("Resolve() error = nil, want size limit failure")
	}
	if !errors.Is(err, ErrDirectFileTooLarge) {
		t.Fatalf("Resolve() error = %v, want %v", err, ErrDirectFileTooLarge)
	}
}

func TestResolverResolveYTDLPURLUsesMetadataAndStreamSeparately(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method == http.MethodHead {
			return
		}
		_, _ = io.WriteString(w, "<html>video page</html>")
	}))
	defer server.Close()

	tempDir := t.TempDir()
	ytdlpPath := filepath.Join(tempDir, "yt-dlp")
	writeExecutable(t, ytdlpPath, `#!/bin/sh
mode="stream"
for arg in "$@"; do
  if [ "$arg" = "--dump-single-json" ]; then
    mode="metadata"
  fi
done
if [ "$mode" = "metadata" ]; then
  printf '{"title":"Example Video"}'
  exit 0
fi
printf 'stream-audio'
`)

	resolver := NewResolver(ResolverConfig{
		HTTPClient: server.Client(),
		YTDLPPath:  ytdlpPath,
	})

	kind, err := resolver.Classify(context.Background(), server.URL+"/watch")
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	if kind != SourceTypeYTDLP {
		t.Fatalf("Classify() = %q, want %q", kind, SourceTypeYTDLP)
	}

	source, err := resolver.Resolve(context.Background(), server.URL+"/watch")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	defer source.Cleanup()

	metadata := source.Metadata()
	if metadata.SourceType != SourceTypeYTDLP {
		t.Fatalf("Metadata().SourceType = %q, want %q", metadata.SourceType, SourceTypeYTDLP)
	}
	if metadata.Title != "Example Video" {
		t.Fatalf("Metadata().Title = %q, want %q", metadata.Title, "Example Video")
	}
	if source.TempFilePath() != "" {
		t.Fatalf("TempFilePath() = %q, want empty for yt-dlp", source.TempFilePath())
	}

	reader, err := source.Open(context.Background())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("reader.Close() error = %v", err)
	}
	if string(data) != "stream-audio" {
		t.Fatalf("stream body = %q, want %q", string(data), "stream-audio")
	}
}

func TestResolverResolveReturnsUnsupportedURLWhenNeitherDirectNorYTDLP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method == http.MethodHead {
			return
		}
		_, _ = io.WriteString(w, "<html>plain page</html>")
	}))
	defer server.Close()

	tempDir := t.TempDir()
	ytdlpPath := filepath.Join(tempDir, "yt-dlp")
	writeExecutable(t, ytdlpPath, `#!/bin/sh
echo 'Unsupported URL' >&2
exit 1
`)

	resolver := NewResolver(ResolverConfig{
		HTTPClient: server.Client(),
		YTDLPPath:  ytdlpPath,
	})

	_, err := resolver.Classify(context.Background(), server.URL+"/page")
	if err == nil {
		t.Fatal("Classify() error = nil, want unsupported URL")
	}
	if !errors.Is(err, ErrUnsupportedURL) {
		t.Fatalf("Classify() error = %v, want %v", err, ErrUnsupportedURL)
	}

	_, err = resolver.Resolve(context.Background(), server.URL+"/page")
	if err == nil {
		t.Fatal("Resolve() error = nil, want unsupported URL")
	}
	if !errors.Is(err, ErrUnsupportedURL) {
		t.Fatalf("Resolve() error = %v, want %v", err, ErrUnsupportedURL)
	}
}

func writeExecutable(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}
