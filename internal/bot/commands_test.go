package appbot

import (
	"testing"

	"github.com/disgoorg/disgo/discord"
)

func TestCommandsDefinesMusicSlashCommands(t *testing.T) {
	commands := Commands()
	if len(commands) != 6 {
		t.Fatalf("len(Commands()) = %d, want 6", len(commands))
	}

	commandByName := make(map[string]discord.SlashCommandCreate, len(commands))
	for _, command := range commands {
		slashCommand, ok := command.(discord.SlashCommandCreate)
		if !ok {
			t.Fatalf("command type = %T, want discord.SlashCommandCreate", command)
		}
		commandByName[slashCommand.Name] = slashCommand
	}

	for _, commandName := range []string{
		dplayCommandName,
		dstopCommandName,
		dtermCommandName,
		dnextCommandName,
		dqueueCommandName,
		dvolCommandName,
	} {
		if _, ok := commandByName[commandName]; !ok {
			t.Fatalf("command %q was not registered", commandName)
		}
	}

	playCommand := commandByName[dplayCommandName]
	if len(playCommand.Options) != 1 {
		t.Fatalf("len(%s options) = %d, want 1", dplayCommandName, len(playCommand.Options))
	}
	playOption, ok := playCommand.Options[0].(discord.ApplicationCommandOptionString)
	if !ok {
		t.Fatalf("%s option type = %T, want discord.ApplicationCommandOptionString", dplayCommandName, playCommand.Options[0])
	}
	if playOption.Name != dplayCommandURLOptionName {
		t.Fatalf("%s option name = %q, want %q", dplayCommandName, playOption.Name, dplayCommandURLOptionName)
	}
	if !playOption.Required {
		t.Fatalf("%s option should be required", dplayCommandName)
	}

	for _, commandName := range []string{dstopCommandName, dtermCommandName, dnextCommandName, dqueueCommandName} {
		if got := len(commandByName[commandName].Options); got != 0 {
			t.Fatalf("len(%s options) = %d, want 0", commandName, got)
		}
	}

	volumeCommand := commandByName[dvolCommandName]
	if len(volumeCommand.Options) != 1 {
		t.Fatalf("len(%s options) = %d, want 1", dvolCommandName, len(volumeCommand.Options))
	}
	volumeOption, ok := volumeCommand.Options[0].(discord.ApplicationCommandOptionInt)
	if !ok {
		t.Fatalf("%s option type = %T, want discord.ApplicationCommandOptionInt", dvolCommandName, volumeCommand.Options[0])
	}
	if volumeOption.Name != dvolCommandVolumeOptionName {
		t.Fatalf("%s option name = %q, want %q", dvolCommandName, volumeOption.Name, dvolCommandVolumeOptionName)
	}
	if !volumeOption.Required {
		t.Fatalf("%s option should be required", dvolCommandName)
	}
	if volumeOption.MinValue == nil || *volumeOption.MinValue != minVolumeOptionValue {
		t.Fatalf("%s min value = %v, want %d", dvolCommandName, volumeOption.MinValue, minVolumeOptionValue)
	}
	if volumeOption.MaxValue == nil || *volumeOption.MaxValue != maxVolumeOptionValue {
		t.Fatalf("%s max value = %v, want %d", dvolCommandName, volumeOption.MaxValue, maxVolumeOptionValue)
	}
}
