package music

import (
	"context"
	"errors"
	"fmt"
	"io"
)

const DefaultMaxDirectFileBytes int64 = 100 << 20

type SourceType string

const (
	SourceTypeDirect SourceType = "direct"
	SourceTypeYTDLP  SourceType = "yt-dlp"
)

type Metadata struct {
	OriginalURL   string
	ResolvedURL   string
	Title         string
	SourceType    SourceType
	ContentType   string
	ContentLength int64
}

type InputSource interface {
	Metadata() Metadata
	Open(ctx context.Context) (io.ReadCloser, error)
	Cleanup() error
	Description() string
	TempFilePath() string
}

var (
	ErrUnsupportedURL     = errors.New("unsupported music url")
	ErrDirectFileTooLarge = errors.New("direct audio file exceeds size limit")
)

type UnsupportedURLError struct {
	URL    string
	Reason string
}

func (e *UnsupportedURLError) Error() string {
	if e == nil {
		return ""
	}
	if e.Reason == "" {
		return fmt.Sprintf("unsupported music url: %s", e.URL)
	}
	return fmt.Sprintf("unsupported music url: %s (%s)", e.URL, e.Reason)
}

func (e *UnsupportedURLError) Unwrap() error {
	return ErrUnsupportedURL
}

type DirectFileTooLargeError struct {
	URL        string
	LimitBytes int64
	SizeBytes  int64
}

func (e *DirectFileTooLargeError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("direct audio file exceeds size limit: url=%s size=%d limit=%d", e.URL, e.SizeBytes, e.LimitBytes)
}

func (e *DirectFileTooLargeError) Unwrap() error {
	return ErrDirectFileTooLarge
}

type YTDLPMetadataError struct {
	URL    string
	Stderr string
	Err    error
}

func (e *YTDLPMetadataError) Error() string {
	if e == nil {
		return ""
	}
	if e.Stderr == "" {
		return fmt.Sprintf("yt-dlp metadata resolution failed for %s: %v", e.URL, e.Err)
	}
	return fmt.Sprintf("yt-dlp metadata resolution failed for %s: %v: %s", e.URL, e.Err, e.Stderr)
}

func (e *YTDLPMetadataError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
