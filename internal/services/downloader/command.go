package downloader

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// buildInputAuthOpts returns ffmpeg input options that inject authentication
// into HTTP(S) requests: -user_agent for the User-Agent and -headers for the
// Referer and extra headers (but NOT Cookie — CDN rejects requests with
// kino.pub cookies, causing timeouts). The returned options must be placed
// immediately before an -i so they apply to that input.
//
// ffmpeg's -headers option takes a single string of CRLF-separated header
// lines. Headers are emitted in a deterministic order so the command is stable.
func buildInputAuthOpts(auth domain.RequestAuth) []string {
	if auth.IsZero() {
		return nil
	}

	var opts []string

	if auth.UserAgent != "" {
		opts = append(opts, "-user_agent", auth.UserAgent)
	}

	// Collect header lines: Cookie first, then extra headers in sorted order.
	var lines []string
	if auth.Cookie != "" {
		lines = append(lines, "Cookie: "+auth.Cookie)
	}
	if len(auth.Headers) > 0 {
		keys := make([]string, 0, len(auth.Headers))
		for k := range auth.Headers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			lines = append(lines, k+": "+auth.Headers[k])
		}
	}

	if len(lines) > 0 {
		// ffmpeg expects header lines separated (and terminated) by CRLF.
		opts = append(opts, "-headers", strings.Join(lines, "\r\n")+"\r\n")
	}

	return opts
}

// iso639Map maps common 2-letter language codes to their ISO 639-2 (3-letter)
// equivalents. If a code is already 3 letters or is unknown, it passes through.
var iso639Map = map[string]string{
	"ru": "rus",
	"en": "eng",
	"uk": "ukr",
	"de": "ger",
	"fr": "fre",
	"es": "spa",
	"it": "ita",
	"ja": "jpn",
	"ko": "kor",
	"zh": "chi",
	"pt": "por",
	"pl": "pol",
	"nl": "dut",
	"sv": "swe",
	"no": "nor",
	"da": "dan",
	"fi": "fin",
	"cs": "cze",
	"sk": "slo",
	"hu": "hun",
	"ro": "rum",
	"bg": "bul",
	"hr": "hrv",
	"sr": "srp",
	"tr": "tur",
	"ar": "ara",
	"he": "heb",
	"hi": "hin",
	"th": "tha",
	"vi": "vie",
	"el": "gre",
	"ka": "geo",
}

// ToISO6392 converts a language code to its ISO 639-2 (3-letter) form.
// If the input is already 3 letters, it passes through unchanged.
// If the input is a known 2-letter code, it maps to the 3-letter equivalent.
// Unknown codes pass through as-is.
func ToISO6392(lang string) string {
	if lang == "" {
		return ""
	}
	lower := strings.ToLower(lang)
	if len(lower) == 3 {
		return lower
	}
	if mapped, ok := iso639Map[lower]; ok {
		return mapped
	}
	return lower
}

// BuildAudioLabels derives a unique label for each audio track.
// Priority: Studio > Language > "Audio" fallback.
// Duplicate labels get ordinal suffixes " (2)", " (3)", etc.
func BuildAudioLabels(tracks []domain.AudioTrack) []string {
	labels := make([]string, len(tracks))
	for i, t := range tracks {
		switch {
		case t.Studio != "":
			labels[i] = t.Studio
		case t.Language != "":
			labels[i] = t.Language
		default:
			labels[i] = "Audio"
		}
	}
	return makeUnique(labels)
}

// BuildSubtitleLabels derives a unique label for each subtitle track.
// Priority: Source > Language > "Subtitle" fallback.
// Duplicate labels get ordinal suffixes " (2)", " (3)", etc.
func BuildSubtitleLabels(tracks []domain.SubtitleTrack) []string {
	labels := make([]string, len(tracks))
	for i, t := range tracks {
		switch {
		case t.Source != "":
			labels[i] = t.Source
		case t.Language != "":
			labels[i] = t.Language
		default:
			labels[i] = "Subtitle"
		}
	}
	return makeUnique(labels)
}

// makeUnique ensures all labels are unique by appending ordinal suffixes
// " (2)", " (3)", etc. to duplicates. The first occurrence keeps its original
// label; subsequent duplicates get increasing suffixes.
func makeUnique(labels []string) []string {
	// Count occurrences so duplicate labels can be given ordinal suffixes:
	// the first occurrence keeps the original; duplicates get " (2)", " (3)", etc.
	counts := make(map[string]int)
	for _, l := range labels {
		counts[l]++
	}

	seen := make(map[string]int)
	result := make([]string, len(labels))
	for i, l := range labels {
		seen[l]++
		if counts[l] > 1 && seen[l] > 1 {
			result[i] = fmt.Sprintf("%s (%d)", l, seen[l])
		} else {
			result[i] = l
		}
	}
	return result
}

// BuildFFmpegArgs constructs the full ffmpeg argument list for a download job.
// It is a pure function with no side effects.
//
// For HLS sources: the video URL is the first -i, then each audio and subtitle
// track with a separate URI gets its own -i.
// For progressive sources: a single -i contains all streams.
//
// The auth argument, when non-empty, injects a Cookie header, User-Agent, and
// any extra headers via ffmpeg's -headers / -user_agent input options so that
// ffmpeg's requests pass Cloudflare and kino.pub authentication. These options
// are applied before each -i so they affect every input.
//
// The function produces:
//   - Per-input -i flags (each preceded by auth options when provided)
//   - -map flags for video, audio, and subtitle tracks
//   - -c copy (stream copy, no re-encode)
//   - -metadata:s:a:N and -metadata:s:s:N for labels and languages
//   - -progress pipe:1 for progress reporting
//   - The temp output path as the final argument
func BuildFFmpegArgs(job domain.Job, proxyEnv []string, auth domain.RequestAuth, tempPath string, extraArgs []string) []string {
	media := job.Media
	isHLS := media.Source.Kind == domain.MediaHLS

	// inputOpts are options that must precede every -i (auth headers, UA).
	inputOpts := buildInputAuthOpts(auth)

	addInput := func(args []string, url string) []string {
		args = append(args, inputOpts...)
		args = append(args, "-i", url)
		return args
	}

	var args []string

	// Add -y to overwrite without asking.
	args = append(args, "-y")

	// Add proxy environment as protocol options if provided.
	for _, env := range proxyEnv {
		// proxyEnv entries are in the form "key=value" for env vars,
		// or "-http_proxy <url>" style args. We pass them as protocol options.
		if strings.HasPrefix(env, "-") {
			// Direct ffmpeg arg like -http_proxy <url>
			parts := strings.SplitN(env, " ", 2)
			args = append(args, parts...)
		}
	}

	// Build input list and track the input index for mapping.
	if isHLS {
		// HLS: video URL is first input.
		args = addInput(args, media.Source.URL)

		// Each audio track with a URI gets its own input.
		for range media.Audio {
			args = addInput(args, media.Source.URL)
		}

		// Each subtitle track gets its own input.
		for range media.Subtitles {
			args = addInput(args, media.Source.URL)
		}
	} else {
		// Progressive: single input contains all streams.
		args = addInput(args, media.Source.URL)
	}

	// Map video track: always from input 0, video stream.
	args = append(args, "-map", "0:v")

	// Map audio tracks.
	if isHLS {
		for i := range media.Audio {
			// Each audio is a separate input (starting at index 1).
			args = append(args, "-map", fmt.Sprintf("%d:a", i+1))
		}
	} else {
		// Progressive: all audio streams are in input 0.
		for i := range media.Audio {
			args = append(args, "-map", fmt.Sprintf("0:a:%d", i))
		}
	}

	// Map subtitle tracks.
	if isHLS {
		audioCount := len(media.Audio)
		for i := range media.Subtitles {
			// Subtitles start after audio inputs.
			args = append(args, "-map", fmt.Sprintf("%d:s", audioCount+1+i))
		}
	} else {
		for i := range media.Subtitles {
			args = append(args, "-map", fmt.Sprintf("0:s:%d", i))
		}
	}

	// Stream copy — no re-encoding.
	args = append(args, "-c", "copy")

	// Audio metadata: title and language.
	audioLabels := BuildAudioLabels(media.Audio)
	for i, track := range media.Audio {
		args = append(args, fmt.Sprintf("-metadata:s:a:%d", i), fmt.Sprintf("title=%s", audioLabels[i]))
		if track.Language != "" {
			lang := ToISO6392(track.Language)
			args = append(args, fmt.Sprintf("-metadata:s:a:%d", i), fmt.Sprintf("language=%s", lang))
		}
	}

	// Subtitle metadata: title and language.
	subtitleLabels := BuildSubtitleLabels(media.Subtitles)
	for i, track := range media.Subtitles {
		args = append(args, fmt.Sprintf("-metadata:s:s:%d", i), fmt.Sprintf("title=%s", subtitleLabels[i]))
		if track.Language != "" {
			lang := ToISO6392(track.Language)
			args = append(args, fmt.Sprintf("-metadata:s:s:%d", i), fmt.Sprintf("language=%s", lang))
		}
	}

	// Progress reporting to stdout pipe.
	args = append(args, "-progress", "pipe:1")

	// Output format must be specified explicitly because the temp file extension
	// (.mkv.tmp) is not recognized by ffmpeg's format auto-detection.
	outFormat := "matroska"
	if strings.HasSuffix(strings.TrimSuffix(tempPath, ".tmp"), ".mp4") {
		outFormat = "mp4"
	}

	// Container-level metadata (title, show, season/episode for media players).
	if job.Episode.Title != "" {
		args = append(args, "-metadata", fmt.Sprintf("title=%s", job.Episode.Title))
	}
	if job.SeriesTitle != "" {
		args = append(args, "-metadata", fmt.Sprintf("SHOW=%s", job.SeriesTitle))
	}
	// Series title from the episode key's context — we embed season/episode info
	// so media players (Plex, Kodi, VLC) can display it properly.
	args = append(args, "-metadata", fmt.Sprintf("episode_sort=%d", job.Episode.Key.Episode))
	args = append(args, "-metadata", fmt.Sprintf("season_number=%d", job.Episode.Key.Season))
	args = append(args, "-metadata", fmt.Sprintf("episode_id=S%02dE%02d", job.Episode.Key.Season, job.Episode.Key.Episode))

	// Attach poster image as cover art (MKV only — -attach is a Matroska feature).
	if job.PosterPath != "" && outFormat == "matroska" {
		args = append(args, "-attach", job.PosterPath)
		args = append(args, "-metadata:s:t:0", "mimetype=image/jpeg")
		args = append(args, "-metadata:s:t:0", "filename=cover.jpg")
	}

	// Extra user-supplied ffmpeg arguments (advanced: --ffmpeg-args / --x).
	// Inserted before -f and output path so they can override -c copy or add
	// filters/encoding options.
	if len(extraArgs) > 0 {
		args = append(args, extraArgs...)
	}

	args = append(args, "-f", outFormat)

	// Output path (temp file).
	args = append(args, tempPath)

	return args
}

// BuildRemuxArgs constructs ffmpeg arguments to remux a LOCAL media file
// (e.g. a concatenated HLS .ts) into the final container. Unlike
// BuildFFmpegArgs, it:
//   - takes a single local input (no auth options, no proxy)
//   - uses "-map 0" to copy EVERY stream (all video, all audio, all subtitles)
//   - relies on the muxed streams already present in the input file
//
// This is used by the HLS pipeline where the downloaded .ts already contains
// video + all audio tracks muxed together.
func BuildRemuxArgs(job domain.Job, localInput, tempPath string) []string {
	var args []string

	// Overwrite without asking.
	args = append(args, "-y")

	// Single local input — no auth, no proxy options.
	args = append(args, "-i", localInput)

	// Map ALL streams from the input (video + every audio + subtitles).
	args = append(args, "-map", "0")

	// Stream copy — no re-encoding.
	args = append(args, "-c", "copy")

	// Determine output format from the final extension.
	outFormat := "matroska"
	finalPath := strings.TrimSuffix(tempPath, ".tmp")
	if strings.HasSuffix(finalPath, ".mp4") {
		outFormat = "mp4"
	}

	// Container-level metadata.
	if job.Episode.Title != "" {
		args = append(args, "-metadata", fmt.Sprintf("title=%s", job.Episode.Title))
	}
	if job.SeriesTitle != "" {
		args = append(args, "-metadata", fmt.Sprintf("SHOW=%s", job.SeriesTitle))
	}
	args = append(args, "-metadata", fmt.Sprintf("episode_sort=%d", job.Episode.Key.Episode))
	args = append(args, "-metadata", fmt.Sprintf("season_number=%d", job.Episode.Key.Season))
	args = append(args, "-metadata", fmt.Sprintf("episode_id=S%02dE%02d", job.Episode.Key.Season, job.Episode.Key.Episode))

	// Attach poster as cover art (MKV only).
	if job.PosterPath != "" && outFormat == "matroska" {
		args = append(args, "-attach", job.PosterPath)
		args = append(args, "-metadata:s:t:0", "mimetype=image/jpeg")
		args = append(args, "-metadata:s:t:0", "filename=cover.jpg")
	}

	args = append(args, "-f", outFormat)
	args = append(args, tempPath)

	return args
}

// BuildHLSMuxArgs constructs ffmpeg arguments to mux a downloaded HLS video
// file together with separately-downloaded audio track files into the final
// container. Each audio file is a separate input; -c copy avoids re-encoding.
//
// Layout:
//   -i video.ts -i audio_0.ts -i audio_1.ts ...
//   -map 0:v -map 1:a -map 2:a ...
//   -c copy
//   -metadata:s:a:N title=... language=...
func BuildHLSMuxArgs(job domain.Job, hls *domain.HLSDownloadResult, tempPath string) []string {
	var args []string

	args = append(args, "-y")

	// Input 0: video.
	args = append(args, "-i", hls.VideoPath)

	// Inputs 1..N: audio tracks.
	for _, a := range hls.AudioTracks {
		args = append(args, "-i", a.Path)
	}

	// Subtitle muxing applies only when NOT in external/sidecar mode and there
	// are subtitle tracks to mux. In external mode subtitles are written as
	// sidecar .srt files separately, so the muxed output must be byte-identical
	// to the no-subtitle case (AC2b regression guard).
	muxSubs := !job.SubtitlesExternal && len(hls.Subtitles) > 0

	// Inputs N+1..M: subtitle tracks. Each is a LOCAL HLS playlist of .vtt
	// segments, which ffmpeg only reads with -allowed_extensions ALL -f hls
	// placed immediately before the -i.
	if muxSubs {
		for _, s := range hls.Subtitles {
			args = append(args, "-allowed_extensions", "ALL", "-f", "hls", "-i", s.Path)
		}
	}

	// Map video from input 0.
	args = append(args, "-map", "0:v:0")

	if len(hls.AudioTracks) > 0 {
		// Map each audio input's first audio stream.
		for i := range hls.AudioTracks {
			args = append(args, "-map", fmt.Sprintf("%d:a:0", i+1))
		}
	} else {
		// No separate audio — the video file may contain muxed audio. Map any
		// audio streams from input 0 if present (ignore failure with ?).
		args = append(args, "-map", "0:a?")
	}

	// Map subtitle streams. Subtitle inputs start after the video (1) and audio
	// inputs, so the first subtitle input index is 1 + len(AudioTracks).
	if muxSubs {
		subBase := 1 + len(hls.AudioTracks)
		for k := range hls.Subtitles {
			args = append(args, "-map", fmt.Sprintf("%d:s:0", subBase+k))
		}
	}

	// Stream copy.
	args = append(args, "-c", "copy")

	// Subtitle codec depends on the output container. Placed after -c copy so it
	// overrides only the subtitle streams. Only emitted when subtitles are muxed.
	// (outFormat is computed below; recompute the format decision here.)
	if muxSubs {
		subCodec := "srt"
		finalPath := strings.TrimSuffix(tempPath, ".tmp")
		if strings.HasSuffix(finalPath, ".mp4") {
			subCodec = "mov_text"
		}
		args = append(args, "-c:s", subCodec)
	}

	// Audio metadata: labels and languages.
	labels := make([]string, len(hls.AudioTracks))
	for i, a := range hls.AudioTracks {
		if a.Name != "" {
			labels[i] = a.Name
		} else if a.Language != "" {
			labels[i] = a.Language
		} else {
			labels[i] = "Audio"
		}
	}
	labels = makeUnique(labels)
	for i, a := range hls.AudioTracks {
		args = append(args, fmt.Sprintf("-metadata:s:a:%d", i), fmt.Sprintf("title=%s", labels[i]))
		if a.Language != "" {
			args = append(args, fmt.Sprintf("-metadata:s:a:%d", i), fmt.Sprintf("language=%s", ToISO6392(a.Language)))
		}
	}

	// Subtitle metadata: labels and languages. Labels prefer Name, fall back to
	// Language, then "Subtitle", deduped via makeUnique (mirrors BuildSubtitleLabels).
	if muxSubs {
		subLabels := subtitleTrackLabels(hls.Subtitles)
		for k, s := range hls.Subtitles {
			args = append(args, fmt.Sprintf("-metadata:s:s:%d", k), fmt.Sprintf("title=%s", subLabels[k]))
			if s.Language != "" {
				args = append(args, fmt.Sprintf("-metadata:s:s:%d", k), fmt.Sprintf("language=%s", ToISO6392(s.Language)))
			}
		}
	}

	// Output format.
	outFormat := "matroska"
	finalPath := strings.TrimSuffix(tempPath, ".tmp")
	if strings.HasSuffix(finalPath, ".mp4") {
		outFormat = "mp4"
	}

	// Container metadata.
	if job.Episode.Title != "" {
		args = append(args, "-metadata", fmt.Sprintf("title=%s", job.Episode.Title))
	}
	if job.SeriesTitle != "" {
		args = append(args, "-metadata", fmt.Sprintf("SHOW=%s", job.SeriesTitle))
	}
	args = append(args, "-metadata", fmt.Sprintf("episode_sort=%d", job.Episode.Key.Episode))
	args = append(args, "-metadata", fmt.Sprintf("season_number=%d", job.Episode.Key.Season))
	args = append(args, "-metadata", fmt.Sprintf("episode_id=S%02dE%02d", job.Episode.Key.Season, job.Episode.Key.Episode))

	// Poster as cover art (MKV only).
	if job.PosterPath != "" && outFormat == "matroska" {
		args = append(args, "-attach", job.PosterPath)
		args = append(args, "-metadata:s:t:0", "mimetype=image/jpeg")
		args = append(args, "-metadata:s:t:0", "filename=cover.jpg")
	}

	args = append(args, "-f", outFormat)
	args = append(args, tempPath)

	return args
}

// subtitleTrackLabels derives a unique label for each HLS subtitle track,
// preferring Name, falling back to Language, then "Subtitle". Duplicates get
// ordinal suffixes via makeUnique (mirrors BuildSubtitleLabels for the HLS
// subtitle track type).
func subtitleTrackLabels(tracks []domain.HLSSubtitleTrack) []string {
	labels := make([]string, len(tracks))
	for i, t := range tracks {
		switch {
		case t.Name != "":
			labels[i] = t.Name
		case t.Language != "":
			labels[i] = t.Language
		default:
			labels[i] = "Subtitle"
		}
	}
	return makeUnique(labels)
}

// BuildSubtitleSidecarArgs constructs ffmpeg arguments to extract a single
// subtitle track from a LOCAL HLS playlist (of .vtt segments) into a standalone
// .srt sidecar file. The input is local, so no auth/proxy options are needed.
//
// Layout:
//
//	-y -allowed_extensions ALL -f hls -i <playlistPath> -map 0:s:0 -c:s srt -f srt <outSrtPath>
func BuildSubtitleSidecarArgs(playlistPath, outSrtPath string) []string {
	return []string{
		"-y",
		"-allowed_extensions", "ALL",
		"-f", "hls",
		"-i", playlistPath,
		"-map", "0:s:0",
		"-c:s", "srt",
		"-f", "srt",
		outSrtPath,
	}
}

// SubtitleSidecarPath derives the sidecar .srt path for a subtitle track placed
// next to the container output. The result is
// "<containerOutPath without ext>.<lang>.srt"; when dedupSuffix is non-empty
// (used to disambiguate multiple tracks sharing a language), it is inserted as
// "<base>.<lang>.<dedupSuffix>.srt" so files do not collide.
func SubtitleSidecarPath(containerOutPath, lang string, dedupSuffix string) string {
	ext := filepath.Ext(containerOutPath)
	base := strings.TrimSuffix(containerOutPath, ext)
	if dedupSuffix != "" {
		return fmt.Sprintf("%s.%s.%s.srt", base, lang, dedupSuffix)
	}
	return fmt.Sprintf("%s.%s.srt", base, lang)
}
