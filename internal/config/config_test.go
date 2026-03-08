package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRequiresOnlyDiscordTokenAndResolvesCommands(t *testing.T) {
	binDir := t.TempDir()
	ffmpegPath := createExecutable(t, binDir, "ffmpeg")
	ytdlpPath := createExecutable(t, binDir, "yt-dlp")

	t.Setenv("PATH", binDir)
	t.Setenv("DISCORD_TOKEN", "test-token")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Token != "test-token" {
		t.Fatalf("Token = %q, want %q", cfg.Token, "test-token")
	}
	if cfg.FFmpegPath != ffmpegPath {
		t.Fatalf("FFmpegPath = %q, want %q", cfg.FFmpegPath, ffmpegPath)
	}
	if cfg.YTDLPPath != ytdlpPath {
		t.Fatalf("YTDLPPath = %q, want %q", cfg.YTDLPPath, ytdlpPath)
	}
}

func TestLoadUsesOptionalCommandPathOverrides(t *testing.T) {
	binDir := t.TempDir()
	ffmpegPath := createExecutable(t, binDir, "custom-ffmpeg")
	ytdlpPath := createExecutable(t, binDir, "custom-yt-dlp")

	t.Setenv("PATH", t.TempDir())
	t.Setenv("DISCORD_TOKEN", "test-token")
	t.Setenv("FFMPEG_PATH", ffmpegPath)
	t.Setenv("YT_DLP_PATH", ytdlpPath)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.FFmpegPath != ffmpegPath {
		t.Fatalf("FFmpegPath = %q, want %q", cfg.FFmpegPath, ffmpegPath)
	}
	if cfg.YTDLPPath != ytdlpPath {
		t.Fatalf("YTDLPPath = %q, want %q", cfg.YTDLPPath, ytdlpPath)
	}
}

func TestLoadReturnsErrorWhenFFmpegIsMissing(t *testing.T) {
	binDir := t.TempDir()
	createExecutable(t, binDir, "yt-dlp")

	t.Setenv("PATH", binDir)
	t.Setenv("DISCORD_TOKEN", "test-token")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "ffmpeg command was not found in PATH") {
		t.Fatalf("Load() error = %q", err.Error())
	}
}

func TestLoadReturnsErrorWhenYTDLPIsMissing(t *testing.T) {
	binDir := t.TempDir()
	createExecutable(t, binDir, "ffmpeg")

	t.Setenv("PATH", binDir)
	t.Setenv("DISCORD_TOKEN", "test-token")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "yt-dlp command was not found in PATH") {
		t.Fatalf("Load() error = %q", err.Error())
	}
}

func createExecutable(t *testing.T, dir string, name string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	return path
}
