package appbot

import (
	"fmt"
	"log/slog"

	disgobot "github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
)

const (
	dplayCommandName            = "dplay"
	dplayCommandURLOptionName   = "url"
	dstopCommandName            = "dstop"
	dtermCommandName            = "dterm"
	dnextCommandName            = "dnext"
	dqueueCommandName           = "dqueue"
	dvolCommandName             = "dvol"
	dvolCommandVolumeOptionName = "volume"
	minVolumeOptionValue        = 0
	maxVolumeOptionValue        = 10
)

func Commands() []discord.ApplicationCommandCreate {
	minVolume := minVolumeOptionValue
	maxVolume := maxVolumeOptionValue

	return []discord.ApplicationCommandCreate{
		discord.SlashCommandCreate{
			Name:        dplayCommandName,
			Description: "URL を再生するか、既存セッションのキューへ追加します",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionString{
					Name:        dplayCommandURLOptionName,
					Description: "再生またはキュー追加する URL",
					Required:    true,
				},
			},
		},
		discord.SlashCommandCreate{
			Name:        dstopCommandName,
			Description: "現在再生中の曲を停止して次の曲へ進めます",
		},
		discord.SlashCommandCreate{
			Name:        dtermCommandName,
			Description: "再生を停止し、キューを破棄してボイス接続を切断します",
		},
		discord.SlashCommandCreate{
			Name:        dnextCommandName,
			Description: "現在再生中の曲をスキップして次の曲へ進めます",
		},
		discord.SlashCommandCreate{
			Name:        dqueueCommandName,
			Description: "現在再生中の曲と待機キューを表示します",
		},
		discord.SlashCommandCreate{
			Name:        dvolCommandName,
			Description: "再生音量を 0 から 10 で設定します",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionInt{
					Name:        dvolCommandVolumeOptionName,
					Description: "音量 (0-10, 10 が等倍)",
					Required:    true,
					MinValue:    &minVolume,
					MaxValue:    &maxVolume,
				},
			},
		},
	}
}

func RegisterCommands(client *disgobot.Client, guildID *snowflake.ID) error {
	commands := Commands()

	if guildID != nil {
		if _, err := client.Rest.SetGuildCommands(client.ApplicationID, *guildID, commands); err != nil {
			return fmt.Errorf("register guild commands: %w", err)
		}
		slog.Info("registered guild slash commands",
			slog.Uint64("guild_id", uint64(*guildID)),
			slog.Int("command_count", len(commands)),
		)
		return nil
	}

	if _, err := client.Rest.SetGlobalCommands(client.ApplicationID, commands); err != nil {
		return fmt.Errorf("register global commands: %w", err)
	}

	slog.Info("registered global slash commands", slog.Int("command_count", len(commands)))
	return nil
}
