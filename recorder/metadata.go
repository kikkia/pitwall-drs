package recorder

import (
	"f1sockets/api"
	"f1sockets/model"
	"fmt"
	"time"
)

// GenerateMetadataFromState extracts relevant session metadata from the final GlobalState.
func GenerateMetadataFromState(state *model.GlobalState) (*api.RecordingMetadata, error) {
	if state == nil {
		return nil, fmt.Errorf("cannot generate metadata from nil state")
	}

	meta := &api.RecordingMetadata{}

	if state.R.SessionInfo != nil {
		meta.SessionType = state.R.SessionInfo.Type
		if state.R.SessionInfo.Meeting.Country.Code != "" {
			meta.CountryFlagCode = state.R.SessionInfo.Meeting.Country.Code
		}
	}

	if state.R.SessionData != nil {
		for i := len(state.R.SessionData.StatusSeries) - 1; i >= 0; i-- {
			statusChange := state.R.SessionData.StatusSeries[i]
			if statusChange.SessionStatus == "Finalised" || statusChange.SessionStatus == "Ends" {
				finishedTime, err := time.Parse(time.RFC3339, statusChange.Utc)

				if err == nil {
					meta.FinishedAt = finishedTime
					break
				}
			}
		}
	}

	if state.R.TopThree != nil && len(state.R.TopThree.Lines) > 0 {
		meta.TopThree = make([]api.RecordingTopThreeDriver, 0, 3)
		for i, driverLine := range state.R.TopThree.Lines {
			if i >= 3 {
				break
			}
			driver := api.RecordingTopThreeDriver{
				TLA:          driverLine.Tla,
				RacingNumber: driverLine.RacingNumber,
				TeamColour:   driverLine.TeamColour,
			}
			meta.TopThree = append(meta.TopThree, driver)
		}
	}

	return meta, nil
}
