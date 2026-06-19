package domain

import (
	"sort"
	"strconv"
	"strings"
)

// SubtitleTrackInfo describes a single subtitle rendition available for an episode.
// It is the lightweight, transport-agnostic view used by subtitle selection and
// the interactive picker (the HLS layer builds these from its renditions).
type SubtitleTrackInfo struct {
	// Index is the position of the track within the episode's subtitle list
	// (0-based). SelectSubtitles returns these indices.
	Index int
	// Label is the human label from the source, e.g. "Russian" or "English SDH".
	Label string
	// Language is the raw language tag from the source, e.g. "rus", "eng".
	// It may be empty when the source only encodes the language in Label.
	Language string
}

// SubtitlePreference describes which subtitle tracks to keep for a download run.
//
// Selection is substring-based and case-insensitive so it survives the naming
// drift seen across episodes.
//
// The zero value (no Include, no Exclude) means "keep every track".
type SubtitlePreference struct {
	// Include lists patterns to keep. A track is kept when its label or language
	// contains any Include pattern (case-insensitive). When Include is empty,
	// every track is kept (subject to Exclude).
	Include []string
	// Exclude lists patterns to drop. A track is dropped when its label or
	// language contains any Exclude pattern (case-insensitive). Exclude is
	// applied before Include. If excluding would remove every track, the
	// exclusion is ignored so the output always carries subtitles.
	Exclude []string
	// Prefer lists language hints used only to rank the fallback track. When
	// Include matches nothing in a given episode, the highest-ranked remaining
	// track is chosen, preferring tracks whose language matches a Prefer hint;
	// ties break toward the track highest in the source list.
	Prefer []string
}

// IsAll reports whether the preference keeps every available track unchanged.
func (p SubtitlePreference) IsAll() bool {
	return len(p.Include) == 0 && len(p.Exclude) == 0
}

// subtitleMatches reports whether track t matches pattern. A match occurs when
// the track's combined label+language contains the pattern (case-insensitive),
// or when their canonical languages are equal (so "rus" matches a Language "ru"
// as well as a label containing "Russian").
func subtitleMatches(t SubtitleTrackInfo, pattern string) bool {
	p := strings.ToLower(strings.TrimSpace(pattern))
	if p == "" {
		return false
	}
	hay := strings.ToLower(t.Label + " " + t.Language)
	if strings.Contains(hay, p) {
		return true
	}
	if tl := normLang(t.Language); tl != "" && tl == normLang(p) {
		return true
	}
	return false
}

// subtitleMatchesAny reports whether t matches any of the patterns.
func subtitleMatchesAny(t SubtitleTrackInfo, patterns []string) bool {
	for _, p := range patterns {
		if subtitleMatches(t, p) {
			return true
		}
	}
	return false
}

// subtitlePreferRank returns the rank of a track against the Prefer hints: a
// lower value means a stronger preference. Tracks matching no hint rank last.
func subtitlePreferRank(t SubtitleTrackInfo, prefer []string) int {
	for i, p := range prefer {
		if subtitleMatches(t, p) {
			return i
		}
	}
	return len(prefer)
}

// SelectSubtitles resolves which subtitle tracks to keep for an episode given a
// preference. It returns the indices (into tracks) to download, in ascending
// order. The result is deterministic and never empty unless tracks is empty.
//
// Algorithm:
//  1. Drop tracks matching any Exclude pattern. If that removes everything, the
//     exclusion is ignored (a download must keep some subtitles).
//  2. If Include is empty, keep all remaining tracks.
//  3. Otherwise keep the remaining tracks that match any Include pattern.
//  4. If Include matched nothing (the desired subtitle is missing this episode),
//     fall back to a single best remaining track: prefer tracks matching a
//     Prefer hint, then the one highest in the source list.
func SelectSubtitles(tracks []SubtitleTrackInfo, pref SubtitlePreference) []int {
	if len(tracks) == 0 {
		return nil
	}

	// 1. Apply excludes.
	remaining := make([]int, 0, len(tracks))
	for i, t := range tracks {
		if !subtitleMatchesAny(t, pref.Exclude) {
			remaining = append(remaining, i)
		}
	}
	if len(remaining) == 0 {
		// Excludes nuked everything — keep all so the output still has subtitles.
		remaining = remaining[:0]
		for i := range tracks {
			remaining = append(remaining, i)
		}
	}

	// 2. No positive filter → keep everything that survived excludes.
	if len(pref.Include) == 0 {
		return remaining
	}

	// 3. Keep includes among the remaining tracks.
	matched := make([]int, 0, len(remaining))
	for _, i := range remaining {
		if subtitleMatchesAny(tracks[i], pref.Include) {
			matched = append(matched, i)
		}
	}
	if len(matched) > 0 {
		return matched
	}

	// 4. Fallback: pick the single best remaining track.
	best := append([]int(nil), remaining...)
	sort.SliceStable(best, func(a, b int) bool {
		ra, rb := subtitlePreferRank(tracks[best[a]], pref.Prefer), subtitlePreferRank(tracks[best[b]], pref.Prefer)
		if ra != rb {
			return ra < rb
		}
		return best[a] < best[b]
	})
	return []int{best[0]}
}

// SelectSubtitlesStrict resolves which subtitle tracks to keep for an episode
// like SelectSubtitles, but WITHOUT the never-empty guarantees. It is used by
// the --subs-only mode, where a requested subtitle that is missing from an
// episode must surface as an error rather than silently downloading a different
// track.
//
// Differences from SelectSubtitles:
//   - When Exclude removes every track, the result is empty (no keep-all rescue).
//   - When Include matches nothing among the survivors, the result is empty
//     (no single-best fallback).
//
// As before, an empty Include keeps every track that survives Exclude, so
// "--subs-only" with no "--subs" downloads all available subtitles. The result
// indices are in ascending source order.
func SelectSubtitlesStrict(tracks []SubtitleTrackInfo, pref SubtitlePreference) []int {
	if len(tracks) == 0 {
		return nil
	}

	// Apply excludes. Unlike SelectSubtitles, an exclusion that removes
	// everything is honoured (the caller treats an empty result as "no match").
	remaining := make([]int, 0, len(tracks))
	for i, t := range tracks {
		if !subtitleMatchesAny(t, pref.Exclude) {
			remaining = append(remaining, i)
		}
	}
	if len(remaining) == 0 {
		return nil
	}

	// No positive filter → keep everything that survived excludes.
	if len(pref.Include) == 0 {
		return remaining
	}

	// Keep only the includes; no fallback when nothing matches.
	var matched []int
	for _, i := range remaining {
		if subtitleMatchesAny(tracks[i], pref.Include) {
			matched = append(matched, i)
		}
	}
	return matched
}

// subtitleStopwords are non-distinctive tokens in subtitle track labels.
var subtitleStopwords = map[string]bool{
	// Descriptor words that appear across many tracks.
	"sdh": true, "cc": true, "forced": true, "full": true,
	"subtitles": true, "subtitle": true, "subs": true, "sub": true,
	// Language words that are handled via the language fallback.
	"russian": true, "english": true, "japanese": true, "ukrainian": true,
	"german": true, "french": true, "spanish": true, "italian": true,
	"korean": true, "chinese": true,
	"русский": true, "английский": true, "японский": true, "украинский": true,
}

// subtitleSplitter splits a label into tokens on punctuation and whitespace.
func subtitleSplitter(r rune) bool {
	switch r {
	case '.', ',', '(', ')', '[', ']', '/', '\\', ':', ';', '-', '_', ' ', '\t':
		return true
	}
	return false
}

// ExtractSubtitleKeywords reduces a subtitle track label to the distinctive
// substrings that identify it, suitable for use as Include patterns. It strips
// stopword descriptors and bare language words. When nothing distinctive remains,
// it falls back to the canonical language.
func ExtractSubtitleKeywords(track SubtitleTrackInfo) []string {
	var keywords []string
	seen := make(map[string]bool)
	for _, tok := range strings.FieldsFunc(track.Label, subtitleSplitter) {
		low := strings.ToLower(tok)
		if low == "" || subtitleStopwords[low] {
			continue
		}
		if _, err := strconv.Atoi(low); err == nil {
			continue // pure number
		}
		if _, ok := langAliases[low]; ok {
			continue // bare language code/word handled via fallback
		}
		if len([]rune(low)) < 2 {
			continue
		}
		if !seen[low] {
			seen[low] = true
			keywords = append(keywords, tok)
		}
	}
	if len(keywords) > 0 {
		return keywords
	}
	// No distinctive token — fall back to language so the choice still
	// targets a specific track.
	lang := normLang(track.Language)
	if lang == "" {
		lang = parseTrailingLang(track.Label)
	}
	if lang != "" {
		return []string{lang}
	}
	return nil
}

// BuildSubtitlePreference constructs a SubtitlePreference that keeps exactly
// the chosen tracks across episodes. It derives Include patterns from the chosen
// tracks' distinctive label keywords and Prefer hints from their languages so a
// subtitle missing in some episode falls back to another track in the same
// language.
func BuildSubtitlePreference(tracks []SubtitleTrackInfo, chosen []int) SubtitlePreference {
	var include, prefer []string
	seenInc := make(map[string]bool)
	seenPref := make(map[string]bool)
	for _, idx := range chosen {
		if idx < 0 || idx >= len(tracks) {
			continue
		}
		for _, kw := range ExtractSubtitleKeywords(tracks[idx]) {
			key := strings.ToLower(kw)
			if !seenInc[key] {
				seenInc[key] = true
				include = append(include, kw)
			}
		}
		lang := normLang(tracks[idx].Language)
		if lang == "" {
			lang = parseTrailingLang(tracks[idx].Label)
		}
		if lang != "" && !seenPref[lang] {
			seenPref[lang] = true
			prefer = append(prefer, lang)
		}
	}
	return SubtitlePreference{Include: include, Prefer: prefer}
}
