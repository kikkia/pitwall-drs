package model

type Track struct {
	Lon      float64 `json:"lon"`
	Lat      float64 `json:"lat"`
	Location string  `json:"location"`
	Name     string  `json:"name"`
	ID       string  `json:"id"`
	PitDelta float64 `json:"pitDelta"`
}
