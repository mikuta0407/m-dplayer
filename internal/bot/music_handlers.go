package appbot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	disgobot "github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
	"github.com/mikuta0407/m-dplayer/internal/audio"
	appconfig "github.com/mikuta0407/m-dplayer/internal/config"
	"github.com/mikuta0407/m-dplayer/internal/music"
	"github.com/mikuta0407/m-dplayer/internal/session"
)

const commandResolveTimeout = 90 * time.Second

func newMusicResolver(cfg appconfig.Config) *music.Resolver {
	return music.NewResolver(music.ResolverConfig{YTDLPPath: cfg.YTDLPPath})
}

func newFFmpegConfig(cfg appconfig.Config) audio.FFmpegConfig {
	return audio.FFmpegConfig{CommandPath: cfg.FFmpegPath}
}

func (h *Handler) handleDPlay(event *events.ApplicationCommandInteractionCreate) {
	guildID := event.GuildID()
	if guildID == nil {
		h.respondEphemeral(event, guildOnlyMessage())
		return
	}

	rawURL, ok := event.SlashCommandInteractionData().OptString(dplayCommandURLOptionName)
	if !ok || strings.TrimSpace(rawURL) == "" {
		h.respondEphemeral(event, "再生する URL を指定してください。")
		return
	}

	if err := event.DeferCreateMessage(true); err != nil {
		slog.Error("failed to defer dplay interaction response",
			slog.Any("err", err),
			slog.Uint64("guild_id", uint64(*guildID)),
			slog.Uint64("actor_user_id", uint64(event.User().ID)),
		)
		return
	}

	go h.processDPlay(
		event,
		*guildID,
		event.Channel().ID(),
		event.User().ID,
		interactionActorDisplayName(event),
		strings.TrimSpace(rawURL),
	)
}

func (h *Handler) processDPlay(event *events.ApplicationCommandInteractionCreate, guildID snowflake.ID, textChannelID snowflake.ID, actorUserID snowflake.ID, actorDisplayName string, rawURL string) {
	finalized := false
	finish := func(content string) {
		finalized = true
		_ = h.updateDeferredResponse(event, content)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("panic while handling dplay command",
				slog.Any("panic", recovered),
				slog.Uint64("guild_id", uint64(guildID)),
				slog.Uint64("actor_user_id", uint64(actorUserID)),
			)
			if !finalized {
				finish(internalErrorMessage())
			}
		}
	}()

	voiceState, err := h.resolveUserVoiceState(event.Client(), guildID, actorUserID)
	if err != nil {
		slog.Error("failed to resolve user voice state for dplay",
			slog.Any("err", err),
			slog.Uint64("guild_id", uint64(guildID)),
			slog.Uint64("actor_user_id", uint64(actorUserID)),
		)
		finish("コマンド実行者の Voice State を取得できませんでした。少し待ってから再度お試しください。")
		return
	}
	if voiceState == nil || voiceState.ChannelID == nil {
		finish("ボイスチャンネルに参加してから `/dplay` を実行してください。")
		return
	}
	voiceChannelID := *voiceState.ChannelID

	var (
		sess       *session.Session
		newSession bool
	)
	if h.sessions != nil {
		if existing, ok := h.sessions.Get(guildID); ok {
			if existing.VoiceChannelID() != voiceChannelID {
				finish("このサーバーでは別のボイスチャンネルで再生中です。同じボイスチャンネルで実行するか、`/dterm` で終了してください。")
				return
			}
			sess = existing
		}
	}

	resolveCtx, cancelResolve := context.WithTimeout(context.Background(), commandResolveTimeout)
	defer cancelResolve()

	source, err := h.resolver.Resolve(resolveCtx, rawURL)
	if err != nil {
		slog.Warn("failed to resolve music source",
			slog.Any("err", err),
			slog.Uint64("guild_id", uint64(guildID)),
			slog.String("url", rawURL),
		)
		finish(userMessageForResolveError(err))
		return
	}

	cleanupSource := true
	defer func() {
		if cleanupSource && source != nil {
			_ = source.Cleanup()
		}
	}()

	if sess == nil {
		sess, err = h.openVoiceSession(event.Client(), guildID, textChannelID, voiceChannelID)
		if err != nil {
			slog.Error("failed to open music voice session",
				slog.Any("err", err),
				slog.Uint64("guild_id", uint64(guildID)),
				slog.Uint64("voice_channel_id", uint64(voiceChannelID)),
			)
			finish("ボイスチャンネルへの接続に失敗しました。Bot に接続権限があるか確認して、再度お試しください。")
			return
		}
		newSession = true
	}

	item := buildQueueItem(source, actorUserID, actorDisplayName, textChannelID)
	if item.TempFilePath != "" {
		sess.TrackTempFile(item.TempFilePath)
	}
	if err := sess.Enqueue(item); err != nil {
		if item.TempFilePath != "" {
			_ = sess.RemoveTempFile(item.TempFilePath)
		}
		if newSession && h.sessions != nil {
			closeCtx, closeCancel := context.WithTimeout(context.Background(), voiceCleanupTimeout)
			defer closeCancel()
			_ = h.sessions.Destroy(closeCtx, guildID)
		}
		finish(userMessageForEnqueueError(err))
		return
	}
	cleanupSource = false

	if err := h.postQueuedMessage(event.Client(), textChannelID, item); err != nil {
		slog.Warn("failed to post queued message",
			slog.Any("err", err),
			slog.Uint64("guild_id", uint64(guildID)),
			slog.Uint64("text_channel_id", uint64(textChannelID)),
		)
	}

	if newSession {
		go h.runPlaybackWorker(sess)
		go h.runIdleVoiceKeepAlive(sess)
		if err := h.primeVoiceConnection(sess); err != nil {
			slog.Warn("failed to prime music voice connection",
				slog.Any("err", err),
				slog.Uint64("guild_id", uint64(guildID)),
				slog.Uint64("voice_channel_id", uint64(voiceChannelID)),
			)
		}
	}

	finish(fmt.Sprintf("キューへ追加しました。現在のキュー件数: %d", sess.QueueLen()))
}

func (h *Handler) handleDStopLike(event *events.ApplicationCommandInteractionCreate, commandName string) {
	guildID := event.GuildID()
	if guildID == nil {
		h.respondEphemeral(event, guildOnlyMessage())
		return
	}

	content := h.processDStopLike(event.Client(), *guildID, event.Channel().ID(), interactionActorDisplayName(event), commandName)
	h.respondEphemeral(event, content)
}

func (h *Handler) processDStopLike(client *disgobot.Client, guildID snowflake.ID, textChannelID snowflake.ID, actorDisplayName string, commandName string) string {
	if h.sessions == nil {
		return noActiveSessionMessage()
	}

	sess, ok := h.sessions.Get(guildID)
	if !ok {
		return noActiveSessionMessage()
	}

	current, ok := sess.Current()
	if !ok {
		return "現在再生中の曲はありません。"
	}
	if !sess.SkipCurrent() {
		return "現在再生中の曲はありません。"
	}

	verb := "停止"
	channelMessage := fmt.Sprintf("Stopped: %s by %s", formatTrackLink(current), actorDisplayName)
	if commandName == dnextCommandName {
		verb = "スキップ"
		channelMessage = fmt.Sprintf("Skipped: %s by %s", formatTrackLink(current), actorDisplayName)
	}
	if err := postTextChannelMessage(client, textChannelID, channelMessage); err != nil {
		slog.Warn("failed to post stop/next update message",
			slog.Any("err", err),
			slog.Uint64("guild_id", uint64(guildID)),
			slog.Uint64("text_channel_id", uint64(textChannelID)),
		)
	}
	return fmt.Sprintf("現在再生中の曲を%sしました。", verb)
}

func (h *Handler) handleDTerm(event *events.ApplicationCommandInteractionCreate) {
	guildID := event.GuildID()
	if guildID == nil {
		h.respondEphemeral(event, guildOnlyMessage())
		return
	}

	content := h.processDTerm(event.Client(), *guildID, event.Channel().ID(), interactionActorDisplayName(event))
	h.respondEphemeral(event, content)
}

func (h *Handler) processDTerm(client *disgobot.Client, guildID snowflake.ID, textChannelID snowflake.ID, actorDisplayName string) string {
	if h.sessions == nil {
		return noActiveSessionMessage()
	}

	sess, ok := h.sessions.Get(guildID)
	if !ok {
		return noActiveSessionMessage()
	}

	cleared := sess.ClearQueue()
	sess.SkipCurrent()

	closeCtx, cancel := context.WithTimeout(context.Background(), voiceCleanupTimeout)
	defer cancel()
	if err := h.sessions.Destroy(closeCtx, guildID); err != nil && !errors.Is(err, session.ErrSessionNotFound) {
		slog.Error("failed to terminate music session",
			slog.Any("err", err),
			slog.Uint64("guild_id", uint64(guildID)),
		)
		return "再生セッションの終了に失敗しました。少し待ってから再度お試しください。"
	}

	message := fmt.Sprintf("Terminated playback and cleared %d queued track(s) by %s", len(cleared), actorDisplayName)
	if err := postTextChannelMessage(client, textChannelID, message); err != nil {
		slog.Warn("failed to post termination update message",
			slog.Any("err", err),
			slog.Uint64("guild_id", uint64(guildID)),
			slog.Uint64("text_channel_id", uint64(textChannelID)),
		)
	}
	return fmt.Sprintf("再生を終了し、待機キュー %d 件を破棄しました。", len(cleared))
}

func (h *Handler) handleDQueue(event *events.ApplicationCommandInteractionCreate) {
	guildID := event.GuildID()
	if guildID == nil {
		h.respondEphemeral(event, guildOnlyMessage())
		return
	}
	h.respondEphemeral(event, h.queueSnapshotMessage(*guildID))
}

func (h *Handler) queueSnapshotMessage(guildID snowflake.ID) string {
	if h.sessions == nil {
		return noActiveSessionMessage()
	}

	sess, ok := h.sessions.Get(guildID)
	if !ok {
		return noActiveSessionMessage()
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("音量: %d\n", sess.Volume()))

	if current, ok := sess.Current(); ok {
		builder.WriteString("再生中:\n")
		builder.WriteString("- ")
		builder.WriteString(formatQueueLine(current))
		builder.WriteString("\n")
	} else {
		builder.WriteString("再生中: なし\n")
	}

	queue := sess.SnapshotQueue()
	if len(queue) == 0 {
		builder.WriteString("待機キュー: なし")
		return truncateDiscordMessage(builder.String())
	}

	builder.WriteString("待機キュー:\n")
	for i, item := range queue {
		builder.WriteString(fmt.Sprintf("%d. %s\n", i+1, formatQueueLine(item)))
	}
	return truncateDiscordMessage(strings.TrimSpace(builder.String()))
}

func (h *Handler) handleDVol(event *events.ApplicationCommandInteractionCreate) {
	guildID := event.GuildID()
	if guildID == nil {
		h.respondEphemeral(event, guildOnlyMessage())
		return
	}

	volume, ok := event.SlashCommandInteractionData().OptInt(dvolCommandVolumeOptionName)
	if !ok {
		h.respondEphemeral(event, "音量を指定してください。")
		return
	}

	content := h.processDVol(event.Client(), *guildID, event.Channel().ID(), interactionActorDisplayName(event), volume)
	h.respondEphemeral(event, content)
}

func (h *Handler) processDVol(client *disgobot.Client, guildID snowflake.ID, textChannelID snowflake.ID, actorDisplayName string, volume int) string {
	if volume < session.MinVolume || volume > session.MaxVolume {
		return fmt.Sprintf("音量は %d から %d の範囲で指定してください。", session.MinVolume, session.MaxVolume)
	}
	if h.sessions == nil {
		return noActiveSessionMessage()
	}

	sess, ok := h.sessions.Get(guildID)
	if !ok {
		return noActiveSessionMessage()
	}
	if err := sess.SetVolume(volume); err != nil {
		if errors.Is(err, session.ErrInvalidVolume) {
			return fmt.Sprintf("音量は %d から %d の範囲で指定してください。", session.MinVolume, session.MaxVolume)
		}
		return internalErrorMessage()
	}

	if err := postTextChannelMessage(client, textChannelID, fmt.Sprintf("Volume: %d by %s", volume, actorDisplayName)); err != nil {
		slog.Warn("failed to post volume update message",
			slog.Any("err", err),
			slog.Uint64("guild_id", uint64(guildID)),
			slog.Uint64("text_channel_id", uint64(textChannelID)),
		)
	}
	return fmt.Sprintf("音量を %d に設定しました。", volume)
}

func (h *Handler) runPlaybackWorker(sess *session.Session) {
	if sess == nil {
		return
	}

	for {
		if sess.Context().Err() != nil {
			return
		}
		if sess.ShouldAutoDisconnect() {
			h.destroySessionAfterPlayback(sess.GuildID())
			return
		}

		request, err := sess.WaitDequeue()
		if err != nil {
			if errors.Is(err, session.ErrSessionClosed) || errors.Is(err, context.Canceled) {
				return
			}
			slog.Warn("failed to dequeue playback request",
				slog.Any("err", err),
				slog.Uint64("guild_id", uint64(sess.GuildID())),
			)
			continue
		}

		h.processPlaybackRequest(sess, request)
		if sess.ShouldAutoDisconnect() {
			h.destroySessionAfterPlayback(sess.GuildID())
			return
		}
	}
}

func (h *Handler) processPlaybackRequest(sess *session.Session, request session.PlaybackRequest) {
	playbackCtx, playbackCancel := context.WithCancel(sess.Context())
	if err := sess.StartCurrent(request, playbackCancel); err != nil {
		if !errors.Is(err, session.ErrSessionClosed) {
			slog.Warn("failed to mark current playback request",
				slog.Any("err", err),
				slog.Uint64("guild_id", uint64(sess.GuildID())),
			)
		}
		return
	}
	defer sess.FinishCurrent()
	defer cleanupPlaybackRequest(sess, request)

	stream, err := h.newPlaybackStream(playbackCtx, sess, request)
	if err != nil {
		logMusicPlaybackError(sess, request, err)
		return
	}

	if err := sendPlaybackStream(playbackCtx, sess, stream); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		logMusicPlaybackError(sess, request, err)
		return
	}

	slog.Info("completed music playback",
		slog.Uint64("guild_id", uint64(sess.GuildID())),
		slog.Uint64("voice_channel_id", uint64(sess.VoiceChannelID())),
		slog.String("title", request.Title),
		slog.String("url", request.URL),
	)
}

func (h *Handler) newPlaybackStream(ctx context.Context, sess *session.Session, request session.PlaybackRequest) (audio.OpusFrameStream, error) {
	if request.SourceType == session.QueueSourceTypeDirect {
		if request.TempFilePath == "" {
			return nil, fmt.Errorf("direct track temp file path is empty")
		}
		pcmReader, err := audio.NewFFmpegPCMReaderFromFile(ctx, h.ffmpegConfig, request.TempFilePath)
		if err != nil {
			return nil, err
		}
		return audio.NewPCMStream(pcmReader, audio.PCMStreamOptions{VolumeProvider: sess.Volume})
	}

	if request.Source == nil {
		return nil, fmt.Errorf("input source is nil")
	}
	inputReader, err := request.Source.Open(ctx)
	if err != nil {
		return nil, fmt.Errorf("open input source: %w", err)
	}
	pcmReader, err := audio.NewFFmpegPCMReaderFromReader(ctx, h.ffmpegConfig, inputReader)
	if err != nil {
		_ = inputReader.Close()
		return nil, err
	}
	return audio.NewPCMStream(pcmReader, audio.PCMStreamOptions{VolumeProvider: sess.Volume})
}

func (h *Handler) destroySessionAfterPlayback(guildID snowflake.ID) {
	if h.sessions == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), voiceCleanupTimeout)
	defer cancel()
	if err := h.sessions.Destroy(ctx, guildID); err != nil && !errors.Is(err, session.ErrSessionNotFound) {
		slog.Warn("failed to destroy session after queue drained",
			slog.Any("err", err),
			slog.Uint64("guild_id", uint64(guildID)),
		)
	}
}

func buildQueueItem(source music.InputSource, actorUserID snowflake.ID, actorDisplayName string, textChannelID snowflake.ID) session.QueueItem {
	metadata := source.Metadata()
	item := session.QueueItem{
		URL:           metadata.OriginalURL,
		Title:         metadata.Title,
		RequestedBy:   session.RequestUser{ID: actorUserID, DisplayName: actorDisplayName},
		TextChannelID: textChannelID,
		TempFilePath:  source.TempFilePath(),
		Source:        source,
	}
	if item.URL == "" {
		item.URL = metadata.ResolvedURL
	}
	if item.Title == "" {
		item.Title = item.URL
	}
	switch metadata.SourceType {
	case music.SourceTypeDirect:
		item.SourceType = session.QueueSourceTypeDirect
	case music.SourceTypeYTDLP:
		item.SourceType = session.QueueSourceTypeYTDLP
	default:
		item.SourceType = session.QueueSourceTypeDirect
	}
	return item
}

func cleanupPlaybackRequest(sess *session.Session, request session.PlaybackRequest) {
	if request.TempFilePath != "" && sess != nil {
		if err := sess.RemoveTempFile(request.TempFilePath); err != nil {
			slog.Warn("failed to remove temp playback file",
				slog.Any("err", err),
				slog.Uint64("guild_id", uint64(sess.GuildID())),
				slog.String("temp_file_path", request.TempFilePath),
			)
		}
	}
	if request.Source != nil {
		if err := request.Source.Cleanup(); err != nil {
			slog.Warn("failed to clean up input source",
				slog.Any("err", err),
				slog.Uint64("guild_id", uint64(sess.GuildID())),
				slog.String("title", request.Title),
			)
		}
	}
}

func logMusicPlaybackError(sess *session.Session, request session.PlaybackRequest, err error) {
	slog.Error("failed to play queued track",
		slog.Any("err", err),
		slog.Uint64("guild_id", uint64(sess.GuildID())),
		slog.Uint64("text_channel_id", uint64(request.TextChannelID)),
		slog.Uint64("voice_channel_id", uint64(sess.VoiceChannelID())),
		slog.String("title", request.Title),
		slog.String("url", request.URL),
	)
}

func interactionActorDisplayName(event *events.ApplicationCommandInteractionCreate) string {
	if member := event.Member(); member != nil {
		if name := strings.TrimSpace(member.EffectiveName()); name != "" {
			return name
		}
	}
	if name := strings.TrimSpace(event.User().EffectiveName()); name != "" {
		return name
	}
	return "Unknown User"
}

func userMessageForResolveError(err error) string {
	switch {
	case errors.Is(err, music.ErrUnsupportedURL):
		return "対応していない URL です。直接音声ファイルリンクまたは yt-dlp 対応 URL を指定してください。"
	case errors.Is(err, music.ErrDirectFileTooLarge):
		return "直接音声ファイルは 100MB を超えるため再生できません。"
	default:
		return "URL の解決に失敗しました。少し待ってから再度お試しください。"
	}
}

func userMessageForEnqueueError(err error) string {
	switch {
	case errors.Is(err, session.ErrQueueFull):
		return "このサーバーの待機キューは満杯です。少し待ってから再度お試しください。"
	case errors.Is(err, session.ErrSessionClosed):
		return "再生セッションが閉じられました。もう一度 `/dplay` を実行してください。"
	default:
		return internalErrorMessage()
	}
}

func guildOnlyMessage() string {
	return "このコマンドはサーバー内でのみ使用できます。"
}

func noActiveSessionMessage() string {
	return "現在、このサーバーで接続中の再生セッションはありません。"
}

func internalErrorMessage() string {
	return "内部エラーが発生しました。少し待ってから再度お試しください。"
}

func postTextChannelMessage(client *disgobot.Client, channelID snowflake.ID, content string) error {
	if client == nil {
		return nil
	}
	_, err := client.Rest.CreateMessage(channelID, discord.NewMessageCreate().WithContent(truncateDiscordMessage(content)))
	return err
}

func (h *Handler) postQueuedMessage(client *disgobot.Client, channelID snowflake.ID, item session.QueueItem) error {
	return postTextChannelMessage(client, channelID, fmt.Sprintf("Queued: %s from %s", formatTrackLink(item), item.RequestedBy.DisplayName))
}

func formatQueueLine(item session.QueueItem) string {
	line := formatTrackLink(item)
	if item.RequestedBy.DisplayName != "" {
		line += " from " + item.RequestedBy.DisplayName
	}
	return line
}

func formatTrackLink(item session.QueueItem) string {
	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = strings.TrimSpace(item.URL)
	}
	if title == "" {
		title = "unknown"
	}
	if strings.TrimSpace(item.URL) == "" {
		return escapeMarkdownText(title)
	}
	return fmt.Sprintf("[%s](%s)", escapeMarkdownText(title), item.URL)
}

func escapeMarkdownText(text string) string {
	replacer := strings.NewReplacer(
		`\\`, `\\\\`,
		`[`, `\\[`,
		`]`, `\\]`,
	)
	return replacer.Replace(text)
}

func truncateDiscordMessage(content string) string {
	content = strings.TrimSpace(content)
	if len(content) <= 2000 {
		return content
	}
	return content[:1997] + "..."
}
