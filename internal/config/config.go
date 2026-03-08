package config

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/disgoorg/snowflake/v2"
)

type Config struct {
	Token          string
	FFmpegPath     string
	YTDLPPath      string
	CommandGuildID *snowflake.ID
}

func Load() (Config, error) {
	cfg := Config{
		Token: strings.TrimSpace(os.Getenv("DISCORD_TOKEN")),
	}

	if err := cfg.validateRequired(); err != nil {
		return Config{}, err
	}

	ffmpegPath, err := resolveCommandPath("FFMPEG_PATH", "ffmpeg")
	if err != nil {
		return Config{}, err
	}
	cfg.FFmpegPath = ffmpegPath

	ytdlpPath, err := resolveCommandPath("YT_DLP_PATH", "yt-dlp")
	if err != nil {
		return Config{}, err
	}
	cfg.YTDLPPath = ytdlpPath

	commandGuildID, err := loadOptionalSnowflakeEnv("DISCORD_COMMAND_GUILD_ID")
	if err != nil {
		return Config{}, err
	}
	cfg.CommandGuildID = commandGuildID

	return cfg, nil
}

func (c Config) validateRequired() error {
	missing := make([]string, 0, 1)
	if c.Token == "" {
		missing = append(missing, "DISCORD_TOKEN")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	return nil
}

func resolveCommandPath(envKey string, defaultCommand string) (string, error) {
	configuredPath := strings.TrimSpace(os.Getenv(envKey))
	commandName := defaultCommand
	if configuredPath != "" {
		commandName = configuredPath
	}

	resolvedPath, err := exec.LookPath(commandName)
	if err != nil {
		if configuredPath != "" {
			return "", fmt.Errorf("%s command was not found: %s: %w", envKey, configuredPath, err)
		}
		return "", fmt.Errorf("%s command was not found in PATH: %w", defaultCommand, err)
	}
	return resolvedPath, nil
}

func loadOptionalSnowflakeEnv(key string) (*snowflake.ID, error) {
	rawValue := strings.TrimSpace(os.Getenv(key))
	if rawValue == "" {
		return nil, nil
	}

	id, err := parseSnowflakeEnv(key, rawValue)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func parseSnowflakeEnv(key string, value string) (snowflake.ID, error) {
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an unsigned integer: %w", key, err)
	}
	return snowflake.ID(id), nil
}
