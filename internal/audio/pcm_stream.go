package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"

	"github.com/disgoorg/disgo/voice"
	"github.com/kazzmir/opus-go/opus"
)

const (
	DefaultVolumeBase = 10
	pcmFrameSamples   = voice.OpusFrameSize * AudioChannels
	pcmFrameBytes     = pcmFrameSamples * BytesPerSample
)

type PCMStreamOptions struct {
	VolumeProvider func() int
	VolumeBase     int
}

type PCMStream struct {
	reader         io.ReadCloser
	encoder        *opus.Encoder
	opusBuf        []byte
	pcmBytes       []byte
	pcmSamples     []int16
	volumeProvider func() int
	volumeBase     int
}

func NewPCMStream(reader io.ReadCloser, options PCMStreamOptions) (*PCMStream, error) {
	if reader == nil {
		return nil, fmt.Errorf("pcm reader is nil")
	}

	encoder, err := opus.NewEncoder(DiscordSampleRate, AudioChannels, opus.ApplicationAudio)
	if err != nil {
		return nil, err
	}

	volumeBase := options.VolumeBase
	if volumeBase <= 0 {
		volumeBase = DefaultVolumeBase
	}

	return &PCMStream{
		reader:         reader,
		encoder:        encoder,
		opusBuf:        make([]byte, MaxOpusPacketSize),
		pcmBytes:       make([]byte, pcmFrameBytes),
		pcmSamples:     make([]int16, pcmFrameSamples),
		volumeProvider: options.VolumeProvider,
		volumeBase:     volumeBase,
	}, nil
}

func (s *PCMStream) Close() error {
	var firstErr error
	if s.reader != nil {
		if err := s.reader.Close(); err != nil {
			firstErr = err
		}
		s.reader = nil
	}
	if s.encoder != nil {
		if err := s.encoder.Close(); firstErr == nil && err != nil {
			firstErr = err
		}
		s.encoder = nil
	}
	return firstErr
}

func (s *PCMStream) NextOpusFrame() ([]byte, error) {
	pcm, err := s.readPCMFrame()
	if err != nil {
		return nil, err
	}

	n, err := s.encoder.Encode(pcm, voice.OpusFrameSize, s.opusBuf)
	if err != nil {
		return nil, err
	}
	return s.opusBuf[:n], nil
}

func (s *PCMStream) readPCMFrame() ([]int16, error) {
	if s.reader == nil {
		return nil, io.EOF
	}

	for i := range s.pcmBytes {
		s.pcmBytes[i] = 0
	}

	n, err := io.ReadFull(s.reader, s.pcmBytes)
	switch {
	case err == nil:
	case err == io.EOF && n == 0:
		return nil, io.EOF
	case err == io.EOF || err == io.ErrUnexpectedEOF:
		if n == 0 {
			return nil, io.EOF
		}
	default:
		return nil, err
	}

	scale := s.currentVolumeScale()
	for i := 0; i < pcmFrameSamples; i++ {
		offset := i * BytesPerSample
		sample := int16(binary.LittleEndian.Uint16(s.pcmBytes[offset : offset+BytesPerSample]))
		s.pcmSamples[i] = scalePCM16(sample, scale)
	}
	return s.pcmSamples, nil
}

func (s *PCMStream) currentVolumeScale() float64 {
	if s.volumeProvider == nil {
		return 1
	}

	level := s.volumeProvider()
	if level < 0 {
		level = 0
	}
	if level > s.volumeBase {
		level = s.volumeBase
	}
	return float64(level) / float64(s.volumeBase)
}

func scalePCM16(sample int16, scale float64) int16 {
	if scale <= 0 {
		return 0
	}
	if scale == 1 {
		return sample
	}

	value := math.Round(float64(sample) * scale)
	if value > math.MaxInt16 {
		value = math.MaxInt16
	}
	if value < math.MinInt16 {
		value = math.MinInt16
	}
	return int16(value)
}
