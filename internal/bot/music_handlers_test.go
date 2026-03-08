package appbot

import (
	"strings"
	"testing"

	"github.com/mikuta0407/m-dplayer/internal/session"
)

func TestQueueSnapshotMessageIncludesCurrentAndQueuedTracks(t *testing.T) {
	handler := &Handler{sessions: session.NewManager()}
	sess, err := handler.sessions.Create(session.CreateParams{
		GuildID:        1,
		TextChannelID:  10,
		VoiceChannelID: 20,
		Volume:         7,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	current := session.QueueItem{
		Title:       "Current Song",
		URL:         "https://example.com/current",
		RequestedBy: session.RequestUser{DisplayName: "Alice"},
	}
	if err := sess.StartCurrent(current, func() {}); err != nil {
		t.Fatalf("StartCurrent() error = %v", err)
	}
	defer sess.FinishCurrent()

	queued := session.QueueItem{
		Title:       "Next Song",
		URL:         "https://example.com/next",
		RequestedBy: session.RequestUser{DisplayName: "Bob"},
	}
	if err := sess.Enqueue(queued); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	got := handler.queueSnapshotMessage(1)
	for _, want := range []string{
		"音量: 7",
		"再生中:",
		"[Current Song](https://example.com/current) from Alice",
		"待機キュー:",
		"1. [Next Song](https://example.com/next) from Bob",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("queueSnapshotMessage() = %q, want substring %q", got, want)
		}
	}
}

func TestProcessDVolUpdatesSessionVolume(t *testing.T) {
	handler := &Handler{sessions: session.NewManager()}
	sess, err := handler.sessions.Create(session.CreateParams{
		GuildID:        1,
		TextChannelID:  10,
		VoiceChannelID: 20,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got := handler.processDVol(nil, 1, 10, "Alice", 5)
	if got != "音量を 5 に設定しました。" {
		t.Fatalf("processDVol() = %q, want %q", got, "音量を 5 に設定しました。")
	}
	if sess.Volume() != 5 {
		t.Fatalf("session volume = %d, want 5", sess.Volume())
	}
}

func TestProcessDStopLikeCancelsCurrentPlayback(t *testing.T) {
	handler := &Handler{sessions: session.NewManager()}
	sess, err := handler.sessions.Create(session.CreateParams{
		GuildID:        1,
		TextChannelID:  10,
		VoiceChannelID: 20,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	cancelled := false
	current := session.QueueItem{
		Title:       "Current Song",
		URL:         "https://example.com/current",
		RequestedBy: session.RequestUser{DisplayName: "Alice"},
	}
	if err := sess.StartCurrent(current, func() { cancelled = true }); err != nil {
		t.Fatalf("StartCurrent() error = %v", err)
	}

	got := handler.processDStopLike(nil, 1, 10, "Bob", dstopCommandName)
	if got != "現在再生中の曲を停止しました。" {
		t.Fatalf("processDStopLike() = %q, want %q", got, "現在再生中の曲を停止しました。")
	}
	if !cancelled {
		t.Fatal("current playback cancel func was not called")
	}
}

func TestProcessDTermClearsQueueAndDestroysSession(t *testing.T) {
	handler := &Handler{sessions: session.NewManager()}
	conn := &stubVoiceConn{udpConn: &stubVoiceUDPConn{}}
	sess, err := handler.sessions.Create(session.CreateParams{
		GuildID:        1,
		TextChannelID:  10,
		VoiceChannelID: 20,
		Conn:           conn,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := sess.Enqueue(session.QueueItem{Title: "Queued Song", URL: "https://example.com/queued"}); err != nil {
			t.Fatalf("Enqueue() error = %v", err)
		}
	}
	if err := sess.StartCurrent(session.QueueItem{Title: "Current Song", URL: "https://example.com/current"}, func() {}); err != nil {
		t.Fatalf("StartCurrent() error = %v", err)
	}

	got := handler.processDTerm(nil, 1, 10, "Alice")
	if got != "再生を終了し、待機キュー 2 件を破棄しました。" {
		t.Fatalf("processDTerm() = %q, want %q", got, "再生を終了し、待機キュー 2 件を破棄しました。")
	}
	if handler.sessions.Exists(1) {
		t.Fatal("session should be removed after dterm")
	}
	if conn.closeCalls != 1 {
		t.Fatalf("closeCalls = %d, want 1", conn.closeCalls)
	}
}
