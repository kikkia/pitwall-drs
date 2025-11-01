package api

import "time"

type RecordingTopThreeDriver struct {
	TLA          string `json:"tla"`
	RacingNumber string `json:"racingNumber"`
	TeamColour   string `json:"teamColour"`
}

type RecordingMetadata struct {
	SessionType     string                    `json:"sessionType"`
	FinishedAt      time.Time                 `json:"finishedAt"`
	TopThree        []RecordingTopThreeDriver `json:"topThree"`
	CountryFlagCode string                    `json:"countryFlagCode"`
}

// Structure returned by the /recordings API.
type RecordingInfo struct {
	Path            string                    `json:"path"`
	SessionType     string                    `json:"sessionType"`
	FinishedAt      time.Time                 `json:"finishedAt"`
	TopThree        []RecordingTopThreeDriver `json:"topThree"`
	CountryFlagCode string                    `json:"countryFlagCode"`
}
