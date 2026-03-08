package music

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
)

type directSource struct {
	metadata Metadata
	path     string
}

func (s *directSource) Metadata() Metadata {
	return s.metadata
}

func (s *directSource) Open(ctx context.Context) (io.ReadCloser, error) {
	return os.Open(s.path)
}

func (s *directSource) Cleanup() error {
	return removeFileIfExists(s.path)
}

func (s *directSource) Description() string {
	return s.path
}

func (s *directSource) TempFilePath() string {
	return s.path
}

func (r *Resolver) resolveDirect(ctx context.Context, rawURL string, probe *directProbe) (InputSource, error) {
	if probe == nil {
		if detected, err := r.probeDirect(ctx, rawURL); err == nil {
			probe = &detected
		}
	}
	if probe != nil && probe.contentLength > r.maxDirectFileBytes {
		return nil, &DirectFileTooLargeError{URL: rawURL, SizeBytes: probe.contentLength, LimitBytes: r.maxDirectFileBytes}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build GET request: %w", err)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download direct audio: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("download direct audio: unexpected status %s", resp.Status)
	}
	if resp.ContentLength > r.maxDirectFileBytes {
		return nil, &DirectFileTooLargeError{URL: rawURL, SizeBytes: resp.ContentLength, LimitBytes: r.maxDirectFileBytes}
	}

	resolvedURL := cloneURL(resp.Request.URL)
	if !looksLikeDirectFromHeaders(resolvedURL, resp.Header) && (probe == nil || !probe.isDirect) {
		return nil, &UnsupportedURLError{URL: rawURL, Reason: "response does not look like a direct audio file"}
	}

	title := resolveDirectTitle(rawURL, resolvedURL, resp.Header)
	if title == rawURL && probe != nil {
		if probedTitle := resolveDirectTitle(rawURL, probe.resolvedURL, probe.headers); probedTitle != "" {
			title = probedTitle
		}
	}

	pattern := "direct_*"
	if suffix := directTempFileSuffix(title, resolvedURL); suffix != "" {
		pattern += suffix
	}

	outputFile, err := os.CreateTemp(r.tempDir, pattern)
	if err != nil {
		return nil, fmt.Errorf("create temp file for direct audio: %w", err)
	}
	outputFilePath := outputFile.Name()
	cleanupOnError := true
	defer func() {
		if cleanupOnError {
			_ = outputFile.Close()
			_ = removeFileIfExists(outputFilePath)
		}
	}()

	written, err := io.Copy(outputFile, io.LimitReader(resp.Body, r.maxDirectFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("copy direct audio response: %w", err)
	}
	if written > r.maxDirectFileBytes {
		return nil, &DirectFileTooLargeError{URL: rawURL, SizeBytes: written, LimitBytes: r.maxDirectFileBytes}
	}
	if err := outputFile.Close(); err != nil {
		return nil, fmt.Errorf("close temp file for direct audio: %w", err)
	}

	cleanupOnError = false
	return &directSource{
		path: outputFilePath,
		metadata: Metadata{
			OriginalURL:   rawURL,
			ResolvedURL:   resolvedURL.String(),
			Title:         title,
			SourceType:    SourceTypeDirect,
			ContentType:   resp.Header.Get("Content-Type"),
			ContentLength: written,
		},
	}, nil
}

func removeFileIfExists(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
