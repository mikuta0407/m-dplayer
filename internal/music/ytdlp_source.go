package music

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

type ytdlpSource struct {
	metadata       Metadata
	ytdlpPath      string
	commandContext CommandContextFunc
}

type ytdlpMetadataPayload struct {
	Title string `json:"title"`
}

func (r *Resolver) resolveYTDLP(ctx context.Context, rawURL string) (InputSource, error) {
	metadata, err := r.resolveYTDLPMetadata(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	return &ytdlpSource{
		metadata:       metadata,
		ytdlpPath:      r.ytdlpPath,
		commandContext: r.commandContext,
	}, nil
}

func (r *Resolver) resolveYTDLPMetadata(ctx context.Context, rawURL string) (Metadata, error) {
	cmd := r.commandContext(ctx, r.ytdlpPath, "--dump-single-json", "--no-playlist", "--no-warnings", "--skip-download", rawURL)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrText := strings.TrimSpace(stderr.String())
		if isYTDLPUnsupported(stderrText) {
			return Metadata{}, &UnsupportedURLError{URL: rawURL, Reason: stderrText}
		}
		return Metadata{}, &YTDLPMetadataError{URL: rawURL, Stderr: stderrText, Err: err}
	}

	var payload ytdlpMetadataPayload
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		return Metadata{}, &YTDLPMetadataError{URL: rawURL, Err: fmt.Errorf("decode yt-dlp metadata JSON: %w", err), Stderr: strings.TrimSpace(stderr.String())}
	}
	if payload.Title == "" {
		return Metadata{}, &YTDLPMetadataError{URL: rawURL, Err: fmt.Errorf("yt-dlp metadata JSON did not contain a title"), Stderr: strings.TrimSpace(stderr.String())}
	}

	return Metadata{
		OriginalURL: rawURL,
		ResolvedURL: rawURL,
		Title:       payload.Title,
		SourceType:  SourceTypeYTDLP,
	}, nil
}

func (s *ytdlpSource) Metadata() Metadata {
	return s.metadata
}

func (s *ytdlpSource) Open(ctx context.Context) (io.ReadCloser, error) {
	cmd := s.commandContext(ctx, s.ytdlpPath, "--no-playlist", "--no-warnings", "-f", "bestaudio/best", "-o", "-", s.metadata.OriginalURL)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open yt-dlp stdout pipe: %w", err)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start yt-dlp stream command: %w", err)
	}

	return &commandPipeReadCloser{
		ctx:    ctx,
		reader: stdout,
		wait:   cmd.Wait,
		stderr: &stderr,
	}, nil
}

func (s *ytdlpSource) Cleanup() error {
	return nil
}

func (s *ytdlpSource) Description() string {
	return fmt.Sprintf("yt-dlp:%s", s.metadata.OriginalURL)
}

func (s *ytdlpSource) TempFilePath() string {
	return ""
}

type commandPipeReadCloser struct {
	ctx    context.Context
	reader io.ReadCloser
	wait   func() error
	stderr *bytes.Buffer

	mu     sync.Mutex
	closed bool
}

func (c *commandPipeReadCloser) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func (c *commandPipeReadCloser) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	readErr := c.reader.Close()
	waitErr := c.wait()
	if readErr != nil {
		return readErr
	}
	if waitErr != nil {
		if c.ctx != nil && c.ctx.Err() != nil {
			return nil
		}
		stderrText := strings.TrimSpace(c.stderr.String())
		if stderrText == "" {
			return fmt.Errorf("wait for yt-dlp stream command: %w", waitErr)
		}
		return fmt.Errorf("wait for yt-dlp stream command: %w: %s", waitErr, stderrText)
	}
	return nil
}

func isYTDLPUnsupported(stderr string) bool {
	lower := strings.ToLower(stderr)
	return strings.Contains(lower, "unsupported url") ||
		strings.Contains(lower, "no suitable extractor") ||
		strings.Contains(lower, "not a valid url") ||
		strings.Contains(lower, "unsupported site")
}
