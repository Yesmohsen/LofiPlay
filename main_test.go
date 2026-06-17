package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanTrackName(t *testing.T) {
	tests := map[string]string{
		"Artist - Song - musicgeek.ir.mp3": "Artist - Song",
		"Track Name.mp3":                   "Track Name",
		"Name - example.com.mp3":           "Name",
		"Name___.mp3":                      "Name",
	}

	for input, want := range tests {
		if got := cleanTrackName(input); got != want {
			t.Fatalf("cleanTrackName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestMP3FrameInfoRejectsInvalidHeader(t *testing.T) {
	frameLength, samples, sampleRate := mp3FrameInfo(0)
	if frameLength != 0 || samples != 0 || sampleRate != 0 {
		t.Fatalf("invalid header returned %d, %d, %d", frameLength, samples, sampleRate)
	}
}

func TestMP3Bitrate(t *testing.T) {
	if got := mp3Bitrate(3, 1, 9); got != 128 {
		t.Fatalf("mp3Bitrate returned %d, want 128", got)
	}
}

func TestRadioFilesKeyChangesWithFileContent(t *testing.T) {
	dir := t.TempDir()
	oldAudioDir := audioDir
	audioDir = dir
	t.Cleanup(func() { audioDir = oldAudioDir })

	path := filepath.Join(dir, "track.mp3")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	first := radioFilesKey([]string{"track.mp3"})

	if err := os.WriteFile(path, []byte("two-two"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := radioFilesKey([]string{"track.mp3"})

	if first == second {
		t.Fatal("radioFilesKey did not change after file content changed")
	}
}
