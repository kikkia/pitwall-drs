package model

import "time"

// Event represents a single F1 event.
type Event struct {
	UID         string    `json:"uid"`
	Location    string    `json:"location"`
	Summary     string    `json:"summary"`
	Description string    `json:"description"`
	StartTime   time.Time `json:"startTime"`
	EndTime     time.Time `json:"endTime"`
}

// SeasonSchedule holds all the events for a season.
type SeasonSchedule struct {
	Events []Event `json:"events"`
}