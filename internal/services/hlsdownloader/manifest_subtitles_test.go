package hlsdownloader

import (
	"strings"
	"testing"
)

const subtitleMasterPlaylist = `#EXTM3U
#EXT-X-VERSION:3

#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",NAME="Russian",LANGUAGE="rus",URI="audio/rus/index.m3u8"
#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="sub",NAME="RUS #12",LANGUAGE="rus",URI="https://cdn.example/hls/HASH/subtitles/e/74/2699290.srt/index.m3u8?loc=nl"
#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="sub",NAME="ENG #5",LANGUAGE="eng",URI="https://cdn.example/hls/HASH/subtitles/e/74/2699291.srt/index.m3u8?loc=nl"

#EXT-X-STREAM-INF:BANDWIDTH=2500000,RESOLUTION=1920x1080,CODECS="avc1.640028,mp4a.40.2",AUDIO="audio",SUBTITLES="sub"
https://cdn.example/hls/HASH/1080p/index.m3u8
`

func TestParseMasterPlaylist_Subtitles(t *testing.T) {
	baseURL := "https://cdn.example/hls/HASH/master.m3u8"
	mp, err := parseMasterPlaylist(strings.NewReader(subtitleMasterPlaylist), baseURL)
	if err != nil {
		t.Fatalf("parseMasterPlaylist error: %v", err)
	}

	// --- subtitle rendition count ---
	if got := len(mp.Subtitles); got != 2 {
		t.Fatalf("want 2 subtitle renditions, got %d", got)
	}

	// --- first subtitle: RUS ---
	rus := mp.Subtitles[0]
	if rus.GroupID != "sub" {
		t.Errorf("Subtitles[0].GroupID: want %q, got %q", "sub", rus.GroupID)
	}
	if rus.Name != "RUS #12" {
		t.Errorf("Subtitles[0].Name: want %q, got %q", "RUS #12", rus.Name)
	}
	if rus.Language != "rus" {
		t.Errorf("Subtitles[0].Language: want %q, got %q", "rus", rus.Language)
	}
	// URI is already absolute — must be returned unchanged.
	wantRusURI := "https://cdn.example/hls/HASH/subtitles/e/74/2699290.srt/index.m3u8?loc=nl"
	if rus.URI != wantRusURI {
		t.Errorf("Subtitles[0].URI: want %q, got %q", wantRusURI, rus.URI)
	}

	// --- second subtitle: ENG ---
	eng := mp.Subtitles[1]
	if eng.Language != "eng" {
		t.Errorf("Subtitles[1].Language: want %q, got %q", "eng", eng.Language)
	}
	wantEngURI := "https://cdn.example/hls/HASH/subtitles/e/74/2699291.srt/index.m3u8?loc=nl"
	if eng.URI != wantEngURI {
		t.Errorf("Subtitles[1].URI: want %q, got %q", wantEngURI, eng.URI)
	}

	// --- variant SubtitleGroup ---
	if got := len(mp.Variants); got != 1 {
		t.Fatalf("want 1 variant, got %d", got)
	}
	if mp.Variants[0].SubtitleGroup != "sub" {
		t.Errorf("Variants[0].SubtitleGroup: want %q, got %q", "sub", mp.Variants[0].SubtitleGroup)
	}

	// --- audio rendition still parsed (existing behaviour unchanged) ---
	if got := len(mp.Audio); got != 1 {
		t.Errorf("want 1 audio rendition, got %d", got)
	}
	if mp.Variants[0].AudioGroup != "audio" {
		t.Errorf("Variants[0].AudioGroup: want %q, got %q", "audio", mp.Variants[0].AudioGroup)
	}
}

func TestParseMasterPlaylist_Subtitles_RelativeURI(t *testing.T) {
	// Relative subtitle URIs must be resolved against the base URL.
	const playlist = `#EXTM3U
#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="sub",NAME="RUS",LANGUAGE="rus",URI="subtitles/rus/index.m3u8"

#EXT-X-STREAM-INF:BANDWIDTH=1000000,RESOLUTION=1280x720,SUBTITLES="sub"
720p/index.m3u8
`
	baseURL := "https://cdn.example/hls/HASH/master.m3u8"
	mp, err := parseMasterPlaylist(strings.NewReader(playlist), baseURL)
	if err != nil {
		t.Fatalf("parseMasterPlaylist error: %v", err)
	}

	if len(mp.Subtitles) != 1 {
		t.Fatalf("want 1 subtitle rendition, got %d", len(mp.Subtitles))
	}

	want := "https://cdn.example/hls/HASH/subtitles/rus/index.m3u8"
	if got := mp.Subtitles[0].URI; got != want {
		t.Errorf("resolved subtitle URI: want %q, got %q", want, got)
	}
}
