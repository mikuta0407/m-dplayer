package session

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestSessionEnqueueAndSnapshotQueue(t *testing.T) {
	session := New(CreateParams{QueueCapacity: 3})

	first := QueueItem{URL: "https://example.com/1", Title: "track-1", SourceType: QueueSourceTypeDirect}
	second := QueueItem{URL: "https://example.com/2", Title: "track-2", SourceType: QueueSourceTypeYTDLP}

	if err := session.Enqueue(first); err != nil {
		t.Fatalf("Enqueue(first) error = %v", err)
	}
	if err := session.Enqueue(second); err != nil {
		t.Fatalf("Enqueue(second) error = %v", err)
	}

	queue := session.SnapshotQueue()
	if len(queue) != 2 {
		t.Fatalf("len(SnapshotQueue()) = %d, want 2", len(queue))
	}
	if queue[0] != first {
		t.Fatalf("SnapshotQueue()[0] = %+v, want %+v", queue[0], first)
	}
	if queue[1] != second {
		t.Fatalf("SnapshotQueue()[1] = %+v, want %+v", queue[1], second)
	}

	queue[0].Title = "modified"
	current := session.SnapshotQueue()
	if current[0].Title != first.Title {
		t.Fatalf("SnapshotQueue() should return a copy, got title %q, want %q", current[0].Title, first.Title)
	}
}

func TestSessionWaitDequeueReturnsQueuedItem(t *testing.T) {
	session := New(CreateParams{QueueCapacity: 2})
	want := QueueItem{Title: "track-1", URL: "https://example.com/1"}

	if err := session.Enqueue(want); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	got, err := session.WaitDequeue()
	if err != nil {
		t.Fatalf("WaitDequeue() error = %v", err)
	}
	if got != want {
		t.Fatalf("WaitDequeue() = %+v, want %+v", got, want)
	}
	if session.QueueLen() != 0 {
		t.Fatalf("QueueLen() = %d, want 0", session.QueueLen())
	}
	if !session.ShouldAutoDisconnect() {
		t.Fatal("ShouldAutoDisconnect() = false, want true")
	}
}

func TestSessionWaitDequeueReturnsErrSessionClosedWhenClosed(t *testing.T) {
	session := New(CreateParams{})

	resultCh := make(chan error, 1)
	go func() {
		_, err := session.WaitDequeue()
		resultCh <- err
	}()

	time.Sleep(10 * time.Millisecond)
	session.Close(context.Background())

	select {
	case err := <-resultCh:
		if err != ErrSessionClosed {
			t.Fatalf("WaitDequeue() error = %v, want %v", err, ErrSessionClosed)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("WaitDequeue() did not unblock after Close()")
	}
}

func TestSessionClearQueueRemovesQueuedTempFiles(t *testing.T) {
	tempDir := t.TempDir()
	file1 := filepath.Join(tempDir, "voice-1.wav")
	file2 := filepath.Join(tempDir, "voice-2.wav")
	if err := os.WriteFile(file1, []byte("voice-1"), 0o600); err != nil {
		t.Fatalf("WriteFile(file1) error = %v", err)
	}
	if err := os.WriteFile(file2, []byte("voice-2"), 0o600); err != nil {
		t.Fatalf("WriteFile(file2) error = %v", err)
	}

	session := New(CreateParams{})
	session.TrackTempFile(file1)
	session.TrackTempFile(file2)

	if err := session.Enqueue(QueueItem{Title: "track-1", TempFilePath: file1}); err != nil {
		t.Fatalf("Enqueue(track-1) error = %v", err)
	}
	if err := session.Enqueue(QueueItem{Title: "track-2", TempFilePath: file2}); err != nil {
		t.Fatalf("Enqueue(track-2) error = %v", err)
	}

	cleared := session.ClearQueue()
	if len(cleared) != 2 {
		t.Fatalf("len(ClearQueue()) = %d, want 2", len(cleared))
	}
	if session.QueueLen() != 0 {
		t.Fatalf("QueueLen() = %d, want 0", session.QueueLen())
	}
	if session.TempFileCount() != 0 {
		t.Fatalf("TempFileCount() = %d, want 0", session.TempFileCount())
	}
	if _, err := os.Stat(file1); !os.IsNotExist(err) {
		t.Fatalf("file1 should be removed, stat err = %v", err)
	}
	if _, err := os.Stat(file2); !os.IsNotExist(err) {
		t.Fatalf("file2 should be removed, stat err = %v", err)
	}
	if !session.ShouldAutoDisconnect() {
		t.Fatal("ShouldAutoDisconnect() = false, want true")
	}
}

func TestSessionSkipCurrentCancelsPlaybackContext(t *testing.T) {
	session := New(CreateParams{})
	ctx, cancel := context.WithCancel(context.Background())

	if err := session.StartCurrent(QueueItem{Title: "track-1"}, cancel); err != nil {
		t.Fatalf("StartCurrent() error = %v", err)
	}
	if _, ok := session.Current(); !ok {
		t.Fatal("Current() = false, want active item")
	}
	if session.ShouldAutoDisconnect() {
		t.Fatal("ShouldAutoDisconnect() = true, want false while current playback is active")
	}

	if !session.SkipCurrent() {
		t.Fatal("SkipCurrent() = false, want true")
	}

	select {
	case <-ctx.Done():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("current playback context was not canceled")
	}

	session.FinishCurrent()
	if _, ok := session.Current(); ok {
		t.Fatal("Current() should be cleared after FinishCurrent()")
	}
	if !session.ShouldAutoDisconnect() {
		t.Fatal("ShouldAutoDisconnect() = false, want true")
	}
}

func TestSessionSetVolume(t *testing.T) {
	session := New(CreateParams{})
	if session.Volume() != DefaultVolume {
		t.Fatalf("Volume() = %d, want %d", session.Volume(), DefaultVolume)
	}

	if err := session.SetVolume(3); err != nil {
		t.Fatalf("SetVolume() error = %v", err)
	}
	if session.Volume() != 3 {
		t.Fatalf("Volume() = %d, want 3", session.Volume())
	}

	if err := session.SetVolume(MaxVolume + 1); err != ErrInvalidVolume {
		t.Fatalf("SetVolume() error = %v, want %v", err, ErrInvalidVolume)
	}
	if session.Volume() != 3 {
		t.Fatalf("Volume() after invalid update = %d, want 3", session.Volume())
	}
}

func TestSessionCloseRemovesTrackedTempFiles(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "voice.wav")
	if err := os.WriteFile(filePath, []byte("voice"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	session := New(CreateParams{})
	session.TrackTempFile(filePath)

	session.Close(context.Background())

	if session.TempFileCount() != 0 {
		t.Fatalf("TempFileCount() = %d, want 0", session.TempFileCount())
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("temp file should be removed, stat err = %v", err)
	}
}

func TestSessionRemoveTempFile(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "voice.wav")
	if err := os.WriteFile(filePath, []byte("voice"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	session := New(CreateParams{})
	session.TrackTempFile(filePath)

	if err := session.RemoveTempFile(filePath); err != nil {
		t.Fatalf("RemoveTempFile() error = %v", err)
	}
	if session.TempFileCount() != 0 {
		t.Fatalf("TempFileCount() = %d, want 0", session.TempFileCount())
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("temp file should be removed, stat err = %v", err)
	}
}

func TestSessionCloseCallsBeforeCloseHook(t *testing.T) {
	var calls atomic.Int32
	session := New(CreateParams{
		BeforeClose: func() {
			calls.Add(1)
		},
	})

	session.Close(context.Background())
	session.Close(context.Background())

	if calls.Load() != 1 {
		t.Fatalf("beforeClose calls = %d, want 1", calls.Load())
	}
}
