package hlsdownloader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubtitleRenditionsFor(t *testing.T) {
	master := &MasterPlaylist{
		Subtitles: []SubtitleRendition{
			{GroupID: "sub", Name: "RUS", Language: "rus", URI: "https://cdn.example/rus/index.m3u8"},
			{GroupID: "sub", Name: "ENG", Language: "eng", URI: "https://cdn.example/eng/index.m3u8"},
			{GroupID: "other", Name: "FRA", Language: "fra", URI: "https://cdn.example/fra/index.m3u8"},
			{GroupID: "sub", Name: "NoURI", Language: "deu", URI: ""},
		},
	}

	// --- matching group: keep group "sub" renditions with a non-empty URI ---
	selected := Variant{SubtitleGroup: "sub"}
	got := subtitleRenditionsFor(master, selected)
	if len(got) != 2 {
		t.Fatalf("want 2 renditions for group %q, got %d", "sub", len(got))
	}
	if got[0].Name != "RUS" || got[1].Name != "ENG" {
		t.Errorf("want [RUS ENG] in master-playlist order, got [%s %s]", got[0].Name, got[1].Name)
	}

	// --- empty group: no subtitles ---
	if r := subtitleRenditionsFor(master, Variant{SubtitleGroup: ""}); len(r) != 0 {
		t.Errorf("want 0 renditions for empty group, got %d", len(r))
	}

	// --- unknown group: no subtitles ---
	if r := subtitleRenditionsFor(master, Variant{SubtitleGroup: "missing"}); len(r) != 0 {
		t.Errorf("want 0 renditions for unknown group, got %d", len(r))
	}
}

func TestWriteLocalSubtitlePlaylist(t *testing.T) {
	subDir := t.TempDir()
	pl := &MediaPlaylist{
		Segments: []Segment{
			{Index: 0, Duration: 6},
			{Index: 1, Duration: 4.5},
			{Index: 2, Duration: 6},
		},
		TotalDuration: 16.5,
	}

	plPath, err := writeLocalSubtitlePlaylist(subDir, pl)
	if err != nil {
		t.Fatalf("writeLocalSubtitlePlaylist error: %v", err)
	}

	// --- returned path is index.m3u8 inside subDir ---
	wantPath := filepath.Join(subDir, "index.m3u8")
	if plPath != wantPath {
		t.Errorf("playlist path: want %q, got %q", wantPath, plPath)
	}

	data, err := os.ReadFile(plPath)
	if err != nil {
		t.Fatalf("read playlist: %v", err)
	}
	content := string(data)

	// --- header lines ---
	for _, want := range []string{
		"#EXTM3U",
		"#EXT-X-VERSION:3",
		"#EXT-X-PLAYLIST-TYPE:VOD",
		"#EXT-X-MEDIA-SEQUENCE:0",
		"#EXT-X-ENDLIST",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("playlist missing %q\n--- playlist ---\n%s", want, content)
		}
	}

	// --- target duration = ceil(max EXTINF) = ceil(6) = 6 ---
	if !strings.Contains(content, "#EXT-X-TARGETDURATION:6") {
		t.Errorf("want #EXT-X-TARGETDURATION:6\n--- playlist ---\n%s", content)
	}

	// --- EXTINF lines and bare segment filenames (no directory, no URL) ---
	for _, want := range []string{
		"#EXTINF:6,\nseg_00000.vtt",
		"#EXTINF:4.5,\nseg_00001.vtt",
		"#EXTINF:6,\nseg_00002.vtt",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("playlist missing segment block %q\n--- playlist ---\n%s", want, content)
		}
	}

	// --- segment references must be bare (no path separators, no scheme) ---
	for _, line := range strings.Split(content, "\n") {
		if strings.HasSuffix(line, ".vtt") {
			if strings.ContainsAny(line, "/\\") {
				t.Errorf("segment reference is not a bare filename: %q", line)
			}
			if strings.Contains(line, "http") {
				t.Errorf("segment reference must be relative, got %q", line)
			}
		}
	}

	// --- ENDLIST is the final tag ---
	trimmed := strings.TrimRight(content, "\n")
	if !strings.HasSuffix(trimmed, "#EXT-X-ENDLIST") {
		t.Errorf("playlist must end with #EXT-X-ENDLIST\n--- playlist ---\n%s", content)
	}
}

func TestWriteLocalSubtitlePlaylist_EmptySegments(t *testing.T) {
	subDir := t.TempDir()
	pl := &MediaPlaylist{}

	plPath, err := writeLocalSubtitlePlaylist(subDir, pl)
	if err != nil {
		t.Fatalf("writeLocalSubtitlePlaylist error: %v", err)
	}
	data, err := os.ReadFile(plPath)
	if err != nil {
		t.Fatalf("read playlist: %v", err)
	}
	content := string(data)

	// TARGETDURATION must be a valid positive integer even with no segments.
	if !strings.Contains(content, "#EXT-X-TARGETDURATION:1") {
		t.Errorf("want #EXT-X-TARGETDURATION:1 for empty playlist\n--- playlist ---\n%s", content)
	}
	if !strings.Contains(content, "#EXT-X-ENDLIST") {
		t.Errorf("want #EXT-X-ENDLIST for empty playlist\n--- playlist ---\n%s", content)
	}
}
