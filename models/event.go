package models

// TrackRequest is the payload for POST /track (single event).
type TrackRequest struct {
	Event      string         `json:"event"`                 // e.g. "agent_created"
	DistinctID string         `json:"distinct_id"`           // user / entity identifier
	Properties map[string]any `json:"properties,omitempty"`  // arbitrary k-v pairs
}

// BatchTrackRequest is the payload for POST /track/batch (multiple events).
type BatchTrackRequest struct {
	Events []TrackRequest `json:"events"`
}

// InternalEvent is the struct pushed into the pipeline channel.
// JSON tags used for spill/DLQ persistence.
type InternalEvent struct {
	Event      string         `json:"event"`
	DistinctID string         `json:"distinct_id"`
	Properties map[string]any `json:"properties,omitempty"`
}

// SuccessResponse is the standard 202 response body.
// Pre-serialised in handlers to avoid per-request allocation.
type SuccessResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// ErrorResponse is returned on validation / auth failures.
type ErrorResponse struct {
	Error string `json:"error"`
}

// ProfileRequest is the payload for POST /profile (set user profile in Mixpanel People).
// Use "ip" in properties for geolocation; Mixpanel will set $city, $region, $country_code from it.
type ProfileRequest struct {
	DistinctID string         `json:"distinct_id"`
	Properties map[string]any `json:"properties,omitempty"`
}
