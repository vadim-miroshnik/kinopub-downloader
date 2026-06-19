package hlsdownloader

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// noopLogger is a domain.Logger that discards everything.
type noopLogger struct{}

func (noopLogger) Debug(string, ...domain.Field)        {}
func (noopLogger) Info(string, ...domain.Field)         {}
func (noopLogger) Warn(string, ...domain.Field)         {}
func (noopLogger) Error(string, ...domain.Field)        {}
func (n noopLogger) With(...domain.Field) domain.Logger { return n }
func (n noopLogger) Component(string) domain.Logger     { return n }

// subsTestServer serves a master playlist with two subtitle renditions (rus,
// eng), their media playlists, and .vtt segments. When withSubs is false the
// master advertises no subtitle group.
func subsTestServer(t *testing.T, withSubs bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/master.m3u8", func(w http.ResponseWriter, _ *http.Request) {
		var b strings.Builder
		b.WriteString("#EXTM3U\n")
		if withSubs {
			b.WriteString(`#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="subs",NAME="RUS",LANGUAGE="rus",URI="rus.m3u8"` + "\n")
			b.WriteString(`#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="subs",NAME="ENG",LANGUAGE="eng",URI="eng.m3u8"` + "\n")
			b.WriteString("#EXT-X-STREAM-INF:BANDWIDTH=1000000,RESOLUTION=1280x720,SUBTITLES=\"subs\"\n")
		} else {
			b.WriteString("#EXT-X-STREAM-INF:BANDWIDTH=1000000,RESOLUTION=1280x720\n")
		}
		b.WriteString("video.m3u8\n")
		_, _ = w.Write([]byte(b.String()))
	})

	mediaPlaylist := func(seg string) string {
		return "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:6\n" +
			"#EXTINF:6,\n" + seg + "_0.vtt\n" +
			"#EXTINF:6,\n" + seg + "_1.vtt\n" +
			"#EXT-X-ENDLIST\n"
	}
	mux.HandleFunc("/rus.m3u8", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(mediaPlaylist("rus")))
	})
	mux.HandleFunc("/eng.m3u8", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(mediaPlaylist("eng")))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".vtt") {
			_, _ = w.Write([]byte("WEBVTT\n\n00:00:00.000 --> 00:00:06.000\nhello\n"))
			return
		}
		http.NotFound(w, r)
	})

	return httptest.NewServer(mux)
}

func newTestDownloader(client *http.Client) *Downloader {
	return &Downloader{
		client:      client,
		logger:      noopLogger{},
		concurrency: 4,
	}
}

func TestDownloadSubtitlesOnly_HappyPath(t *testing.T) {
	srv := subsTestServer(t, true)
	defer srv.Close()

	d := newTestDownloader(srv.Client())
	// Keep only Russian.
	d.SetSubtitlePreference(domain.SubtitlePreference{Include: []string{"rus"}})

	outBase := filepath.Join(t.TempDir(), "S01E01.mkv")
	key := domain.EpisodeKey{Series: "1", Season: 1, Episode: 1}

	res, err := d.DownloadSubtitlesOnly(context.Background(), srv.URL+"/master.m3u8", "", outBase, key, nil)
	if err != nil {
		t.Fatalf("DownloadSubtitlesOnly error: %v", err)
	}
	defer os.RemoveAll(res.TempDir)

	if len(res.Subtitles) != 1 {
		t.Fatalf("want 1 subtitle track, got %d", len(res.Subtitles))
	}
	if res.Subtitles[0].Language != "rus" {
		t.Errorf("want rus track, got %q", res.Subtitles[0].Language)
	}
	// No video/audio produced.
	if res.VideoPath != "" {
		t.Errorf("VideoPath must be empty for subtitles-only, got %q", res.VideoPath)
	}
	if len(res.AudioTracks) != 0 {
		t.Errorf("AudioTracks must be empty, got %d", len(res.AudioTracks))
	}
	// The local playlist and its .vtt segments exist on disk.
	if _, statErr := os.Stat(res.Subtitles[0].Path); statErr != nil {
		t.Errorf("subtitle playlist not written: %v", statErr)
	}
	segDir := filepath.Dir(res.Subtitles[0].Path)
	for _, seg := range []string{"seg_00000.vtt", "seg_00001.vtt"} {
		if _, statErr := os.Stat(filepath.Join(segDir, seg)); statErr != nil {
			t.Errorf("segment %s missing: %v", seg, statErr)
		}
	}
}

func TestDownloadSubtitlesOnly_KeepAll(t *testing.T) {
	srv := subsTestServer(t, true)
	defer srv.Close()

	d := newTestDownloader(srv.Client())
	// No preference → keep all available subtitles.
	outBase := filepath.Join(t.TempDir(), "S01E01.mkv")
	key := domain.EpisodeKey{Series: "1", Season: 1, Episode: 1}

	res, err := d.DownloadSubtitlesOnly(context.Background(), srv.URL+"/master.m3u8", "", outBase, key, nil)
	if err != nil {
		t.Fatalf("DownloadSubtitlesOnly error: %v", err)
	}
	defer os.RemoveAll(res.TempDir)

	if len(res.Subtitles) != 2 {
		t.Fatalf("want 2 subtitle tracks (keep all), got %d", len(res.Subtitles))
	}
}

func TestDownloadSubtitlesOnly_StrictMissIsError(t *testing.T) {
	srv := subsTestServer(t, true)
	defer srv.Close()

	d := newTestDownloader(srv.Client())
	// Request a language that does not exist → strict error, no fallback.
	d.SetSubtitlePreference(domain.SubtitlePreference{Include: []string{"jpn"}})

	outBase := filepath.Join(t.TempDir(), "S01E01.mkv")
	key := domain.EpisodeKey{Series: "1", Season: 1, Episode: 1}

	_, err := d.DownloadSubtitlesOnly(context.Background(), srv.URL+"/master.m3u8", "", outBase, key, nil)
	if !errors.Is(err, domain.ErrNoSubtitlesMatched) {
		t.Fatalf("want ErrNoSubtitlesMatched, got %v", err)
	}
}

func TestDownloadSubtitlesOnly_NoSubtitleRenditions(t *testing.T) {
	srv := subsTestServer(t, false) // master with no subtitle group
	defer srv.Close()

	d := newTestDownloader(srv.Client())
	outBase := filepath.Join(t.TempDir(), "S01E01.mkv")
	key := domain.EpisodeKey{Series: "1", Season: 1, Episode: 1}

	_, err := d.DownloadSubtitlesOnly(context.Background(), srv.URL+"/master.m3u8", "", outBase, key, nil)
	if !errors.Is(err, domain.ErrNoSubtitlesMatched) {
		t.Fatalf("want ErrNoSubtitlesMatched when no renditions, got %v", err)
	}
}
