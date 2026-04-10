package audio

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/flac"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/wav"
)

// Decode opens an audio file and returns a seekable streamer.
// Supported formats: MP3, WAV, FLAC.
func Decode(path string) (beep.StreamSeekCloser, beep.Format, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, beep.Format{}, fmt.Errorf("open: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mp3":
		return decodeMP3(f)
	case ".wav":
		return decodeWAV(f)
	case ".flac":
		return decodeFLAC(f)
	default:
		f.Close()
		return nil, beep.Format{}, fmt.Errorf("unsupported format: %s", ext)
	}
}

func decodeMP3(r io.ReadSeekCloser) (beep.StreamSeekCloser, beep.Format, error) {
	streamer, format, err := mp3.Decode(r)
	if err != nil {
		r.Close()
		return nil, beep.Format{}, fmt.Errorf("mp3 decode: %w", err)
	}
	return streamer, format, nil
}

func decodeWAV(r io.ReadSeekCloser) (beep.StreamSeekCloser, beep.Format, error) {
	streamer, format, err := wav.Decode(r)
	if err != nil {
		r.Close()
		return nil, beep.Format{}, fmt.Errorf("wav decode: %w", err)
	}
	return streamer, format, nil
}

func decodeFLAC(r io.ReadSeekCloser) (beep.StreamSeekCloser, beep.Format, error) {
	streamer, format, err := flac.Decode(r)
	if err != nil {
		r.Close()
		return nil, beep.Format{}, fmt.Errorf("flac decode: %w", err)
	}
	return streamer, format, nil
}
