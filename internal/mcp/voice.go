package mcp

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp-local/internal/platform"
)

// The programs a voice note is made with, looked up on the PATH. Tests point
// them elsewhere.
var ffmpegBin, ffprobeBin = "ffmpeg", "ffprobe"

// voiceMIME is the only format WhatsApp plays as a voice note.
const voiceMIME = "audio/ogg; codecs=opus"

const voiceTimeout = 2 * time.Minute

// toVoiceNote converts an audio file to what WhatsApp plays as a voice note:
// Ogg Opus, mono, 48 kHz. The new file lives in a temporary folder that
// cleanup removes. wacli's send voice then marks it as one (ptt), with its
// length in seconds and a 64-sample waveform it measures with ffprobe and
// ffmpeg.
func toVoiceNote(ctx context.Context, src string) (string, func(), error) {
	nothing := func() {}
	bin, err := exec.LookPath(ffmpegBin)
	if err != nil {
		return "", nothing, errors.New("ffmpeg is not installed")
	}
	dir, err := os.MkdirTemp("", "whatsapp-mcp-voice-")
	if err != nil {
		return "", nothing, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	out := filepath.Join(dir, "voice.ogg")
	ctx, cancel := context.WithTimeout(ctx, voiceTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-nostdin", "-hide_banner", "-loglevel", "error", "-y",
		"-i", src, "-vn", "-sn", "-dn", "-map_metadata", "-1",
		"-ac", "1", "-ar", "48000", "-c:a", "libopus", "-b:a", "32k", "-application", "voip",
		"-f", "ogg", out)
	platform.Background(cmd)
	if b, err := cmd.CombinedOutput(); err != nil {
		cleanup()
		if msg := lastLine(string(b)); msg != "" {
			return "", nothing, fmt.Errorf("ffmpeg could not convert the audio: %s", msg)
		}
		return "", nothing, fmt.Errorf("ffmpeg could not convert the audio: %v", err)
	}
	if info, err := os.Stat(out); err != nil || info.Size() == 0 {
		cleanup()
		return "", nothing, errors.New("ffmpeg produced no audio")
	}
	return out, cleanup, nil
}

// voiceSeconds is the length of a voice note, rounded up as WhatsApp shows
// it, or 0 when ffprobe cannot tell (wacli then sends it without a length).
func voiceSeconds(ctx context.Context, path string) uint32 {
	bin, err := exec.LookPath(ffprobeBin)
	if err != nil {
		return 0
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-v", "error", "-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1", path)
	platform.Background(cmd)
	b, err := cmd.Output()
	if err != nil {
		return 0
	}
	s, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
	if err != nil || s <= 0 {
		return 0
	}
	return uint32(math.Ceil(s))
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// wantsVoice tells whether a send goes out as a voice note: audio does by
// default, voice false keeps it a file, and channels take no voice notes.
func wantsVoice(as string, voice *bool, to string) bool {
	if as != "audio" || strings.HasSuffix(strings.TrimSpace(to), "@newsletter") {
		return false
	}
	return voice == nil || *voice
}
