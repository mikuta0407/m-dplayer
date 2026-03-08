package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/snowflake/v2"
)

const (
	DefaultQueueCapacity = 32
	DefaultVolume        = 10
	MinVolume            = 0
	MaxVolume            = 10
)

var (
	ErrSessionClosed           = errors.New("session is closed")
	ErrQueueFull               = errors.New("session queue is full")
	ErrInvalidVolume           = errors.New("invalid session volume")
	ErrCurrentPlaybackInFlight = errors.New("current playback is already in flight")
)

type QueueSourceType string

const (
	QueueSourceTypeDirect QueueSourceType = "direct"
	QueueSourceTypeYTDLP  QueueSourceType = "yt-dlp"
	QueueSourceTypeTTS    QueueSourceType = "tts"
)

type RequestUser struct {
	ID          snowflake.ID
	DisplayName string
}

type QueueItem struct {
	URL           string
	Title         string
	SourceType    QueueSourceType
	RequestedBy   RequestUser
	TextChannelID snowflake.ID
	TempFilePath  string

	// Content is kept temporarily so the existing TTS pipeline can continue to
	// log normalized text while the migration to music playback is in progress.
	Content string
}

type PlaybackRequest = QueueItem

type CreateParams struct {
	GuildID        snowflake.ID
	TextChannelID  snowflake.ID
	VoiceChannelID snowflake.ID
	Conn           voice.Conn
	BeforeClose    func()
	QueueCapacity  int
	Volume         int
}

type Session struct {
	guildID        snowflake.ID
	textChannelID  snowflake.ID
	voiceChannelID snowflake.ID
	conn           voice.Conn
	queue          []QueueItem
	queueCapacity  int
	current        *QueueItem
	currentCancel  context.CancelFunc
	volume         int
	ctx            context.Context
	cancel         context.CancelFunc

	sendMu       sync.Mutex
	mu           sync.Mutex
	queueCond    *sync.Cond
	closed       bool
	idleSpeaking bool
	closeOnce    sync.Once
	beforeClose  func()
	onClose      func()
	tempFiles    map[string]struct{}
}

func New(params CreateParams) *Session {
	queueCapacity := params.QueueCapacity
	if queueCapacity <= 0 {
		queueCapacity = DefaultQueueCapacity
	}

	volume := params.Volume
	if volume == 0 {
		volume = DefaultVolume
	}
	if volume < MinVolume || volume > MaxVolume {
		volume = DefaultVolume
	}

	ctx, cancel := context.WithCancel(context.Background())

	session := &Session{
		guildID:        params.GuildID,
		textChannelID:  params.TextChannelID,
		voiceChannelID: params.VoiceChannelID,
		conn:           params.Conn,
		queueCapacity:  queueCapacity,
		volume:         volume,
		ctx:            ctx,
		cancel:         cancel,
		beforeClose:    params.BeforeClose,
		tempFiles:      make(map[string]struct{}),
	}
	session.queueCond = sync.NewCond(&session.mu)
	return session
}

func (s *Session) GuildID() snowflake.ID {
	return s.guildID
}

func (s *Session) TextChannelID() snowflake.ID {
	return s.textChannelID
}

func (s *Session) VoiceChannelID() snowflake.ID {
	return s.voiceChannelID
}

func (s *Session) Conn() voice.Conn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn
}

func (s *Session) Context() context.Context {
	return s.ctx
}

func (s *Session) QueueLen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queue)
}

func (s *Session) QueueCap() int {
	return s.queueCapacity
}

func (s *Session) Closed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *Session) Volume() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.volume
}

func (s *Session) SetVolume(volume int) error {
	if volume < MinVolume || volume > MaxVolume {
		return ErrInvalidVolume
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrSessionClosed
	}

	s.volume = volume
	return nil
}

func (s *Session) Enqueue(request PlaybackRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.ctx.Err() != nil {
		return ErrSessionClosed
	}
	if len(s.queue) >= s.queueCapacity {
		return ErrQueueFull
	}

	s.queue = append(s.queue, request)
	s.queueCond.Signal()
	return nil
}

func (s *Session) WaitDequeue() (QueueItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for len(s.queue) == 0 && !s.closed {
		s.queueCond.Wait()
	}
	if s.closed || s.ctx.Err() != nil {
		return QueueItem{}, ErrSessionClosed
	}

	item := s.queue[0]
	copy(s.queue, s.queue[1:])
	lastIndex := len(s.queue) - 1
	s.queue[lastIndex] = QueueItem{}
	s.queue = s.queue[:lastIndex]
	return item, nil
}

func (s *Session) SnapshotQueue() []QueueItem {
	s.mu.Lock()
	defer s.mu.Unlock()

	queue := make([]QueueItem, len(s.queue))
	copy(queue, s.queue)
	return queue
}

func (s *Session) Current() (QueueItem, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.current == nil {
		return QueueItem{}, false
	}
	return *s.current, true
}

func (s *Session) StartCurrent(item QueueItem, cancel context.CancelFunc) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.ctx.Err() != nil {
		if cancel != nil {
			cancel()
		}
		return ErrSessionClosed
	}
	if s.current != nil {
		if cancel != nil {
			cancel()
		}
		return ErrCurrentPlaybackInFlight
	}

	itemCopy := item
	s.current = &itemCopy
	s.currentCancel = cancel
	return nil
}

func (s *Session) FinishCurrent() {
	s.mu.Lock()
	cancel := s.currentCancel
	s.current = nil
	s.currentCancel = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

func (s *Session) SkipCurrent() bool {
	s.mu.Lock()
	hasCurrent := s.current != nil
	cancel := s.currentCancel
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	return hasCurrent
}

func (s *Session) ClearQueue() []QueueItem {
	s.mu.Lock()
	cleared := make([]QueueItem, len(s.queue))
	copy(cleared, s.queue)
	s.queue = nil
	paths := s.removeTrackedQueueFilesLocked(cleared)
	s.mu.Unlock()

	for _, path := range paths {
		_ = removeTrackedFile(path)
	}

	return cleared
}

func (s *Session) ShouldAutoDisconnect() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed && s.current == nil && len(s.queue) == 0
}

func (s *Session) Close(ctx context.Context) {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.idleSpeaking = false
		s.queue = nil
		cancel := s.cancel
		currentCancel := s.currentCancel
		s.current = nil
		s.currentCancel = nil
		conn := s.conn
		s.conn = nil
		beforeClose := s.beforeClose
		onClose := s.onClose
		tempFiles := make([]string, 0, len(s.tempFiles))
		for path := range s.tempFiles {
			tempFiles = append(tempFiles, path)
		}
		s.tempFiles = make(map[string]struct{})
		s.queueCond.Broadcast()
		s.mu.Unlock()

		if currentCancel != nil {
			currentCancel()
		}
		if cancel != nil {
			cancel()
		}

		if ctx == nil {
			ctx = context.Background()
		}
		if beforeClose != nil {
			beforeClose()
		}
		s.sendMu.Lock()
		if err := closeVoiceConn(ctx, conn); err != nil {
			slog.Warn("failed to close voice connection",
				slog.Any("err", err),
				slog.Uint64("guild_id", uint64(s.guildID)),
				slog.Uint64("voice_channel_id", uint64(s.voiceChannelID)),
			)
		}
		s.sendMu.Unlock()
		for _, path := range tempFiles {
			_ = removeTrackedFile(path)
		}

		if onClose != nil {
			onClose()
		}
	})
}

func (s *Session) WithConnExclusive(action func(conn voice.Conn) error) error {
	if action == nil {
		return nil
	}

	s.sendMu.Lock()
	defer s.sendMu.Unlock()

	if s.ctx.Err() != nil {
		return ErrSessionClosed
	}

	conn := s.Conn()
	if conn == nil {
		return ErrSessionClosed
	}

	return action(conn)
}

func (s *Session) IdleSpeakingActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.idleSpeaking
}

func (s *Session) SetIdleSpeakingActive(active bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		s.idleSpeaking = false
		return
	}
	s.idleSpeaking = active
}

func closeVoiceConn(ctx context.Context, conn voice.Conn) (err error) {
	if conn == nil {
		return nil
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic while closing voice connection: %v", recovered)
		}
	}()

	conn.Close(ctx)
	return nil
}

func (s *Session) TrackTempFile(path string) {
	if path == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.tempFiles[path] = struct{}{}
}

func (s *Session) RemoveTempFile(path string) error {
	if path == "" {
		return nil
	}

	s.mu.Lock()
	delete(s.tempFiles, path)
	s.mu.Unlock()

	return removeTrackedFile(path)
}

func (s *Session) TempFileCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.tempFiles)
}

func (s *Session) setOnClose(onClose func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onClose = onClose
}

func (s *Session) removeTrackedQueueFilesLocked(items []QueueItem) []string {
	paths := make([]string, 0, len(items))
	for _, item := range items {
		if item.TempFilePath == "" {
			continue
		}
		if _, tracked := s.tempFiles[item.TempFilePath]; !tracked {
			continue
		}
		delete(s.tempFiles, item.TempFilePath)
		paths = append(paths, item.TempFilePath)
	}
	return paths
}

func removeTrackedFile(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
