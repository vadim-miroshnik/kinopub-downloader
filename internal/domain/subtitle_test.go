package domain

import (
	"reflect"
	"testing"
)

// Realistic subtitle track sets.
var (
	// Episode with Russian, English, and Japanese subtitles.
	subTracksA = []SubtitleTrackInfo{
		{Index: 0, Label: "Russian", Language: "rus"},
		{Index: 1, Label: "English SDH", Language: "eng"},
		{Index: 2, Label: "Japanese", Language: "jpn"},
	}
	// Episode where Russian moved to index 1 with a different label.
	subTracksB = []SubtitleTrackInfo{
		{Index: 0, Label: "English", Language: "eng"},
		{Index: 1, Label: "Русские субтитры", Language: "rus"},
		{Index: 2, Label: "Japanese", Language: "jpn"},
	}
	// Episode missing Russian subtitles entirely.
	subTracksC = []SubtitleTrackInfo{
		{Index: 0, Label: "English", Language: "eng"},
		{Index: 1, Label: "Japanese", Language: "jpn"},
	}
)

func TestSelectSubtitles_EmptyPrefKeepsAll(t *testing.T) {
	got := SelectSubtitles(subTracksA, SubtitlePreference{})
	want := []int{0, 1, 2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SelectSubtitles(empty pref) = %v, want %v", got, want)
	}
}

func TestSelectSubtitles_ExcludeDropsMatches(t *testing.T) {
	pref := SubtitlePreference{Exclude: []string{"jpn"}}
	got := SelectSubtitles(subTracksA, pref)
	want := []int{0, 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Exclude jpn: got %v, want %v", got, want)
	}

	// Exclude by label fragment.
	pref2 := SubtitlePreference{Exclude: []string{"sdh"}}
	got2 := SelectSubtitles(subTracksA, pref2)
	want2 := []int{0, 2}
	if !reflect.DeepEqual(got2, want2) {
		t.Fatalf("Exclude sdh: got %v, want %v", got2, want2)
	}
}

func TestSelectSubtitles_ExcludeAllIsIgnored(t *testing.T) {
	// Excluding every present language must be ignored to keep some subtitles.
	pref := SubtitlePreference{Exclude: []string{"rus", "eng", "jpn"}}
	got := SelectSubtitles(subTracksA, pref)
	if len(got) != len(subTracksA) {
		t.Fatalf("exclude-all should keep all: got %v", got)
	}
}

func TestSelectSubtitles_IncludeKeepsMatches(t *testing.T) {
	pref := SubtitlePreference{Include: []string{"eng"}}
	got := SelectSubtitles(subTracksA, pref)
	want := []int{1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Include eng: got %v, want %v", got, want)
	}
}

func TestSelectSubtitles_IncludeAcrossNamingDrift(t *testing.T) {
	// "rus" should match Language "rus" on subTracksA (index 0)
	// and Language "rus" on subTracksB (index 1).
	pref := SubtitlePreference{Include: []string{"rus"}, Prefer: []string{"rus"}}
	if got := SelectSubtitles(subTracksA, pref); !reflect.DeepEqual(got, []int{0}) {
		t.Errorf("subTracksA: got %v, want [0]", got)
	}
	if got := SelectSubtitles(subTracksB, pref); !reflect.DeepEqual(got, []int{1}) {
		t.Errorf("subTracksB: got %v, want [1]", got)
	}
}

func TestSelectSubtitles_IncludeMissFallsBackToBestByPrefer(t *testing.T) {
	// Russian subtitles are gone in subTracksC. Fallback must pick the eng
	// track (index 0) if Prefer is empty, or the one ranked by Prefer.
	pref := SubtitlePreference{Include: []string{"rus"}, Prefer: []string{"eng"}}
	got := SelectSubtitles(subTracksC, pref)
	// eng is preferred, it is at index 0.
	if !reflect.DeepEqual(got, []int{0}) {
		t.Fatalf("fallback with Prefer eng: got %v, want [0]", got)
	}

	// Without Prefer, falls back to highest in source list (index 0).
	pref2 := SubtitlePreference{Include: []string{"rus"}}
	got2 := SelectSubtitles(subTracksC, pref2)
	if !reflect.DeepEqual(got2, []int{0}) {
		t.Fatalf("fallback no Prefer: got %v, want [0]", got2)
	}
}

func TestSelectSubtitles_LanguageEqualityMatching(t *testing.T) {
	// Pattern "ru" should match Language "rus" via normLang.
	pref := SubtitlePreference{Include: []string{"ru"}}
	got := SelectSubtitles(subTracksA, pref)
	if !reflect.DeepEqual(got, []int{0}) {
		t.Fatalf("pattern 'ru' matching Language 'rus': got %v, want [0]", got)
	}

	// Pattern "russian" should also match via normLang.
	pref2 := SubtitlePreference{Include: []string{"russian"}}
	got2 := SelectSubtitles(subTracksA, pref2)
	if !reflect.DeepEqual(got2, []int{0}) {
		t.Fatalf("pattern 'russian' matching Language 'rus': got %v, want [0]", got2)
	}
}

func TestSelectSubtitles_EmptyTracks(t *testing.T) {
	got := SelectSubtitles(nil, SubtitlePreference{Include: []string{"rus"}})
	if got != nil {
		t.Fatalf("nil tracks should yield nil, got %v", got)
	}
}

func TestSubtitlePreference_IsAll(t *testing.T) {
	if !(SubtitlePreference{}).IsAll() {
		t.Error("zero SubtitlePreference should be IsAll")
	}
	if !(SubtitlePreference{Prefer: []string{"rus"}}).IsAll() {
		t.Error("Prefer-only SubtitlePreference should be IsAll (no filtering)")
	}
	if (SubtitlePreference{Include: []string{"x"}}).IsAll() {
		t.Error("Include SubtitlePreference should not be IsAll")
	}
	if (SubtitlePreference{Exclude: []string{"x"}}).IsAll() {
		t.Error("Exclude SubtitlePreference should not be IsAll")
	}
}

func TestBuildSubtitlePreference_FromChoice(t *testing.T) {
	// User picks Russian (index 0) on subTracksA.
	pref := BuildSubtitlePreference(subTracksA, []int{0})
	// Label "Russian" is a language word → falls back to lang "rus".
	if !reflect.DeepEqual(pref.Include, []string{"rus"}) {
		t.Fatalf("Include = %v, want [rus]", pref.Include)
	}
	if !reflect.DeepEqual(pref.Prefer, []string{"rus"}) {
		t.Fatalf("Prefer = %v, want [rus]", pref.Prefer)
	}

	// That preference selects Russian on both track sets.
	if got := SelectSubtitles(subTracksA, pref); !reflect.DeepEqual(got, []int{0}) {
		t.Errorf("subTracksA: got %v, want [0]", got)
	}
	if got := SelectSubtitles(subTracksB, pref); !reflect.DeepEqual(got, []int{1}) {
		t.Errorf("subTracksB: got %v, want [1]", got)
	}
	// Falls back to eng (index 0) on subTracksC where Russian is absent.
	if got := SelectSubtitles(subTracksC, pref); !reflect.DeepEqual(got, []int{0}) {
		t.Errorf("subTracksC fallback: got %v, want [0]", got)
	}
}

func TestBuildSubtitlePreference_DistinctiveLabel(t *testing.T) {
	// A track with a distinctive non-language label token.
	tracks := []SubtitleTrackInfo{
		{Index: 0, Label: "Netflix Russian", Language: "rus"},
		{Index: 1, Label: "English", Language: "eng"},
	}
	pref := BuildSubtitlePreference(tracks, []int{0})
	// "Netflix" is distinctive; "Russian" is a stopword → Include = ["Netflix"].
	if !reflect.DeepEqual(pref.Include, []string{"Netflix"}) {
		t.Fatalf("Include = %v, want [Netflix]", pref.Include)
	}
	if !reflect.DeepEqual(pref.Prefer, []string{"rus"}) {
		t.Fatalf("Prefer = %v, want [rus]", pref.Prefer)
	}
}

func TestExtractSubtitleKeywords_FallsBackToLang(t *testing.T) {
	// Pure language-word label with no distinctive token.
	track := SubtitleTrackInfo{Label: "Russian", Language: "rus"}
	got := ExtractSubtitleKeywords(track)
	if !reflect.DeepEqual(got, []string{"rus"}) {
		t.Fatalf("ExtractSubtitleKeywords(%q) = %v, want [rus]", track.Label, got)
	}
}

func TestExtractSubtitleKeywords_DistinctiveToken(t *testing.T) {
	track := SubtitleTrackInfo{Label: "Netflix Russian", Language: "rus"}
	got := ExtractSubtitleKeywords(track)
	if !reflect.DeepEqual(got, []string{"Netflix"}) {
		t.Fatalf("ExtractSubtitleKeywords(%q) = %v, want [Netflix]", track.Label, got)
	}
}
