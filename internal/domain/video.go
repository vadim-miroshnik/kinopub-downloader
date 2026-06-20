package domain

// VideoTrackInfo describes a single video quality variant available for an
// episode. It is the lightweight, transport-agnostic view used by the
// interactive quality picker (the HLS layer builds these from its master
// playlist variants).
type VideoTrackInfo struct {
	// Index is the position within the variant list (0-based). ChooseVideo
	// returns one of these indices.
	Index int
	// Label is a human-readable description, e.g. "1080p/h265 (2500 kbps)".
	Label string
	// Quality is the selector string that reselects this variant for every
	// episode via the quality preference, e.g. "1080p-h265". It is assigned to
	// RunConfig.Quality after the user picks.
	Quality string
	// Height is the vertical resolution in pixels, e.g. 1080.
	Height int
	// BitrateKbps is the variant bitrate in kbps.
	BitrateKbps int
}
