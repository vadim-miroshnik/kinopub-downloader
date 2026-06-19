package downloader

import (
	"strings"
	"testing"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

func TestToISO6392(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"ru", "rus"},
		{"en", "eng"},
		{"uk", "ukr"},
		{"de", "ger"},
		{"fr", "fre"},
		{"rus", "rus"},
		{"eng", "eng"},
		{"ukr", "ukr"},
		{"RU", "rus"},
		{"En", "eng"},
		// Unknown 2-letter code passes through lowercased.
		{"xx", "xx"},
		// Unknown 3-letter code passes through lowercased.
		{"xyz", "xyz"},
		// Longer codes pass through lowercased.
		{"abcd", "abcd"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ToISO6392(tt.input)
			if got != tt.want {
				t.Errorf("ToISO6392(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuildAudioLabels(t *testing.T) {
	tests := []struct {
		name   string
		tracks []domain.AudioTrack
		want   []string
	}{
		{
			name:   "empty",
			tracks: nil,
			want:   []string{},
		},
		{
			name: "studio takes priority",
			tracks: []domain.AudioTrack{
				{Studio: "LostFilm", Language: "ru"},
				{Studio: "Кубик в Кубе", Language: "ru"},
			},
			want: []string{"LostFilm", "Кубик в Кубе"},
		},
		{
			name: "language fallback",
			tracks: []domain.AudioTrack{
				{Language: "ru"},
				{Language: "en"},
			},
			want: []string{"ru", "en"},
		},
		{
			name: "audio fallback",
			tracks: []domain.AudioTrack{
				{},
				{},
			},
			want: []string{"Audio", "Audio (2)"},
		},
		{
			name: "mixed with duplicates",
			tracks: []domain.AudioTrack{
				{Studio: "LostFilm", Language: "ru"},
				{Language: "ru"},
				{Language: "ru"},
				{},
			},
			want: []string{"LostFilm", "ru", "ru (2)", "Audio"},
		},
		{
			name: "three identical studios",
			tracks: []domain.AudioTrack{
				{Studio: "Studio"},
				{Studio: "Studio"},
				{Studio: "Studio"},
			},
			want: []string{"Studio", "Studio (2)", "Studio (3)"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildAudioLabels(tt.tracks)
			if len(got) != len(tt.want) {
				t.Fatalf("BuildAudioLabels() returned %d labels, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("label[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestBuildSubtitleLabels(t *testing.T) {
	tests := []struct {
		name   string
		tracks []domain.SubtitleTrack
		want   []string
	}{
		{
			name:   "empty",
			tracks: nil,
			want:   []string{},
		},
		{
			name: "source takes priority",
			tracks: []domain.SubtitleTrack{
				{Source: "OpenSubtitles", Language: "en"},
				{Source: "Forced", Language: "ru"},
			},
			want: []string{"OpenSubtitles", "Forced"},
		},
		{
			name: "language fallback",
			tracks: []domain.SubtitleTrack{
				{Language: "ru"},
				{Language: "en"},
			},
			want: []string{"ru", "en"},
		},
		{
			name: "subtitle fallback with duplicates",
			tracks: []domain.SubtitleTrack{
				{},
				{},
			},
			want: []string{"Subtitle", "Subtitle (2)"},
		},
		{
			name: "mixed",
			tracks: []domain.SubtitleTrack{
				{Source: "SDH", Language: "en"},
				{Language: "en"},
				{},
			},
			want: []string{"SDH", "en", "Subtitle"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildSubtitleLabels(tt.tracks)
			if len(got) != len(tt.want) {
				t.Fatalf("BuildSubtitleLabels() returned %d labels, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("label[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestBuildFFmpegArgs_HLS(t *testing.T) {
	job := domain.Job{
		Episode: domain.Episode{
			Key: domain.EpisodeKey{Series: "test", Season: 1, Episode: 1},
		},
		Media: domain.ResolvedMedia{
			Source: domain.MediaSource{
				Kind: domain.MediaHLS,
				URL:  "https://cdn.example.com/master.m3u8",
			},
			Video: domain.VideoTrack{Index: 0, Resolution: "1920x1080"},
			Audio: []domain.AudioTrack{
				{Index: 0, Language: "ru", Studio: "LostFilm"},
				{Index: 1, Language: "en", Studio: ""},
			},
			Subtitles: []domain.SubtitleTrack{
				{Index: 0, Language: "ru", Source: "Forced"},
			},
		},
		OutPath: "/output/S01E01.mkv",
	}

	args := BuildFFmpegArgs(job, nil, domain.RequestAuth{}, "/tmp/S01E01.mkv.tmp", nil)

	// Verify key elements are present.
	argsStr := strings.Join(args, " ")

	// Should have -y flag.
	if !contains(args, "-y") {
		t.Error("missing -y flag")
	}

	// Should have video input.
	if !strings.Contains(argsStr, "-i https://cdn.example.com/master.m3u8") {
		t.Error("missing video input URL")
	}

	// Should map video from input 0.
	if !containsPair(args, "-map", "0:v") {
		t.Error("missing -map 0:v")
	}

	// Should map audio from inputs 1 and 2 (HLS separate inputs).
	if !containsPair(args, "-map", "1:a") {
		t.Error("missing -map 1:a for first audio")
	}
	if !containsPair(args, "-map", "2:a") {
		t.Error("missing -map 2:a for second audio")
	}

	// Should map subtitle from input 3 (after 2 audio inputs).
	if !containsPair(args, "-map", "3:s") {
		t.Error("missing -map 3:s for subtitle")
	}

	// Should have -c copy.
	if !containsPair(args, "-c", "copy") {
		t.Error("missing -c copy")
	}

	// Should have audio metadata.
	if !strings.Contains(argsStr, "title=LostFilm") {
		t.Error("missing audio title metadata for LostFilm")
	}
	if !strings.Contains(argsStr, "language=rus") {
		t.Error("missing audio language metadata for rus")
	}
	if !strings.Contains(argsStr, "title=en") {
		t.Error("missing audio title metadata for en (language fallback)")
	}
	if !strings.Contains(argsStr, "language=eng") {
		t.Error("missing audio language metadata for eng")
	}

	// Should have subtitle metadata.
	if !strings.Contains(argsStr, "title=Forced") {
		t.Error("missing subtitle title metadata")
	}
	if !strings.Contains(argsStr, "-metadata:s:s:0") {
		t.Error("missing subtitle metadata stream specifier")
	}

	// Should have progress pipe.
	if !containsPair(args, "-progress", "pipe:1") {
		t.Error("missing -progress pipe:1")
	}

	// Last arg should be the temp path.
	if args[len(args)-1] != "/tmp/S01E01.mkv.tmp" {
		t.Errorf("last arg = %q, want temp path", args[len(args)-1])
	}
}

func TestBuildFFmpegArgs_Progressive(t *testing.T) {
	job := domain.Job{
		Episode: domain.Episode{
			Key: domain.EpisodeKey{Series: "test", Season: 2, Episode: 5},
		},
		Media: domain.ResolvedMedia{
			Source: domain.MediaSource{
				Kind: domain.MediaProgressive,
				URL:  "https://cdn.example.com/video.mp4",
			},
			Video: domain.VideoTrack{Index: 0},
			Audio: []domain.AudioTrack{
				{Index: 0, Language: "ru", Studio: "Кубик в Кубе"},
				{Index: 1, Language: "en"},
			},
			Subtitles: nil,
		},
		OutPath: "/output/S02E05.mkv",
	}

	args := BuildFFmpegArgs(job, nil, domain.RequestAuth{}, "/tmp/S02E05.mkv.tmp", nil)

	// Progressive: single -i.
	inputCount := 0
	for _, a := range args {
		if a == "-i" {
			inputCount++
		}
	}
	if inputCount != 1 {
		t.Errorf("progressive should have 1 -i, got %d", inputCount)
	}

	// Audio maps should reference input 0 with stream specifiers.
	if !containsPair(args, "-map", "0:a:0") {
		t.Error("missing -map 0:a:0")
	}
	if !containsPair(args, "-map", "0:a:1") {
		t.Error("missing -map 0:a:1")
	}

	// No subtitle maps.
	for _, a := range args {
		if strings.Contains(a, ":s") && strings.HasPrefix(a, "0:s") {
			// This is fine — we just check there are no subtitle map entries.
		}
	}
	for i, a := range args {
		if a == "-map" && i+1 < len(args) && strings.Contains(args[i+1], ":s") {
			t.Error("progressive with no subtitles should not have subtitle maps")
		}
	}
}

func TestBuildFFmpegArgs_NoAudioNoSubtitles(t *testing.T) {
	job := domain.Job{
		Media: domain.ResolvedMedia{
			Source: domain.MediaSource{
				Kind: domain.MediaProgressive,
				URL:  "https://cdn.example.com/video.mp4",
			},
			Video: domain.VideoTrack{Index: 0},
		},
	}

	args := BuildFFmpegArgs(job, nil, domain.RequestAuth{}, "/tmp/out.mkv.tmp", nil)

	// Should still have -map 0:v.
	if !containsPair(args, "-map", "0:v") {
		t.Error("missing -map 0:v")
	}

	// No audio or subtitle metadata.
	argsStr := strings.Join(args, " ")
	if strings.Contains(argsStr, "-metadata:s:a") {
		t.Error("should not have audio metadata with no audio tracks")
	}
	if strings.Contains(argsStr, "-metadata:s:s") {
		t.Error("should not have subtitle metadata with no subtitle tracks")
	}
}

// TestBuildFFmpegArgs_NoPrefs_Golden is the AC2a guard: BuildFFmpegArgs with no
// audio/subtitle tracks produces output byte-identical to the captured baseline.
func TestBuildFFmpegArgs_NoPrefs_Golden(t *testing.T) {
	job := domain.Job{
		Episode: domain.Episode{
			Key:   domain.EpisodeKey{Series: "test", Season: 1, Episode: 1},
			Title: "Pilot",
		},
		SeriesTitle: "Test Show",
		Media: domain.ResolvedMedia{
			Source: domain.MediaSource{
				Kind: domain.MediaProgressive,
				URL:  "https://cdn.example.com/video.mp4",
			},
			Video: domain.VideoTrack{Index: 0},
		},
		OutPath: "/output/S01E01.mkv",
	}

	got := BuildFFmpegArgs(job, nil, domain.RequestAuth{}, "/tmp/S01E01.mkv.tmp", nil)

	want := []string{
		"-y",
		"-i", "https://cdn.example.com/video.mp4",
		"-map", "0:v",
		"-c", "copy",
		"-progress", "pipe:1",
		"-metadata", "title=Pilot",
		"-metadata", "SHOW=Test Show",
		"-metadata", "episode_sort=1",
		"-metadata", "season_number=1",
		"-metadata", "episode_id=S01E01",
		"-f", "matroska",
		"/tmp/S01E01.mkv.tmp",
	}

	assertArgsEqual(t, got, want)
}

func TestBuildFFmpegArgs_WithProxyEnv(t *testing.T) {
	job := domain.Job{
		Media: domain.ResolvedMedia{
			Source: domain.MediaSource{
				Kind: domain.MediaHLS,
				URL:  "https://cdn.example.com/master.m3u8",
			},
			Video: domain.VideoTrack{Index: 0},
		},
	}

	proxyEnv := []string{"-http_proxy http://proxy.example.com:8080"}
	args := BuildFFmpegArgs(job, proxyEnv, domain.RequestAuth{}, "/tmp/out.mkv.tmp", nil)

	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, "-http_proxy") {
		t.Error("missing proxy arg")
	}
	if !strings.Contains(argsStr, "http://proxy.example.com:8080") {
		t.Error("missing proxy URL")
	}
}

// hlsMuxJob is a shared job fixture for BuildHLSMuxArgs golden tests.
func hlsMuxJob() domain.Job {
	return domain.Job{
		Episode: domain.Episode{
			Key:   domain.EpisodeKey{Series: "test", Season: 1, Episode: 1},
			Title: "Pilot",
		},
		SeriesTitle: "Test Show",
	}
}

// TestBuildHLSMuxArgs_NoSubtitles_Golden is the AC2b regression guard: with no
// subtitles and SubtitlesExternal=false, the output must be byte-identical to
// the pre-subtitle-feature behavior. The expected slice is captured explicitly
// so any future drift fails the test.
func TestBuildHLSMuxArgs_NoSubtitles_Golden(t *testing.T) {
	job := hlsMuxJob()
	hls := &domain.HLSDownloadResult{
		VideoPath: "/tmp/work/video.ts",
		AudioTracks: []domain.HLSAudioTrack{
			{Path: "/tmp/work/audio_0.ts", Name: "LostFilm", Language: "ru"},
		},
	}

	got := BuildHLSMuxArgs(job, hls, "/output/S01E01.mkv.tmp")

	want := []string{
		"-y",
		"-i", "/tmp/work/video.ts",
		"-i", "/tmp/work/audio_0.ts",
		"-map", "0:v:0",
		"-map", "1:a:0",
		"-c", "copy",
		"-metadata:s:a:0", "title=LostFilm",
		"-metadata:s:a:0", "language=rus",
		"-metadata", "title=Pilot",
		"-metadata", "SHOW=Test Show",
		"-metadata", "episode_sort=1",
		"-metadata", "season_number=1",
		"-metadata", "episode_id=S01E01",
		"-f", "matroska",
		"/output/S01E01.mkv.tmp",
	}

	assertArgsEqual(t, got, want)
}

// TestBuildHLSMuxArgs_ExternalSubtitles_Golden asserts that with subtitles
// present but SubtitlesExternal=true, the muxed output is byte-identical to the
// no-subtitle case (subtitles are written as sidecars instead).
func TestBuildHLSMuxArgs_ExternalSubtitles_Golden(t *testing.T) {
	job := hlsMuxJob()
	job.SubtitlesExternal = true
	hls := &domain.HLSDownloadResult{
		VideoPath: "/tmp/work/video.ts",
		AudioTracks: []domain.HLSAudioTrack{
			{Path: "/tmp/work/audio_0.ts", Name: "LostFilm", Language: "ru"},
		},
		Subtitles: []domain.HLSSubtitleTrack{
			{Path: "/tmp/work/subs_0/index.m3u8", Name: "RUS", Language: "rus"},
			{Path: "/tmp/work/subs_1/index.m3u8", Name: "ENG", Language: "eng"},
		},
	}

	got := BuildHLSMuxArgs(job, hls, "/output/S01E01.mkv.tmp")

	want := []string{
		"-y",
		"-i", "/tmp/work/video.ts",
		"-i", "/tmp/work/audio_0.ts",
		"-map", "0:v:0",
		"-map", "1:a:0",
		"-c", "copy",
		"-metadata:s:a:0", "title=LostFilm",
		"-metadata:s:a:0", "language=rus",
		"-metadata", "title=Pilot",
		"-metadata", "SHOW=Test Show",
		"-metadata", "episode_sort=1",
		"-metadata", "season_number=1",
		"-metadata", "episode_id=S01E01",
		"-f", "matroska",
		"/output/S01E01.mkv.tmp",
	}

	assertArgsEqual(t, got, want)

	// No subtitle-specific tokens should appear.
	argsStr := strings.Join(got, " ")
	if strings.Contains(argsStr, "-allowed_extensions") {
		t.Error("external mode must not add -allowed_extensions")
	}
	if strings.Contains(argsStr, "-c:s") {
		t.Error("external mode must not add -c:s")
	}
	if strings.Contains(argsStr, "-metadata:s:s") {
		t.Error("external mode must not add subtitle metadata")
	}
}

// TestBuildHLSMuxArgs_WithSubtitles_MKV verifies that with 2 subtitles in MKV
// mode, the subtitle inputs, maps, codec, and metadata appear in the expected
// positions and ordering.
func TestBuildHLSMuxArgs_WithSubtitles_MKV(t *testing.T) {
	job := hlsMuxJob()
	hls := &domain.HLSDownloadResult{
		VideoPath: "/tmp/work/video.ts",
		AudioTracks: []domain.HLSAudioTrack{
			{Path: "/tmp/work/audio_0.ts", Name: "LostFilm", Language: "ru"},
		},
		Subtitles: []domain.HLSSubtitleTrack{
			{Path: "/tmp/work/subs_0/index.m3u8", Name: "RUS", Language: "rus"},
			{Path: "/tmp/work/subs_1/index.m3u8", Name: "ENG", Language: "eng"},
		},
	}

	got := BuildHLSMuxArgs(job, hls, "/output/S01E01.mkv.tmp")

	want := []string{
		"-y",
		"-i", "/tmp/work/video.ts",
		"-i", "/tmp/work/audio_0.ts",
		// Subtitle inputs: index 2 and 3 (1 video + 1 audio).
		"-allowed_extensions", "ALL", "-f", "hls", "-i", "/tmp/work/subs_0/index.m3u8",
		"-allowed_extensions", "ALL", "-f", "hls", "-i", "/tmp/work/subs_1/index.m3u8",
		"-map", "0:v:0",
		"-map", "1:a:0",
		"-map", "2:s:0",
		"-map", "3:s:0",
		"-c", "copy",
		"-c:s", "srt",
		"-metadata:s:a:0", "title=LostFilm",
		"-metadata:s:a:0", "language=rus",
		"-metadata:s:s:0", "title=RUS",
		"-metadata:s:s:0", "language=rus",
		"-metadata:s:s:1", "title=ENG",
		"-metadata:s:s:1", "language=eng",
		"-metadata", "title=Pilot",
		"-metadata", "SHOW=Test Show",
		"-metadata", "episode_sort=1",
		"-metadata", "season_number=1",
		"-metadata", "episode_id=S01E01",
		"-f", "matroska",
		"/output/S01E01.mkv.tmp",
	}

	assertArgsEqual(t, got, want)
}

// TestBuildHLSMuxArgs_WithSubtitles_MP4 verifies mov_text codec for MP4 output.
func TestBuildHLSMuxArgs_WithSubtitles_MP4(t *testing.T) {
	job := hlsMuxJob()
	hls := &domain.HLSDownloadResult{
		VideoPath: "/tmp/work/video.ts",
		Subtitles: []domain.HLSSubtitleTrack{
			{Path: "/tmp/work/subs_0/index.m3u8", Name: "RUS", Language: "rus"},
		},
	}

	got := BuildHLSMuxArgs(job, hls, "/output/S01E01.mp4.tmp")
	if !containsPair(got, "-c:s", "mov_text") {
		t.Error("mp4 output should use -c:s mov_text")
	}
	// First (and only) subtitle input index = 1 (1 video, 0 audio).
	if !containsPair(got, "-map", "1:s:0") {
		t.Error("missing subtitle map 1:s:0 for single subtitle with no audio")
	}
}

func TestBuildSubtitleSidecarArgs(t *testing.T) {
	got := BuildSubtitleSidecarArgs("/tmp/work/subs_0/index.m3u8", "/output/S01E01.rus.srt")
	want := []string{
		"-y",
		"-allowed_extensions", "ALL",
		"-f", "hls",
		"-i", "/tmp/work/subs_0/index.m3u8",
		"-map", "0:s:0",
		"-c:s", "srt",
		"-f", "srt",
		"/output/S01E01.rus.srt",
	}
	assertArgsEqual(t, got, want)
}

func TestSubtitleSidecarPath(t *testing.T) {
	tests := []struct {
		name      string
		container string
		lang      string
		dedup     string
		want      string
	}{
		{
			name:      "mkv no dedup",
			container: "/output/S01E01.mkv",
			lang:      "rus",
			dedup:     "",
			want:      "/output/S01E01.rus.srt",
		},
		{
			name:      "mp4 no dedup",
			container: "/output/Show - S02E05.mp4",
			lang:      "eng",
			dedup:     "",
			want:      "/output/Show - S02E05.eng.srt",
		},
		{
			name:      "with dedup suffix",
			container: "/output/S01E01.mkv",
			lang:      "rus",
			dedup:     "2",
			want:      "/output/S01E01.rus.2.srt",
		},
		{
			name:      "no extension",
			container: "/output/S01E01",
			lang:      "und",
			dedup:     "",
			want:      "/output/S01E01.und.srt",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SubtitleSidecarPath(tt.container, tt.lang, tt.dedup)
			if got != tt.want {
				t.Errorf("SubtitleSidecarPath(%q, %q, %q) = %q, want %q", tt.container, tt.lang, tt.dedup, got, tt.want)
			}
		})
	}
}

// assertArgsEqual compares two arg slices element-by-element with a precise diff.
func assertArgsEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("arg count mismatch: got %d, want %d\ngot:  %v\nwant: %v", len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// Helper: check if args contains a specific value.
func contains(args []string, val string) bool {
	for _, a := range args {
		if a == val {
			return true
		}
	}
	return false
}

// Helper: check if args contains a pair (flag, value) adjacent.
func containsPair(args []string, flag, value string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}
