package audio

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

type FFmpegCommandContextFunc func(ctx context.Context, name string, args ...string) *exec.Cmd

type FFmpegConfig struct {
	CommandPath    string
	CommandContext FFmpegCommandContextFunc
}

func NewFFmpegPCMReaderFromFile(ctx context.Context, cfg FFmpegConfig, inputPath string) (io.ReadCloser, error) {
	if inputPath == "" {
		return nil, fmt.Errorf("ffmpeg input path is required")
	}
	return startFFmpegPCM(ctx, cfg, inputPath, nil)
}

func NewFFmpegPCMReaderFromReader(ctx context.Context, cfg FFmpegConfig, input io.ReadCloser) (io.ReadCloser, error) {
	if input == nil {
		return nil, fmt.Errorf("ffmpeg input reader is nil")
	}
	return startFFmpegPCM(ctx, cfg, "pipe:0", input)
}

func startFFmpegPCM(ctx context.Context, cfg FFmpegConfig, inputArg string, input io.ReadCloser) (io.ReadCloser, error) {
	commandPath := cfg.CommandPath
	if commandPath == "" {
		commandPath = "ffmpeg"
	}
	commandContext := cfg.CommandContext
	if commandContext == nil {
		commandContext = exec.CommandContext
	}

	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-i", inputArg,
		"-vn",
		"-sn",
		"-dn",
		"-ac", "2",
		"-ar", "48000",
		"-f", "s16le",
		"pipe:1",
	}
	cmd := commandContext(ctx, commandPath, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open ffmpeg stdout pipe: %w", err)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	var (
		startPump     func()
		copyDone      <-chan error
		closeUpstream func() error
	)
	if input != nil {
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, fmt.Errorf("open ffmpeg stdin pipe: %w", err)
		}
		startPump, copyDone, closeUpstream = pumpReaderToPipe(input, stdin)
	}

	if err := cmd.Start(); err != nil {
		if closeUpstream != nil {
			_ = closeUpstream()
		}
		return nil, fmt.Errorf("start ffmpeg command: %w", err)
	}
	if startPump != nil {
		startPump()
	}

	return &processPipeReadCloser{
		ctx:           ctx,
		reader:        stdout,
		wait:          cmd.Wait,
		stderr:        &stderr,
		copyDone:      copyDone,
		closeUpstream: closeUpstream,
		commandName:   "ffmpeg",
	}, nil
}

func pumpReaderToPipe(input io.ReadCloser, pipe io.WriteCloser) (func(), <-chan error, func() error) {
	errCh := make(chan error, 1)
	var closeOnce sync.Once
	closeAll := func() error {
		var firstErr error
		closeOnce.Do(func() {
			if err := pipe.Close(); err != nil {
				firstErr = err
			}
			if err := input.Close(); firstErr == nil && err != nil {
				firstErr = err
			}
		})
		return firstErr
	}

	start := func() {
		go func() {
			_, err := io.Copy(pipe, input)
			closeErr := closeAll()
			if err == nil {
				err = closeErr
			}
			errCh <- err
			close(errCh)
		}()
	}

	return start, errCh, closeAll
}

type processPipeReadCloser struct {
	ctx           context.Context
	reader        io.ReadCloser
	wait          func() error
	stderr        *bytes.Buffer
	copyDone      <-chan error
	closeUpstream func() error
	commandName   string

	mu     sync.Mutex
	closed bool
}

func (p *processPipeReadCloser) Read(b []byte) (int, error) {
	return p.reader.Read(b)
}

func (p *processPipeReadCloser) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()

	var firstErr error
	if p.closeUpstream != nil {
		if err := p.closeUpstream(); err != nil {
			firstErr = err
		}
	}
	if p.reader != nil {
		if err := p.reader.Close(); firstErr == nil && err != nil {
			firstErr = err
		}
	}
	if p.copyDone != nil {
		if err, ok := <-p.copyDone; ok && firstErr == nil && err != nil && !isProcessContextDone(p.ctx, err) {
			firstErr = err
		}
	}
	if p.wait != nil {
		if err := p.wait(); firstErr == nil && err != nil && !isProcessContextDone(p.ctx, err) {
			stderrText := ""
			if p.stderr != nil {
				stderrText = strings.TrimSpace(p.stderr.String())
			}
			if stderrText == "" {
				firstErr = fmt.Errorf("wait for %s process: %w", p.commandName, err)
			} else {
				firstErr = fmt.Errorf("wait for %s process: %w: %s", p.commandName, err, stderrText)
			}
		}
	}
	return firstErr
}

func isProcessContextDone(ctx context.Context, err error) bool {
	return err != nil && ctx != nil && ctx.Err() != nil
}
