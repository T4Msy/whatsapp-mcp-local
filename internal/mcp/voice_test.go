package mcp

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWantsVoice(t *testing.T) {
	yes, no := true, false
	for _, c := range []struct {
		as    string
		voice *bool
		to    string
		want  bool
	}{
		{"audio", nil, "5511900000001@s.whatsapp.net", true},
		{"audio", &yes, "5511900000001", true},
		{"audio", &no, "5511900000001", false},
		{"audio", nil, "120363000000000001@newsletter", false},
		{"document", nil, "5511900000001", false},
		{"video", &yes, "5511900000001", false},
	} {
		if got := wantsVoice(c.as, c.voice, c.to); got != c.want {
			t.Errorf("wantsVoice(%q, %v, %q) = %v, want %v", c.as, c.voice, c.to, got, c.want)
		}
	}
}

func TestToVoiceNoteWithoutFFmpeg(t *testing.T) {
	old := ffmpegBin
	ffmpegBin = "no-such-ffmpeg-for-whatsapp-mcp"
	defer func() { ffmpegBin = old }()
	if _, _, err := toVoiceNote(context.Background(), "x.mp3"); err == nil || !strings.Contains(err.Error(), "ffmpeg is not installed") {
		t.Fatalf("want ffmpeg missing, got %v", err)
	}
}

func TestToVoiceNoteConverts(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "tone.mp3")
	if out, err := exec.Command(ffmpeg, "-nostdin", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=2.3",
		"-ac", "2", "-ar", "44100", src).CombinedOutput(); err != nil {
		t.Skipf("ffmpeg cannot make a test tone: %v %s", err, out)
	}
	ogg, cleanup, err := toVoiceNote(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(ogg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(b, []byte("OggS")) || !bytes.Contains(b, []byte("OpusHead")) {
		t.Fatal("not an Ogg Opus file")
	}
	// OpusHead: version, channel count (byte 9), pre-skip, input sample rate (bytes 12-15, little endian).
	head := b[bytes.Index(b, []byte("OpusHead")):]
	if head[9] != 1 {
		t.Errorf("channels = %d, want mono", head[9])
	}
	if rate := uint32(head[12]) | uint32(head[13])<<8 | uint32(head[14])<<16 | uint32(head[15])<<24; rate != 48000 {
		t.Errorf("sample rate = %d, want 48000", rate)
	}
	if _, err := exec.LookPath("ffprobe"); err == nil {
		if s := voiceSeconds(context.Background(), ogg); s != 3 {
			t.Errorf("seconds = %d, want 3 (2.3 s rounded up)", s)
		}
	}
	cleanup()
	if _, err := os.Stat(filepath.Dir(ogg)); !os.IsNotExist(err) {
		t.Error("cleanup left the temporary folder")
	}

	// A file that is not audio fails, so the send falls back to a file.
	bad := filepath.Join(dir, "bad.mp3")
	_ = os.WriteFile(bad, []byte("not audio at all"), 0o600)
	if _, _, err := toVoiceNote(context.Background(), bad); err == nil {
		t.Error("converting garbage should fail")
	}
}
