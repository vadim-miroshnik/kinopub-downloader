package kinopub

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// fakeSubsHLS extends fakeHLSDownloader with the optional subtitles-only
// capability. Episodes in `missing` have no matching subtitles.
type fakeSubsHLS struct {
	*fakeHLSDownloader
	missing  map[domain.EpisodeKey]bool
	subCalls map[domain.EpisodeKey]int
}

func newFakeSubsHLS() *fakeSubsHLS {
	return &fakeSubsHLS{
		fakeHLSDownloader: newFakeHLS(nil),
		missing:           make(map[domain.EpisodeKey]bool),
		subCalls:          make(map[domain.EpisodeKey]int),
	}
}

func (f *fakeSubsHLS) DownloadSubtitlesOnly(_ context.Context, _ string, _ domain.Quality, _ string, key domain.EpisodeKey, _ domain.ProgressSink) (*domain.HLSDownloadResult, error) {
	f.mu.Lock()
	f.subCalls[key]++
	missing := f.missing[key]
	f.mu.Unlock()
	if missing {
		return nil, fmt.Errorf("%w: S%02dE%02d", domain.ErrNoSubtitlesMatched, key.Season, key.Episode)
	}
	return &domain.HLSDownloadResult{
		Subtitles: []domain.HLSSubtitleTrack{{Path: "/tmp/subs/index.m3u8", Name: "RUS", Language: "rus"}},
		// TempDir empty so the engine does not RemoveAll a real directory.
	}, nil
}

// sidecarDownloader is a muxingDownloader that also writes subtitle sidecars,
// satisfying domain.SubtitleSidecarWriter.
type sidecarDownloader struct {
	muxingDownloader
	written map[domain.EpisodeKey]int
}

func (s *sidecarDownloader) WriteSubtitleSidecars(_ context.Context, job domain.Job, _ *domain.HLSDownloadResult) error {
	s.written[job.Episode.Key]++
	return nil
}

func newSubsOnlyEngine(hls domain.HLSDownloader) (*engine, *recordingReporter, *mockStateStore, *sidecarDownloader) {
	rec := &recordingReporter{}
	ss := &mockStateStore{}
	dl := &sidecarDownloader{written: make(map[domain.EpisodeKey]int)}
	deps := Dependencies{
		Logger:           &mockLogger{},
		InputResolver:    &mockInputResolver{},
		FeedParser:       &mockFeedParser{},
		MediaResolver:    &mockMediaResolver{},
		Scheduler:        &mockScheduler{},
		Downloader:       dl,
		ProxyProvider:    &mockProxyProvider{},
		ProgressReporter: rec,
		StateStore:       ss,
		OutputLayout:     &mockOutputLayout{path: "/tmp/out/ep.mkv"},
		HLSDownloader:    hls,
		PageScraper:      &fakePageScraper{},
	}
	e := &engine{
		deps:         deps,
		retryBackoff: func(int) time.Duration { return 0 },
	}
	return e, rec, ss, dl
}

func subsOnlyConfig() domain.RunConfig {
	cfg := domain.RunConfig{InputURL: "https://kino.pub/item/view/42", Quality: "720p", SubtitlesOnly: true}
	ApplyDefaults(&cfg)
	return cfg
}

// All episodes have subtitles: each gets a sidecar, none is marked completed in
// state (subtitles-only is idempotent and independent of video state).
func TestRunHLS_SubtitlesOnly_Success(t *testing.T) {
	hls := newFakeSubsHLS()
	e, rec, ss, dl := newSubsOnlyEngine(hls)
	e.deps.PageScraper = &fakePageScraper{playlist: makePlaylist(3)}

	res, err := e.runHLS(context.Background(), subsOnlyConfig())
	if err != nil {
		t.Fatalf("runHLS error: %v", err)
	}
	if res.Succeeded != 3 || res.Failed != 0 {
		t.Fatalf("Succeeded=%d Failed=%d, want 3/0", res.Succeeded, res.Failed)
	}
	if len(dl.written) != 3 {
		t.Errorf("sidecars written for %d episodes, want 3", len(dl.written))
	}
	if len(rec.completed) != 3 {
		t.Errorf("completed reported %d times, want 3", len(rec.completed))
	}
	// Subtitles-only must NOT mark video completion state.
	if len(ss.completed) != 0 {
		t.Errorf("state should not be marked completed in subs-only mode, got %d", len(ss.completed))
	}
	// The full-episode video downloader must never be invoked.
	if len(hls.calls) != 0 {
		t.Errorf("DownloadEpisode should not be called in subs-only mode, got %d calls", len(hls.calls))
	}
}

// An episode whose subtitles are missing fails fatally (no retry) while the
// others still produce sidecars — fail-at-end semantics.
func TestRunHLS_SubtitlesOnly_MissingIsFatal(t *testing.T) {
	hls := newFakeSubsHLS()
	key2 := domain.EpisodeKey{Series: "42", Season: 1, Episode: 2}
	hls.missing[key2] = true

	e, _, _, dl := newSubsOnlyEngine(hls)
	e.deps.PageScraper = &fakePageScraper{playlist: makePlaylist(3)}

	res, err := e.runHLS(context.Background(), subsOnlyConfig())
	if err != nil {
		t.Fatalf("runHLS error: %v", err)
	}
	if res.Failed != 1 {
		t.Errorf("Failed=%d, want 1", res.Failed)
	}
	if res.Succeeded != 2 {
		t.Errorf("Succeeded=%d, want 2 (fail-at-end: other episodes still written)", res.Succeeded)
	}
	if dl.written[key2] != 0 {
		t.Errorf("missing-subs episode must not write a sidecar")
	}
	// Missing subtitles are permanent — the episode is attempted exactly once.
	if hls.subCalls[key2] != 1 {
		t.Errorf("missing-subs episode attempted %d times, want 1 (no retry)", hls.subCalls[key2])
	}
}

// --subs-only must refuse to run when the HLS pipeline is unavailable rather
// than silently falling back to the RSS pipeline (which downloads full video).
func TestRun_SubtitlesOnly_RequiresHLSPipeline(t *testing.T) {
	e, _, _, _ := newSubsOnlyEngine(nil) // no HLS downloader
	e.deps.HLSDownloader = nil
	e.deps.PageScraper = nil

	_, err := e.run(context.Background(), subsOnlyConfig())
	if err == nil {
		t.Fatalf("want error when --subs-only and HLS pipeline unavailable, got nil")
	}
}
