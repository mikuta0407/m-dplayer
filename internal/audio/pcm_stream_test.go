package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPCMStreamReadPCMFrame(t *testing.T) {
	data := buildPCMBytes(t, repeatedSamples(1234, pcmFrameSamples)...)
	stream, err := NewPCMStream(io.NopCloser(bytes.NewReader(data)), PCMStreamOptions{})
	if err != nil {
		t.Fatalf("NewPCMStream() error = %v", err)
	}
	defer stream.Close()

	frame, err := stream.readPCMFrame()
	if err != nil {
		t.Fatalf("readPCMFrame() error = %v", err)
	}
	if len(frame) != pcmFrameSamples {
		t.Fatalf("len(readPCMFrame()) = %d, want %d", len(frame), pcmFrameSamples)
	}
	for i, sample := range frame {
		if sample != 1234 {
			t.Fatalf("frame[%d] = %d, want %d", i, sample, 1234)
		}
	}
}

func TestPCMStreamReadPCMFramePadsPartialFinalFrame(t *testing.T) {
	data := buildPCMBytes(t, 100, -100, 200, -200)
	stream, err := NewPCMStream(io.NopCloser(bytes.NewReader(data)), PCMStreamOptions{})
	if err != nil {
		t.Fatalf("NewPCMStream() error = %v", err)
	}
	defer stream.Close()

	frame, err := stream.readPCMFrame()
	if err != nil {
		t.Fatalf("readPCMFrame() error = %v", err)
	}
	if frame[0] != 100 || frame[1] != -100 || frame[2] != 200 || frame[3] != -200 {
		t.Fatalf("unexpected leading samples = %v", frame[:4])
	}
	for i := 4; i < len(frame); i++ {
		if frame[i] != 0 {
			t.Fatalf("frame[%d] = %d, want 0 after EOF padding", i, frame[i])
		}
	}
}

func TestPCMStreamReadPCMFrameReflectsDynamicVolume(t *testing.T) {
	data := buildPCMBytes(
		t,
		repeatedSamples(1000, pcmFrameSamples)...,
	)
	data = append(data, buildPCMBytes(t, repeatedSamples(1000, pcmFrameSamples)...)...)

	volume := 10
	stream, err := NewPCMStream(io.NopCloser(bytes.NewReader(data)), PCMStreamOptions{
		VolumeProvider: func() int { return volume },
	})
	if err != nil {
		t.Fatalf("NewPCMStream() error = %v", err)
	}
	defer stream.Close()

	firstFrame, err := stream.readPCMFrame()
	if err != nil {
		t.Fatalf("readPCMFrame(first) error = %v", err)
	}
	if firstFrame[0] != 1000 {
		t.Fatalf("first frame sample = %d, want %d", firstFrame[0], 1000)
	}

	volume = 5
	secondFrame, err := stream.readPCMFrame()
	if err != nil {
		t.Fatalf("readPCMFrame(second) error = %v", err)
	}
	if secondFrame[0] != 500 {
		t.Fatalf("second frame sample = %d, want %d", secondFrame[0], 500)
	}
}

func TestPCMStreamNextOpusFrameReturnsEOFOnEmptyReader(t *testing.T) {
	stream, err := NewPCMStream(io.NopCloser(bytes.NewReader(nil)), PCMStreamOptions{})
	if err != nil {
		t.Fatalf("NewPCMStream() error = %v", err)
	}
	defer stream.Close()

	_, err = stream.NextOpusFrame()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("NextOpusFrame() error = %v, want %v", err, io.EOF)
	}
}

func TestPCMStreamNextOpusFrameEncodesPCM(t *testing.T) {
	data := buildPCMBytes(t, repeatedSamples(2000, pcmFrameSamples)...)
	stream, err := NewPCMStream(io.NopCloser(bytes.NewReader(data)), PCMStreamOptions{})
	if err != nil {
		t.Fatalf("NewPCMStream() error = %v", err)
	}
	defer stream.Close()

	frame, err := stream.NextOpusFrame()
	if err != nil {
		t.Fatalf("NextOpusFrame() error = %v", err)
	}
	if len(frame) == 0 {
		t.Fatal("NextOpusFrame() returned empty opus frame")
	}
}

func TestNewFFmpegPCMReaderFromFile(t *testing.T) {
	tempDir := t.TempDir()
	ffmpegPath := filepath.Join(tempDir, "ffmpeg")
	writeExecutable(t, ffmpegPath, ffmpegStubScript())

	inputPath := filepath.Join(tempDir, "input.raw")
	want := buildPCMBytes(t, 1, 2, 3, 4, 5, 6)
	if err := os.WriteFile(inputPath, want, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	reader, err := NewFFmpegPCMReaderFromFile(context.Background(), FFmpegConfig{CommandPath: ffmpegPath}, inputPath)
	if err != nil {
		t.Fatalf("NewFFmpegPCMReaderFromFile() error = %v", err)
	}
	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("ffmpeg file output = %v, want %v", got, want)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("reader.Close() error = %v", err)
	}
}

func TestNewFFmpegPCMReaderFromReader(t *testing.T) {
	tempDir := t.TempDir()
	ffmpegPath := filepath.Join(tempDir, "ffmpeg")
	writeExecutable(t, ffmpegPath, ffmpegStubScript())

	want := buildPCMBytes(t, 11, 22, 33, 44)
	reader, err := NewFFmpegPCMReaderFromReader(
		context.Background(),
		FFmpegConfig{CommandPath: ffmpegPath},
		io.NopCloser(bytes.NewReader(want)),
	)
	if err != nil {
		t.Fatalf("NewFFmpegPCMReaderFromReader() error = %v", err)
	}
	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("ffmpeg reader output = %v, want %v", got, want)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("reader.Close() error = %v", err)
	}
}

func TestFFmpegPCMReaderStopsWhenContextCanceled(t *testing.T) {
	tempDir := t.TempDir()
	ffmpegPath := filepath.Join(tempDir, "ffmpeg")
	writeExecutable(t, ffmpegPath, `#!/bin/sh
while :; do
  sleep 1
 done
`)

	inputPath := filepath.Join(tempDir, "input.raw")
	if err := os.WriteFile(inputPath, nil, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	reader, err := NewFFmpegPCMReaderFromFile(ctx, FFmpegConfig{CommandPath: ffmpegPath}, inputPath)
	if err != nil {
		t.Fatalf("NewFFmpegPCMReaderFromFile() error = %v", err)
	}

	cancel()
	closed := make(chan error, 1)
	go func() {
		closed <- reader.Close()
	}()

	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("reader.Close() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reader.Close() did not return after context cancellation")
	}
}

func buildPCMBytes(t *testing.T, samples ...int16) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	for _, sample := range samples {
		if err := binary.Write(buf, binary.LittleEndian, sample); err != nil {
			t.Fatalf("binary.Write() error = %v", err)
		}
	}
	return buf.Bytes()
}

func repeatedSamples(sample int16, count int) []int16 {
	out := make([]int16, count)
	for i := range out {
		out[i] = sample
	}
	return out
}

func writeExecutable(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

func ffmpegStubScript() string {
	return `#!/bin/sh
input=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -i)
      input="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done
if [ "$input" = "pipe:0" ]; then
  cat
  exit $?
fi
cat "$input"
`
}
