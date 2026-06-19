package domain

import (
	"reflect"
	"testing"
)

func TestSelectSubtitlesStrict(t *testing.T) {
	tracks := []SubtitleTrackInfo{
		{Index: 0, Label: "Russian", Language: "rus"},
		{Index: 1, Label: "English SDH", Language: "eng"},
		{Index: 2, Label: "Forced", Language: "rus"},
	}

	tests := []struct {
		name string
		pref SubtitlePreference
		want []int
	}{
		{
			name: "empty preference keeps all (subs-only without --subs)",
			pref: SubtitlePreference{},
			want: []int{0, 1, 2},
		},
		{
			name: "include matches a single language",
			pref: SubtitlePreference{Include: []string{"eng"}},
			want: []int{1},
		},
		{
			name: "include matches multiple tracks of same language",
			pref: SubtitlePreference{Include: []string{"rus"}},
			want: []int{0, 2},
		},
		{
			name: "strict miss returns nil (no fallback, unlike SelectSubtitles)",
			pref: SubtitlePreference{Include: []string{"deu"}},
			want: nil,
		},
		{
			name: "exclude removes matching tracks",
			pref: SubtitlePreference{Exclude: []string{"eng"}},
			want: []int{0, 2},
		},
		{
			name: "exclude removing everything returns nil (no keep-all rescue)",
			pref: SubtitlePreference{Exclude: []string{"rus", "eng"}},
			want: nil,
		},
		{
			name: "include after exclude",
			pref: SubtitlePreference{Include: []string{"rus"}, Exclude: []string{"forced"}},
			want: []int{0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SelectSubtitlesStrict(tracks, tt.pref)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SelectSubtitlesStrict() = %v, want %v", got, tt.want)
			}
		})
	}
}

// SelectSubtitlesStrict must differ from SelectSubtitles precisely on the
// no-match case: the lenient selector falls back to a single best track, the
// strict selector returns nothing so the caller can surface an error.
func TestSelectSubtitlesStrict_DiffersFromLenientOnMiss(t *testing.T) {
	tracks := []SubtitleTrackInfo{
		{Index: 0, Label: "Russian", Language: "rus"},
		{Index: 1, Label: "English", Language: "eng"},
	}
	pref := SubtitlePreference{Include: []string{"jpn"}}

	if lenient := SelectSubtitles(tracks, pref); len(lenient) == 0 {
		t.Fatalf("precondition: SelectSubtitles should fall back to a track, got empty")
	}
	if strict := SelectSubtitlesStrict(tracks, pref); strict != nil {
		t.Errorf("SelectSubtitlesStrict should return nil on miss, got %v", strict)
	}
}

func TestSelectSubtitlesStrict_EmptyTracks(t *testing.T) {
	if got := SelectSubtitlesStrict(nil, SubtitlePreference{}); got != nil {
		t.Errorf("want nil for empty tracks, got %v", got)
	}
}
