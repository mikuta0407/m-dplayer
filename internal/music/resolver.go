package music

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"time"
)

type CommandContextFunc func(ctx context.Context, name string, args ...string) *exec.Cmd

type ResolverConfig struct {
	HTTPClient         *http.Client
	TempDir            string
	MaxDirectFileBytes int64
	YTDLPPath          string
	CommandContext     CommandContextFunc
}

type Resolver struct {
	httpClient         *http.Client
	tempDir            string
	maxDirectFileBytes int64
	ytdlpPath          string
	commandContext     CommandContextFunc
}

type directProbe struct {
	resolvedURL   *url.URL
	headers       http.Header
	contentLength int64
	isDirect      bool
}

func NewResolver(cfg ResolverConfig) *Resolver {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	tempDir := cfg.TempDir
	if tempDir == "" {
		tempDir = os.TempDir()
	}

	maxDirectFileBytes := cfg.MaxDirectFileBytes
	if maxDirectFileBytes <= 0 {
		maxDirectFileBytes = DefaultMaxDirectFileBytes
	}

	ytdlpPath := cfg.YTDLPPath
	if ytdlpPath == "" {
		ytdlpPath = "yt-dlp"
	}

	commandContext := cfg.CommandContext
	if commandContext == nil {
		commandContext = exec.CommandContext
	}

	return &Resolver{
		httpClient:         httpClient,
		tempDir:            tempDir,
		maxDirectFileBytes: maxDirectFileBytes,
		ytdlpPath:          ytdlpPath,
		commandContext:     commandContext,
	}
}

func (r *Resolver) Classify(ctx context.Context, rawURL string) (SourceType, error) {
	parsedURL, err := parseSupportedURL(rawURL)
	if err != nil {
		return "", err
	}
	if isDirectAudioURL(parsedURL) {
		return SourceTypeDirect, nil
	}

	probe, probeErr := r.probeDirect(ctx, rawURL)
	if probeErr == nil && probe.isDirect {
		return SourceTypeDirect, nil
	}

	if _, err := r.resolveYTDLPMetadata(ctx, rawURL); err == nil {
		return SourceTypeYTDLP, nil
	} else if probeErr != nil && errors.Is(err, ErrUnsupportedURL) {
		return "", probeErr
	} else {
		return "", err
	}
}

func (r *Resolver) Resolve(ctx context.Context, rawURL string) (InputSource, error) {
	parsedURL, err := parseSupportedURL(rawURL)
	if err != nil {
		return nil, err
	}

	if isDirectAudioURL(parsedURL) {
		return r.resolveDirect(ctx, rawURL, nil)
	}

	probe, probeErr := r.probeDirect(ctx, rawURL)
	if probeErr == nil && probe.isDirect {
		return r.resolveDirect(ctx, rawURL, &probe)
	}

	source, err := r.resolveYTDLP(ctx, rawURL)
	if err == nil {
		return source, nil
	}
	if probeErr != nil && errors.Is(err, ErrUnsupportedURL) {
		return nil, probeErr
	}
	return nil, err
}

func (r *Resolver) probeDirect(ctx context.Context, rawURL string) (directProbe, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return directProbe{}, fmt.Errorf("build HEAD request: %w", err)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return directProbe{}, fmt.Errorf("perform HEAD request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotImplemented {
		return directProbe{}, nil
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return directProbe{}, nil
	}

	resolvedURL := cloneURL(resp.Request.URL)
	return directProbe{
		resolvedURL:   resolvedURL,
		headers:       resp.Header.Clone(),
		contentLength: resp.ContentLength,
		isDirect:      looksLikeDirectFromHeaders(resolvedURL, resp.Header),
	}, nil
}

func parseSupportedURL(rawURL string) (*url.URL, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, &UnsupportedURLError{URL: rawURL, Reason: err.Error()}
	}
	if parsedURL == nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return nil, &UnsupportedURLError{URL: rawURL, Reason: "http/https URL is required"}
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, &UnsupportedURLError{URL: rawURL, Reason: "unsupported URL scheme"}
	}
	return parsedURL, nil
}

func cloneURL(u *url.URL) *url.URL {
	if u == nil {
		return nil
	}
	copied := *u
	return &copied
}
