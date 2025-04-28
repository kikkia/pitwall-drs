package model

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"sync"
)

type Heartbeat struct {
	UTC string `json:"Utc"`
}

type ExtrapolatedClock struct {
	UTC           string `json:"Utc"`
	Remaining     string `json:"Remaining"`
	Extrapolating bool   `json:"Extrapolating"`
}

type WeatherData struct {
	AirTemp       string `json:"AirTemp"`
	Humidity      string `json:"Humidity"`
	Pressure      string `json:"Pressure"`
	Rainfall      string `json:"Rainfall"`
	TrackTemp     string `json:"TrackTemp"`
	WindDirection string `json:"WindDirection"`
	WindSpeed     string `json:"WindSpeed"`
}

type TrackStatus struct {
	Status  string `json:"Status"`
	Message string `json:"Message"`
}

type DriverInfo struct {
	RacingNumber  string `json:"RacingNumber"`
	BroadcastName string `json:"BroadcastName"`
	FullName      string `json:"FullName"`
	Tla           string `json:"Tla"`  // Three Letter Acronym
	Line          int    `json:"Line"` // Appears to be an ordering or grid line number
	TeamName      string `json:"TeamName"`
	TeamColour    string `json:"TeamColour"` // Hex color code without #
	FirstName     string `json:"FirstName"`
	LastName      string `json:"LastName"`
	Reference     string `json:"Reference"` // Unique identifier
	HeadshotUrl   string `json:"HeadshotUrl"`
}

type TopThreeData struct {
	SessionPart int            `json:"SessionPart"`
	Withheld    bool           `json:"Withheld"`
	Lines       []TopThreeLine `json:"Lines"`
}

type TopThreeLine struct {
	Position        string `json:"Position"`
	ShowPosition    bool   `json:"ShowPosition"`
	RacingNumber    string `json:"RacingNumber"`
	Tla             string `json:"Tla"` // HAM, VER, BOT for example
	BroadcastName   string `json:"BroadcastName"`
	FullName        string `json:"FullName"`
	FirstName       string `json:"FirstName"`
	LastName        string `json:"LastName"`
	Reference       string `json:"Reference"`
	Team            string `json:"Team"` // Note: Field name is "Team", not "TeamName" here
	TeamColour      string `json:"TeamColour"`
	LapTime         string `json:"LapTime"`      // Format "M:SS.fff"
	LapState        int    `json:"LapState"`     // Flag indicating lap validity/status?
	DiffToAhead     string `json:"DiffToAhead"`  // Format "+S.fff"
	DiffToLeader    string `json:"DiffToLeader"` // Format "+S.fff"
	OverallFastest  bool   `json:"OverallFastest"`
	PersonalFastest bool   `json:"PersonalFastest"`
}

type TimingStatsData struct {
	Withheld    bool                         `json:"Withheld"`
	Lines       map[string]DriverTimingStats `json:"Lines"` // Keyed by driver number
	SessionType string                       `json:"SessionType,omitempty"`
}

type DriverTimingStats struct {
	Line                int                 `json:"Line"`
	RacingNumber        string              `json:"RacingNumber"`
	PersonalBestLapTime TimeAndLapInfo      `json:"PersonalBestLapTime"`
	BestSectors         []SectorOrSpeedInfo `json:"BestSectors"` // Index 0=S1, 1=S2, 2=S3
	BestSpeeds          BestSpeedsMap       `json:"BestSpeeds"`
}

type TimeAndLapInfo struct {
	Value    string `json:"Value"`         // Time string e.g., "M:SS.fff"
	Lap      int    `json:"Lap,omitempty"` // Lap number, seems optional sometimes
	Position int    `json:"Position"`      // Position relative to others for this metric
}

type SectorOrSpeedInfo struct {
	Value    string `json:"Value"`    // Time string or Speed string
	Position int    `json:"Position"` // Position relative to others for this metric
}

type BestSpeedsMap struct {
	I1 SectorOrSpeedInfo `json:"I1"` // Intermediate 1
	I2 SectorOrSpeedInfo `json:"I2"` // Intermediate 2
	FL SectorOrSpeedInfo `json:"FL"` // Finish Line
	ST SectorOrSpeedInfo `json:"ST"` // Speed Trap
}

type TimingAppData struct {
	Lines map[string]DriverTimingAppData `json:"Lines"` // Keyed by driver number
}

type DriverTimingAppData struct {
	RacingNumber string      `json:"RacingNumber"`
	Line         int         `json:"Line"`
	Stints       []StintInfo `json:"Stints"`
}

type StintInfo struct {
	LapFlags        int    `json:"LapFlags"`            // TODO: Map
	Compound        string `json:"Compound"`            // e.g., "SOFT", "MEDIUM", "HARD", "INTERMEDIATE", "WET"
	New             string `json:"New"`                 // String "true" or "false" - but f1 sends as string :/
	TyresNotChanged string `json:"TyresNotChanged"`     // String "0" or "1" - f1 sends int but its a bool :/
	TotalLaps       int    `json:"TotalLaps"`           // Laps in this stint
	StartLaps       int    `json:"StartLaps"`           // Lap number stint started on
	LapTime         string `json:"LapTime,omitempty"`   // Fastest lap time during this stint?
	LapNumber       int    `json:"LapNumber,omitempty"` // Lap number of fastest lap?
}

type TyreStint struct {
	Compound        string `json:"Compound"`        // e.g., "SOFT", "MEDIUM", "HARD", "INTERMEDIATE", "WET"
	New             string `json:"New"`             // String "true" or "false" - but f1 sends as string :/
	TyresNotChanged string `json:"TyresNotChanged"` // String "0" or "1" - f1 sends int but its a bool :/
	TotalLaps       int    `json:"TotalLaps"`       // Laps in this stint
	StartLaps       int    `json:"StartLaps"`       // Lap number stint started on
}

type RaceControlData struct {
	Messages []RaceControlMessage `json:"Messages"`
}

type RaceControlMessage struct {
	Utc      string `json:"Utc"`
	Category string `json:"Category"` // e.g., "Other", "Flag", "Drs"
	Message  string `json:"Message"`
	Flag     string `json:"Flag,omitempty"`   // e.g., "GREEN", "YELLOW", "CLEAR", "RED"
	Scope    string `json:"Scope,omitempty"`  // e.g., "Track", "Sector", "Driver"
	Sector   int    `json:"Sector,omitempty"` // Which sector if Scope is Sector
	// Mapped to driver data frontend side
	RacingNumber string `json:"RacingNumber,omitempty"`
}

type SessionInfoData struct {
	Meeting       MeetingInfo       `json:"Meeting"`
	ArchiveStatus ArchiveStatusInfo `json:"ArchiveStatus"`
	Key           int               `json:"Key"`
	Type          string            `json:"Type"` // e.g., "Qualifying", "Race", "Practice"
	Name          string            `json:"Name"` // e.g., "Qualifying", "FP1"
	StartDate     string            `json:"StartDate"`
	EndDate       string            `json:"EndDate"`
	GmtOffset     string            `json:"GmtOffset"` // Format "HH:MM:SS"
	Path          string            `json:"Path"`
}

type MeetingInfo struct {
	Key          int         `json:"Key"`
	Name         string      `json:"Name"`
	OfficialName string      `json:"OfficialName"`
	Location     string      `json:"Location"`
	Country      CountryInfo `json:"Country"`
	Circuit      CircuitInfo `json:"Circuit"`
	Number       int         `json:"Number"`
}

type CountryInfo struct {
	Key  int    `json:"Key"`
	Code string `json:"Code"`
	Name string `json:"Name"`
}

type CircuitInfo struct {
	Key       int    `json:"Key"`
	ShortName string `json:"ShortName"`
}

type ArchiveStatusInfo struct {
	Status string `json:"Status"` // e.g., "Complete", "Ongoing"
}

type SessionData struct {
	Series       []QualifyingPartInfo `json:"Series"`
	StatusSeries []StatusChangeInfo   `json:"StatusSeries"` // Timeline of session/track status changes
}

type QualifyingPartInfo struct {
	Utc            string `json:"Utc"`
	QualifyingPart int    `json:"QualifyingPart"` // 0, 1, 2, 3
}

type StatusChangeInfo struct {
	Utc           string `json:"Utc"`
	TrackStatus   string `json:"TrackStatus,omitempty"`   // e.g., "Yellow", "AllClear", "Red"
	SessionStatus string `json:"SessionStatus,omitempty"` // e.g., "Aborted", "Inactive", "Started", "Finished", "Finalised", "Ends"
}

type TimingData struct {
	NoEntries   []int                       `json:"NoEntries"` // [20, 15, 10] Q1/Q2/Q3 driver amounts
	SessionPart int                         `json:"SessionPart"`
	Lines       map[string]DriverTimingData `json:"Lines"` // Keyed by driver number
}

type DriverTimingData struct {
	KnockedOut       bool             `json:"KnockedOut"`
	Cutoff           bool             `json:"Cutoff"`       // Seems to be referencing 7% quali cutoff?
	BestLapTimes     []TimeAndLapInfo `json:"BestLapTimes"` // Best time in each session part (Q1/Q2/Q3)
	Stats            []QualifyingStat `json:"Stats"`        // Diff stats for each session part (Q1/Q2/Q3)
	Line             int              `json:"Line"`         // Grid position
	Position         string           `json:"Position"`
	ShowPosition     bool             `json:"ShowPosition"`
	RacingNumber     string           `json:"RacingNumber"`
	Retired          bool             `json:"Retired"`
	InPit            bool             `json:"InPit"`
	PitOut           bool             `json:"PitOut"`  // Just left pits
	Stopped          bool             `json:"Stopped"` // Stopped on track
	Status           int              `json:"Status"`  // TODO: map
	Sectors          []SectorTiming   `json:"Sectors"` // Current/last lap sector info (Index 0=S1, 1=S2, 2=S3)
	Speeds           SpeedsTimingMap  `json:"Speeds"`  // Current/last lap speeds info
	BestLapTime      TimeAndLapInfo   `json:"BestLapTime"`
	LastLapTime      LapTimeInfo      `json:"LastLapTime"`
	NumberOfLaps     int              `json:"NumberOfLaps"`
	NumberOfPitStops int              `json:"NumberOfPitStops"`
}

type QualifyingStat struct {
	TimeDiffToFastest       string `json:"TimeDiffToFastest"`       // e.g., "+S.fff" or empty
	TimeDifftoPositionAhead string `json:"TimeDifftoPositionAhead"` // e.g., "+S.fff" or empty
}

type SectorTiming struct {
	Stopped         bool            `json:"Stopped"`
	Value           string          `json:"Value"`  // Sector time string "SS.fff", empty if not set/valid
	Status          int             `json:"Status"` // TODO: map
	OverallFastest  bool            `json:"OverallFastest"`
	PersonalFastest bool            `json:"PersonalFastest"`
	Segments        []SegmentStatus `json:"Segments"`                // Mini-sector status flags
	PreviousValue   string          `json:"PreviousValue,omitempty"` // Store previous value for comparison
}

type SegmentStatus struct {
	Status int `json:"Status"` // Todo: Map
}

type SpeedsTimingMap struct {
	I1 SpeedInfo `json:"I1"` // Intermediate 1 speed
	I2 SpeedInfo `json:"I2"` // Intermediate 2 speed
	FL SpeedInfo `json:"FL"` // Finish Line speed
	ST SpeedInfo `json:"ST"` // Speed Trap speed
}

type SpeedInfo struct {
	Value           string `json:"Value"`
	Status          int    `json:"Status"` // TODO: Map
	OverallFastest  bool   `json:"OverallFastest"`
	PersonalFastest bool   `json:"PersonalFastest"`
}

type LapTimeInfo struct {
	Value           string `json:"Value"`  // Lap time string "M:SS.fff", empty if not set/valid
	Status          int    `json:"Status"` // TODO: map
	OverallFastest  bool   `json:"OverallFastest"`
	PersonalFastest bool   `json:"PersonalFastest"`
	Lap             int    `json:"Lap,omitempty"`
}

type TyreStintSeries struct {
	Stints map[string][]TyreStint `json:"Stints"`
}

type LapCount struct {
	CurrentLap int `json:CurrentLap`
	TotalLaps  int `json:TotalLaps`
}

type RaceData struct {
	Heartbeat           *Heartbeat         `json:"Heartbeat,omitempty"`
	ExtrapolatedClock   *ExtrapolatedClock `json:"ExtrapolatedClock,omitempty"`
	WeatherData         *WeatherData       `json:"WeatherData,omitempty"`
	TrackStatus         *TrackStatus       `json:"TrackStatus,omitempty"`
	TopThree            *TopThreeData      `json:"TopThree,omitempty"`
	TimingStats         *TimingStatsData   `json:"TimingStats,omitempty"`
	TimingAppData       *TimingAppData     `json:"TimingAppData,omitempty"`
	RaceControlMessages *RaceControlData   `json:"RaceControlMessages,omitempty"`
	SessionInfo         *SessionInfoData   `json:"SessionInfo,omitempty"`
	SessionData         *SessionData       `json:"SessionData,omitempty"`
	TimingData          *TimingData        `json:"TimingData,omitempty"`
	TyreStintSeries     *TyreStintSeries   `json:"TyreStintSeries,omitempty"`

	DriverList map[string]DriverInfo `json:"DriverList,omitempty"`
	CarDataZ   string                `json:"CarData.z,omitempty"`
	PositionZ  string                `json:"Position.z,omitempty"`
	LapCount   *LapCount             `json:LapCount,omitempty`
}

type GlobalState struct {
	R  RaceData `json:"R"`
	mu sync.RWMutex
}

// Intermediate struct specifically for marshalling to match F1's R object format
type raceDataForJSON struct {
	Heartbeat           *Heartbeat            `json:"Heartbeat,omitempty"`
	CarDataZ            string                `json:"CarData.z,omitempty"`  // Correct tag for output
	PositionZ           string                `json:"Position.z,omitempty"` // Correct tag for output
	ExtrapolatedClock   *ExtrapolatedClock    `json:"ExtrapolatedClock,omitempty"`
	TopThree            *TopThreeData         `json:"TopThree,omitempty"`
	TimingStats         *TimingStatsData      `json:"TimingStats,omitempty"`
	TimingAppData       *TimingAppData        `json:"TimingAppData,omitempty"`
	WeatherData         *WeatherData          `json:"WeatherData,omitempty"`
	TrackStatus         *TrackStatus          `json:"TrackStatus,omitempty"`
	DriverList          map[string]DriverInfo `json:"DriverList,omitempty"`
	RaceControlMessages *RaceControlData      `json:"RaceControlMessages,omitempty"`
	SessionInfo         *SessionInfoData      `json:"SessionInfo,omitempty"`
	SessionData         *SessionData          `json:"SessionData,omitempty"`
	TimingData          *TimingData           `json:"TimingData,omitempty"`
	TyreStintSeries     *TyreStintSeries      `json:"TyreStintSeries,omitempty"`
	LapCount            *LapCount             `json:LapCount,omitempty`
}

func NewEmptyGlobalState() *GlobalState {
	return &GlobalState{
		R: RaceData{
			Heartbeat:         &Heartbeat{},
			ExtrapolatedClock: &ExtrapolatedClock{},
			WeatherData:       &WeatherData{},
			TrackStatus:       &TrackStatus{},
			SessionInfo:       &SessionInfoData{},
			TopThree: &TopThreeData{
				Lines: make([]TopThreeLine, 0),
			},
			TimingStats: &TimingStatsData{
				Lines: make(map[string]DriverTimingStats),
			},
			TimingAppData: &TimingAppData{
				Lines: make(map[string]DriverTimingAppData),
			},
			RaceControlMessages: &RaceControlData{
				Messages: make([]RaceControlMessage, 0),
			},
			SessionData: &SessionData{
				Series:       make([]QualifyingPartInfo, 0),
				StatusSeries: make([]StatusChangeInfo, 0),
			},
			TimingData: &TimingData{
				Lines:     make(map[string]DriverTimingData),
				NoEntries: make([]int, 0),
			},

			DriverList: make(map[string]DriverInfo),
			LapCount:   &LapCount{},
		},
	}
}

func NewGlobalState(initialJsonData []byte) (*GlobalState, error) {
	newState := NewEmptyGlobalState()

	var topLevel map[string]json.RawMessage
	if err := json.Unmarshal(initialJsonData, &topLevel); err != nil {
		return nil, fmt.Errorf("NewGlobalState failed: could not unmarshal top level JSON: %w", err)
	}

	rawR, rExists := topLevel["R"]
	if !rExists {
		fmt.Println("Warning: 'R' field missing in initial JSON data for NewGlobalState.")
		return newState, nil
	}

	var rLevel map[string]json.RawMessage
	if err := json.Unmarshal(rawR, &rLevel); err != nil {
		return nil, fmt.Errorf("NewGlobalState failed: could not unmarshal 'R' field content: %w", err)
	}

	unmarshalField := func(key string, target interface{}) error {
		if raw, ok := rLevel[key]; ok {
			if key == "DriverList" {
				var intermediateDriverList map[string]json.RawMessage
				if err := json.Unmarshal(raw, &intermediateDriverList); err != nil {
					return fmt.Errorf("NewGlobalState failed: could not unmarshal R.DriverList into intermediate map: %w", err)
				}

				// Remove this annoying key
				delete(intermediateDriverList, "_kf")

				cleanedRaw, err := json.Marshal(intermediateDriverList)
				if err != nil {
					return fmt.Errorf("NewGlobalState failed: could not marshal cleaned DriverList: %w", err)
				}

				err = json.Unmarshal(cleanedRaw, target)
				if err != nil {
					return fmt.Errorf("NewGlobalState failed: could not unmarshal cleaned R.DriverList into target: %w", err)
				}
				return nil
			}

			err := json.Unmarshal(raw, target)
			if err != nil {
				return fmt.Errorf("NewGlobalState failed: could not unmarshal R.%s: %w", key, err)
			}
		}
		return nil
	}

	fieldsToPopulate := map[string]interface{}{
		"Heartbeat":           newState.R.Heartbeat,
		"ExtrapolatedClock":   newState.R.ExtrapolatedClock,
		"WeatherData":         newState.R.WeatherData,
		"TrackStatus":         newState.R.TrackStatus,
		"DriverList":          &newState.R.DriverList,
		"TopThree":            newState.R.TopThree,
		"TimingStats":         newState.R.TimingStats,
		"TimingAppData":       newState.R.TimingAppData,
		"RaceControlMessages": newState.R.RaceControlMessages,
		"SessionInfo":         newState.R.SessionInfo,
		"SessionData":         newState.R.SessionData,
		"TimingData":          newState.R.TimingData,
		"CarData.z":           &newState.R.CarDataZ,
		"Position.z":          &newState.R.PositionZ,
		"TyreStintSeries":     &newState.R.TyreStintSeries,
		"LapCount":            newState.R.LapCount,
	}

	for key, target := range fieldsToPopulate {
		if err := unmarshalField(key, target); err != nil {
			// Return the first error encountered during field population
			// The newState object might be partially populated.
			fmt.Printf("Error populating NewGlobalState field (%s): %v\n", key, err)
			return nil, err
		}
	}

	return newState, nil
}

// ApplyFeedUpdate takes the arguments ("A" field) from a SignalR feed message
// and updates the corresponding part of the global state.
// args[0] = field name (string)
// args[1] = update payload (interface{})
func (gs *GlobalState) ApplyFeedUpdate(args []interface{}) error {
	if len(args) < 2 {
		return fmt.Errorf("invalid feed arguments: expected at least 2, got %d", len(args))
	}

	fieldName, ok := args[0].(string)
	if !ok {
		return fmt.Errorf("invalid feed arguments: field name (arg 0) is not a string")
	}

	updatePayload := args[1]

	gs.mu.Lock()
	defer gs.mu.Unlock()

	payloadBytes, err := json.Marshal(updatePayload)
	if err != nil {
		return fmt.Errorf("failed to marshal update payload for field '%s': %w", fieldName, err)
	}

	// fmt.Printf("Applying update for field: %s, Payload: %s\n", fieldName, string(payloadBytes))

	// TODO: Clean this up man jesus
	switch fieldName {
	case "CarData.z":
		var data string
		if err := json.Unmarshal(payloadBytes, &data); err != nil {
			return fmt.Errorf("failed to unmarshal CarData.z payload: %w", err)
		}
		gs.R.CarDataZ = data
		// fmt.Printf("Updated CarData.z\n")

	case "Position.z":
		var data string
		if err := json.Unmarshal(payloadBytes, &data); err != nil {
			return fmt.Errorf("failed to unmarshal Position.z payload: %w", err)
		}
		gs.R.PositionZ = data
		// fmt.Printf("Updated Position.z\n")

	case "Heartbeat":
		if gs.R.Heartbeat == nil {
			gs.R.Heartbeat = &Heartbeat{}
		}
		if err := json.Unmarshal(payloadBytes, gs.R.Heartbeat); err != nil {
			return fmt.Errorf("failed to unmarshal Heartbeat payload: %w", err)
		}

	case "ExtrapolatedClock":
		if gs.R.ExtrapolatedClock == nil {
			gs.R.ExtrapolatedClock = &ExtrapolatedClock{}
		}
		if err := json.Unmarshal(payloadBytes, gs.R.ExtrapolatedClock); err != nil {
			return fmt.Errorf("failed to unmarshal ExtrapolatedClock payload: %w", err)
		}

	case "WeatherData":
		if gs.R.WeatherData == nil {
			gs.R.WeatherData = &WeatherData{}
		}
		if err := json.Unmarshal(payloadBytes, gs.R.WeatherData); err != nil {
			return fmt.Errorf("failed to unmarshal WeatherData payload: %w", err)
		}

	case "TrackStatus":
		if gs.R.TrackStatus == nil {
			gs.R.TrackStatus = &TrackStatus{}
		}
		if err := json.Unmarshal(payloadBytes, gs.R.TrackStatus); err != nil {
			return fmt.Errorf("failed to unmarshal TrackStatus payload: %w", err)
		}

	case "LapCount":
		if gs.R.LapCount == nil {
			gs.R.LapCount = &LapCount{}
		}
		if err := json.Unmarshal(payloadBytes, gs.R.LapCount); err != nil {
			return fmt.Errorf("failed to unmarshal TrackStatus payload: %w", err)
		}

	case "DriverList":
		// Unmarshal the update payload into a map where keys are driver numbers
		// and values are the raw JSON containing the fields to update (e.g., {"Line": 2}).
		var driverUpdates map[string]json.RawMessage
		if err := json.Unmarshal(payloadBytes, &driverUpdates); err != nil {
			return fmt.Errorf("failed to unmarshal DriverList update payload into map: %w", err)
		}

		// Annoying, so annoying
		delete(driverUpdates, "_kf")

		if gs.R.DriverList == nil {
			gs.R.DriverList = make(map[string]DriverInfo)
			fmt.Println("Warning: DriverList was nil during update. Initialized empty map.")
		}

		for driverNumber, rawUpdateData := range driverUpdates {
			existingInfo, found := gs.R.DriverList[driverNumber]
			if found {
				if err := json.Unmarshal(rawUpdateData, &existingInfo); err != nil {
					// Log error for this specific driver update but continue with others
					fmt.Printf("Warning: Failed to apply partial DriverList update for driver %s: %v. Update data: %s\n", driverNumber, err, string(rawUpdateData))
					continue
				}
				// We have to restore this in the map, we edited a copy technically
				gs.R.DriverList[driverNumber] = existingInfo
				// fmt.Printf("Updated DriverList for driver %s: Line=%d\n", driverNumber, existingInfo.Line)
			} else {
				fmt.Printf("Warning: Received DriverList update for driver %s who is not in the current state. Update data: %s\n", driverNumber, string(rawUpdateData))
			}
		}

	case "TopThree":
		type TopThreeUpdatePayload struct {
			Lines       map[string]json.RawMessage `json:"Lines"`
			SessionPart *int                       `json:"SessionPart,omitempty"`
			Withheld    *bool                      `json:"Withheld,omitempty"`
		}

		var updatePayload TopThreeUpdatePayload
		if err := json.Unmarshal(payloadBytes, &updatePayload); err != nil {
			fmt.Printf("Warning: Failed to unmarshal TopThree payload into expected structure: %v. Payload: %s\n", err, string(payloadBytes))
			return nil
		}

		if gs.R.TopThree == nil {
			gs.R.TopThree = &TopThreeData{Lines: make([]TopThreeLine, 0)}
		}

		if updatePayload.SessionPart != nil {
			gs.R.TopThree.SessionPart = *updatePayload.SessionPart
		}
		if updatePayload.Withheld != nil {
			gs.R.TopThree.Withheld = *updatePayload.Withheld
		}

		maxIndex := -1
		keysToProcess := make([]string, 0, len(updatePayload.Lines))
		for k := range updatePayload.Lines {
			keysToProcess = append(keysToProcess, k)
			idx, err := strconv.Atoi(k)
			if err == nil && idx > maxIndex {
				maxIndex = idx
			}
		}
		requiredSize := maxIndex + 1
		if requiredSize > 0 && len(gs.R.TopThree.Lines) < requiredSize {
			// Append empty structs to reach the required size
			for i := len(gs.R.TopThree.Lines); i < requiredSize; i++ {
				gs.R.TopThree.Lines = append(gs.R.TopThree.Lines, TopThreeLine{})
			}
		}

		for key, rawLineData := range updatePayload.Lines {
			// Convert map key ("0", "1", "2") to integer index
			index, err := strconv.Atoi(key)
			if err != nil {
				fmt.Printf("Warning: Invalid non-integer key '%s' in TopThree Lines update map.\n", key)
				continue
			}

			// We shouldn't need this but man I dont trust this api
			if index < 0 || index >= len(gs.R.TopThree.Lines) {
				fmt.Printf("Warning: Index %d out of bounds for TopThree Lines slice (size %d).\n", index, len(gs.R.TopThree.Lines))
				continue
			}

			existingLine := &gs.R.TopThree.Lines[index]

			if err := json.Unmarshal(rawLineData, existingLine); err != nil {
				fmt.Printf("Warning: Failed to apply partial TopThree update for index %d: %v. Update data: %s\n", index, err, string(rawLineData))
				continue
			}
			// No need to put it back in the slice, as we modified it via pointer.
		}

	case "TimingStats":
		type TimingStatsUpdatePayload struct {
			Lines       map[string]json.RawMessage `json:"Lines"`
			Withheld    *bool                      `json:"Withheld,omitempty"`
			SessionType *string                    `json:"SessionType,omitempty"`
		}

		var updatePayload TimingStatsUpdatePayload
		if err := json.Unmarshal(payloadBytes, &updatePayload); err != nil {
			fmt.Printf("Warning: Failed to unmarshal TimingStats payload into expected structure: %v. Payload: %s\n", err, string(payloadBytes))
			return nil
		}

		if gs.R.TimingStats == nil {
			gs.R.TimingStats = &TimingStatsData{Lines: make(map[string]DriverTimingStats)}
		}
		if gs.R.TimingStats.Lines == nil {
			gs.R.TimingStats.Lines = make(map[string]DriverTimingStats)
		}

		if updatePayload.Withheld != nil {
			gs.R.TimingStats.Withheld = *updatePayload.Withheld
		}
		if updatePayload.SessionType != nil {
			gs.R.TimingStats.SessionType = *updatePayload.SessionType
		}

		for driverNumber, rawDriverUpdateData := range updatePayload.Lines {
			existingStats, found := gs.R.TimingStats.Lines[driverNumber]
			if !found {
				existingStats = DriverTimingStats{}
				fmt.Printf("Info: Creating new TimingStats entry for driver %s during update.\n", driverNumber)
			}

			// Check for "BestSectors" value, this value is sent not as an array but with the index as the key.
			// Since this breaks auto marshall we will process it separate and remove after.
			var dataMap map[string]interface{}
			if err = json.Unmarshal(rawDriverUpdateData, &dataMap); err != nil {
				err = fmt.Errorf("failed to unmarshal raw message: %w", err)
				continue
			}
			bestSectorsData, exists := dataMap["BestSectors"]
			if exists {
				bestSectorsBytes, err := json.Marshal(bestSectorsData)
				if err != nil {
					fmt.Printf("Warning: Failed to marshal BestSectors data for driver %s: %v. Update data: %s\n", driverNumber, err, string(rawDriverUpdateData))
					continue
				}

				var parsedBestSectors map[string]SectorOrSpeedInfo
				err = json.Unmarshal(bestSectorsBytes, &parsedBestSectors)
				if err != nil {
					fmt.Printf("Warning: Failed to unmarshal BestSectors map for driver %s: %v. Update data: %s\n", driverNumber, err, string(rawDriverUpdateData))
					continue
				}

				for sectorIndexStr, sectorData := range parsedBestSectors {
					index, err := strconv.Atoi(sectorIndexStr)
					if err != nil {
						fmt.Printf("Warning: Invalid sector index '%s' for driver %s: %v. Update data: %s\n", sectorIndexStr, driverNumber, err, string(rawDriverUpdateData))
						continue
					}
					target := &existingStats.BestSectors[index]
					if sectorData.Value != "" {
						target.Value = sectorData.Value
					}
					if sectorData.Position != 0 {
						target.Position = sectorData.Position
					}
				}

				delete(dataMap, "BestSectors")
				// Re marshal datamap to remove the best sector
				rawDriverUpdateData, err = json.Marshal(dataMap)
				if err != nil {
					fmt.Printf("ERROR: Failed to remarshall datamap post best sector process: %s\n", dataMap)
				}
			}

			if err := json.Unmarshal(rawDriverUpdateData, &existingStats); err != nil {
				fmt.Printf("Warning: Failed to apply partial TimingStats update for driver %s: %v. Update data: %s\n", driverNumber, err, string(rawDriverUpdateData))
				continue
			}

			gs.R.TimingStats.Lines[driverNumber] = existingStats
		}

	case "TimingAppData":
		type TimingAppDataUpdatePayload struct {
			Lines map[string]json.RawMessage `json:"Lines"`
		}

		var updatePayload TimingAppDataUpdatePayload
		if err := json.Unmarshal(payloadBytes, &updatePayload); err != nil {
			fmt.Printf("Warning: Failed to unmarshal TimingAppData payload into expected structure: %v. Payload: %s\n", err, string(payloadBytes))
			return nil
		}

		if gs.R.TimingAppData == nil {
			gs.R.TimingAppData = &TimingAppData{Lines: make(map[string]DriverTimingAppData)}
		}
		if gs.R.TimingAppData.Lines == nil {
			gs.R.TimingAppData.Lines = make(map[string]DriverTimingAppData)
		}

		for driverNumber, rawDriverUpdateData := range updatePayload.Lines {

			// intermediate struct to parse the driver's update data
			type DriverTimingAppDataUpdate struct {
				Line   *int                       `json:"Line,omitempty"`
				Stints map[string]json.RawMessage `json:"Stints,omitempty"`
			}

			var driverUpdate DriverTimingAppDataUpdate
			if err := json.Unmarshal(rawDriverUpdateData, &driverUpdate); err != nil {
				fmt.Printf("Warning: Failed to unmarshal partial TimingAppData update for driver %s: %v. Update data: %s\n", driverNumber, err, string(rawDriverUpdateData))
				continue
			}

			existingAppData, found := gs.R.TimingAppData.Lines[driverNumber]
			if !found {
				existingAppData = DriverTimingAppData{
					RacingNumber: driverNumber,
					Stints:       make([]StintInfo, 0),
				}
				fmt.Printf("Info: Creating new TimingAppData entry for driver %s during update.\n", driverNumber)
			}

			if driverUpdate.Line != nil {
				existingAppData.Line = *driverUpdate.Line
			}

			if driverUpdate.Stints != nil {
				if existingAppData.Stints == nil {
					existingAppData.Stints = make([]StintInfo, 0)
				}

				// key is string index "0", "1" :/
				for stintKey, rawStintData := range driverUpdate.Stints {
					stintIndex, err := strconv.Atoi(stintKey)
					if err != nil {
						fmt.Printf("Warning: Invalid non-integer key '%s' in TimingAppData Stints update for driver %s.\n", stintKey, driverNumber)
						continue
					}

					if stintIndex < 0 {
						fmt.Printf("Warning: Invalid negative index %d in TimingAppData Stints update for driver %s.\n", stintIndex, driverNumber)
						continue
					}

					// Update an existing stint
					if stintIndex < len(existingAppData.Stints) {
						existingStint := &existingAppData.Stints[stintIndex]
						// Unmarshal the partial update data into the existing stint struct
						if err := json.Unmarshal(rawStintData, existingStint); err != nil {
							fmt.Printf("Warning: Failed to merge TimingAppData stint update at index %d for driver %s: %v. Stint data: %s\n", stintIndex, driverNumber, err, string(rawStintData))
						}
						// Append a new stint
					} else if stintIndex == len(existingAppData.Stints) {
						var newStint StintInfo
						// Unmarshal the update data into a new stint struct
						if err := json.Unmarshal(rawStintData, &newStint); err != nil {
							fmt.Printf("Warning: Failed to unmarshal new TimingAppData stint at index %d for driver %s: %v. Stint data: %s\n", stintIndex, driverNumber, err, string(rawStintData))
							continue // Skip appending if unmarshal fails
						}
						existingAppData.Stints = append(existingAppData.Stints, newStint)
					} else {
						fmt.Printf("Warning: Out-of-order stint index %d (slice length %d) in TimingAppData update for driver %s. Appending anyway.\n", stintIndex, len(existingAppData.Stints), driverNumber)
						var newStint StintInfo
						if err := json.Unmarshal(rawStintData, &newStint); err == nil {
							// If we get a gap in stints, then just fill the gap with blanks
							for i := len(existingAppData.Stints); i < stintIndex; i++ {
								existingAppData.Stints = append(existingAppData.Stints, StintInfo{})
							}
							existingAppData.Stints = append(existingAppData.Stints, newStint)
						} else {
							fmt.Printf("Warning: Failed to unmarshal out-of-order TimingAppData stint at index %d for driver %s: %v. Stint data: %s\n", stintIndex, driverNumber, err, string(rawStintData))
						}
					}
				}
			}

			gs.R.TimingAppData.Lines[driverNumber] = existingAppData
		}
	case "RaceControlMessages":
		type RaceControlUpdatePayload struct {
			Messages map[string]json.RawMessage `json:"Messages"`
		}

		// TODO: First message is not parsed correctly, it doesnt have map just array
		var updatePayload RaceControlUpdatePayload
		if err := json.Unmarshal(payloadBytes, &updatePayload); err != nil {
			fmt.Printf("Warning: Failed to unmarshal RaceControlMessages payload into expected structure: %v. Payload: %s\n", err, string(payloadBytes))
			return nil
		}

		if gs.R.RaceControlMessages == nil {
			gs.R.RaceControlMessages = &RaceControlData{Messages: make([]RaceControlMessage, 0)}
		}
		if gs.R.RaceControlMessages.Messages == nil {
			gs.R.RaceControlMessages.Messages = make([]RaceControlMessage, 0)
		}

		// TODO: Enforce the order based on these keys (appears to be index), testing for now if its ok
		for _, rawMessageValue := range updatePayload.Messages {
			var msg RaceControlMessage
			if err := json.Unmarshal(rawMessageValue, &msg); err == nil {
				gs.R.RaceControlMessages.Messages = append(gs.R.RaceControlMessages.Messages, msg)
			} else {
				fmt.Printf("Warning: Failed to unmarshal individual RaceControlMessage object: %v. Raw object: %s\n", err, string(rawMessageValue))
			}
		}

	case "SessionInfo":
		if gs.R.SessionInfo == nil {
			gs.R.SessionInfo = &SessionInfoData{}
		}
		if err := json.Unmarshal(payloadBytes, gs.R.SessionInfo); err != nil {
			return fmt.Errorf("failed to unmarshal SessionInfo payload: %w", err)
		}

	case "SessionData":
		type SessionDataUpdatePayload struct {
			Series       map[string]json.RawMessage `json:"Series,omitempty"`
			StatusSeries map[string]json.RawMessage `json:"StatusSeries,omitempty"`
		}

		var updatePayload SessionDataUpdatePayload
		if err := json.Unmarshal(payloadBytes, &updatePayload); err != nil {
			fmt.Printf("Warning: Failed to unmarshal SessionData payload into expected structure: %v. Payload: %s\n", err, string(payloadBytes))
			return nil
		}

		// Ensure the main struct and slices exist in the global state
		if gs.R.SessionData == nil {
			gs.R.SessionData = &SessionData{
				Series:       make([]QualifyingPartInfo, 0),
				StatusSeries: make([]StatusChangeInfo, 0),
			}
		}
		if gs.R.SessionData.Series == nil {
			gs.R.SessionData.Series = make([]QualifyingPartInfo, 0)
		}
		if gs.R.SessionData.StatusSeries == nil {
			gs.R.SessionData.StatusSeries = make([]StatusChangeInfo, 0)
		}

		if updatePayload.Series != nil {
			// Find max index to determine required slice size
			maxIndex := -1
			keysToProcess := make([]string, 0, len(updatePayload.Series))
			for k := range updatePayload.Series {
				keysToProcess = append(keysToProcess, k)
				idx, err := strconv.Atoi(k)
				if err == nil && idx > maxIndex {
					maxIndex = idx
				}
			}
			// Ensure slice is large enough
			requiredSize := maxIndex + 1
			if requiredSize > 0 && len(gs.R.SessionData.Series) < requiredSize {
				for i := len(gs.R.SessionData.Series); i < requiredSize; i++ {
					gs.R.SessionData.Series = append(gs.R.SessionData.Series, QualifyingPartInfo{})
				}
			}

			// Apply updates
			for key, rawData := range updatePayload.Series {
				index, err := strconv.Atoi(key)
				if err != nil {
					fmt.Printf("Warning: Invalid non-integer key '%s' in SessionData.Series update map.\n", key)
					continue
				}
				if index < 0 || index >= len(gs.R.SessionData.Series) {
					fmt.Printf("Warning: Index %d out of bounds for SessionData.Series slice (size %d).\n", index, len(gs.R.SessionData.Series))
					continue
				}

				// Unmarshal update directly into the slice element at the index
				if err := json.Unmarshal(rawData, &gs.R.SessionData.Series[index]); err != nil {
					fmt.Printf("Warning: Failed to apply SessionData.Series update for index %d: %v. Update data: %s\n", index, err, string(rawData))
				}
			}
		}

		if updatePayload.StatusSeries != nil {
			// Find max index to determine required slice size
			maxIndex := -1
			keysToProcess := make([]string, 0, len(updatePayload.StatusSeries))
			for k := range updatePayload.StatusSeries {
				keysToProcess = append(keysToProcess, k)
				idx, err := strconv.Atoi(k)
				if err == nil && idx > maxIndex {
					maxIndex = idx
				}
			}
			// Ensure slice is large enough
			requiredSize := maxIndex + 1
			if requiredSize > 0 && len(gs.R.SessionData.StatusSeries) < requiredSize {
				for i := len(gs.R.SessionData.StatusSeries); i < requiredSize; i++ {
					gs.R.SessionData.StatusSeries = append(gs.R.SessionData.StatusSeries, StatusChangeInfo{})
				}
			}

			// Apply updates
			for key, rawData := range updatePayload.StatusSeries {
				index, err := strconv.Atoi(key)
				if err != nil {
					fmt.Printf("Warning: Invalid non-integer key '%s' in SessionData.StatusSeries update map.\n", key)
					continue
				}
				if index < 0 || index >= len(gs.R.SessionData.StatusSeries) {
					fmt.Printf("Warning: Index %d out of bounds for SessionData.StatusSeries slice (size %d).\n", index, len(gs.R.SessionData.StatusSeries))
					continue
				}

				// Unmarshal update directly into the slice element at the index
				if err := json.Unmarshal(rawData, &gs.R.SessionData.StatusSeries[index]); err != nil {
					fmt.Printf("Warning: Failed to apply SessionData.StatusSeries update for index %d: %v. Update data: %s\n", index, err, string(rawData))
				}
			}
		}

	case "TimingData":
		type TimingDataUpdatePayload struct {
			NoEntries   []int                      `json:"NoEntries,omitempty"` // Assumes replacement if present
			SessionPart *int                       `json:"SessionPart,omitempty"`
			Lines       map[string]json.RawMessage `json:"Lines"` // Keep Lines as raw for later processing
		}

		var updatePayload TimingDataUpdatePayload
		if err := json.Unmarshal(payloadBytes, &updatePayload); err != nil {
			fmt.Printf("Warning: Failed to unmarshal TimingData payload into expected structure: %v. Payload: %s\n", err, string(payloadBytes))
			return nil
		}

		if gs.R.TimingData == nil {
			gs.R.TimingData = &TimingData{
				Lines:     make(map[string]DriverTimingData),
				NoEntries: make([]int, 0),
			}
		}
		if gs.R.TimingData.Lines == nil {
			gs.R.TimingData.Lines = make(map[string]DriverTimingData)
		}

		if updatePayload.SessionPart != nil {
			gs.R.TimingData.SessionPart = *updatePayload.SessionPart
		}
		if updatePayload.NoEntries != nil {
			gs.R.TimingData.NoEntries = updatePayload.NoEntries
		}

		for driverNumber, rawDriverUpdateData := range updatePayload.Lines {
			existingDriverData, found := gs.R.TimingData.Lines[driverNumber]
			if !found {
				existingDriverData = DriverTimingData{
					RacingNumber: driverNumber,
				}
				fmt.Printf("Info: Creating new TimingData entry for driver %s during update.\n", driverNumber)
			}

			// TODO: Lets do this after removing the slice updates to lower log crap
			// Unmarshal directly into the existing struct pointer. This merges simple fields
			// and fields that are structs (like BestLapTime, LastLapTime, Speeds) automatically.
			// It will *not* correctly handle map-to-slice updates (Stats, Sectors).
			if err := json.Unmarshal(rawDriverUpdateData, &existingDriverData); err != nil {
				// Log error but continue to try and process slice updates if possible
				// fmt.Printf("Warning: Initial unmarshal/merge for TimingData driver %s failed: %v. Partial merge might occur. Data: %s\n", driverNumber, err, string(rawDriverUpdateData))
			}

			// Pass 2: Handle map-to-slice conversions explicitly ==
			// Define a temporary struct to extract only the fields that use map-based slice updates
			type SliceUpdateFields struct {
				BestLapTimes map[string]json.RawMessage `json:"BestLapTimes,omitempty"`
				Stats        map[string]json.RawMessage `json:"Stats,omitempty"`
				Sectors      map[string]json.RawMessage `json:"Sectors,omitempty"`
			}
			var sliceUpdates SliceUpdateFields
			_ = json.Unmarshal(rawDriverUpdateData, &sliceUpdates)

			// Apply updates for each map-to-slice field if updates exist
			if len(sliceUpdates.BestLapTimes) > 0 {
				if err := applyMapUpdatesToSlice(&existingDriverData.BestLapTimes, sliceUpdates.BestLapTimes); err != nil {
					fmt.Printf("Warning: Error applying BestLapTimes updates for driver %s: %v\n", driverNumber, err)
				}
			}
			if len(sliceUpdates.Stats) > 0 {
				if err := applyMapUpdatesToSlice(&existingDriverData.Stats, sliceUpdates.Stats); err != nil {
					fmt.Printf("Warning: Error applying Stats updates for driver %s: %v\n", driverNumber, err)
				}
			}
			if len(sliceUpdates.Sectors) > 0 {
				// Special handling for Sectors due to nested Segments
				if err := applyMapUpdatesToSlice(&existingDriverData.Sectors, sliceUpdates.Sectors); err != nil {
					fmt.Printf("Warning: Error applying Sectors/Segments updates for driver %s: %v\n", driverNumber, err)
				}
			}

			type DeletionHint struct {
				Deleted []string `json:"_deleted"`
			}
			var deletionHint DeletionHint
			// Unmarshal *again* just to check for _deleted hint
			_ = json.Unmarshal(rawDriverUpdateData, &deletionHint)

			if len(deletionHint.Deleted) > 0 {
				// Use reflection on existingDriverData to zero out fields listed in deletionHint.Deleted
				driverDataVal := reflect.ValueOf(&existingDriverData).Elem()
				for _, fieldToZero := range deletionHint.Deleted {
					fieldVal := driverDataVal.FieldByName(fieldToZero)
					if fieldVal.IsValid() && fieldVal.CanSet() {
						fieldVal.Set(reflect.Zero(fieldVal.Type())) // Set to zero value
						fmt.Printf("Info: Zeroed out field '%s' for driver %s based on _deleted hint.\n", fieldToZero, driverNumber)
					} else {
						fmt.Printf("Warning: Could not find or set field '%s' for deletion hint on driver %s.\n", fieldToZero, driverNumber)
					}
				}
			}

			// Put the fully merged struct back into the map
			gs.R.TimingData.Lines[driverNumber] = existingDriverData
		}
		// fmt.Printf("Processed TimingData update.\n")

	case "TyreStintSeries":
		// TODO
		return nil

	default:
		fmt.Printf("Warning: Unhandled feed update for field: %s\n", fieldName)
	}

	return nil
}

func (gs *GlobalState) GetStateAsJSON() ([]byte, error) {
	// Use a read lock as we are only reading the state
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	// Create the intermediate struct instance in a cleaner format
	outputR := raceDataForJSON{
		Heartbeat:           gs.R.Heartbeat,
		CarDataZ:            gs.R.CarDataZ,
		PositionZ:           gs.R.PositionZ,
		ExtrapolatedClock:   gs.R.ExtrapolatedClock,
		TopThree:            gs.R.TopThree,
		TimingStats:         gs.R.TimingStats,
		TimingAppData:       gs.R.TimingAppData,
		WeatherData:         gs.R.WeatherData,
		TrackStatus:         gs.R.TrackStatus,
		DriverList:          gs.R.DriverList,
		RaceControlMessages: gs.R.RaceControlMessages,
		SessionInfo:         gs.R.SessionInfo,
		SessionData:         gs.R.SessionData,
		TimingData:          gs.R.TimingData,
		TyreStintSeries:     gs.R.TyreStintSeries,
	}

	// Wrap the struct within the top-level "R" key map
	outputMap := map[string]interface{}{
		"R": outputR,
	}

	jsonData, err := json.MarshalIndent(outputMap, "", "  ")
	// jsonData, err := json.Marshal(outputMap)

	if err != nil {
		return nil, fmt.Errorf("failed to marshal state to JSON: %w", err)
	}

	return jsonData, nil
}

// applyMapUpdatesToSlice handles merging updates from a map[string]json.RawMessage
// into a target slice. It resizes the slice if needed and unmarshals partial JSON
// into the element at the specified index.
// targetSlicePtr: Pointer to the slice (e.g., *[]YourStructType)
// updateMap: The map containing updates keyed by string index.
func applyMapUpdatesToSlice(targetSlicePtr interface{}, updateMap map[string]json.RawMessage) error {
	sliceVal := reflect.ValueOf(targetSlicePtr).Elem() // Get the slice value itself (*[]T -> []T)
	if !sliceVal.IsValid() {
		return fmt.Errorf("targetSlicePtr is not a valid pointer to a slice")
	}
	itemType := sliceVal.Type().Elem() // Get the type of the slice elements (T)

	if sliceVal.IsNil() {
		sliceVal.Set(reflect.MakeSlice(sliceVal.Type(), 0, len(updateMap))) // Make slice of correct type
	}

	maxIndex := -1
	for k := range updateMap {
		idx, err := strconv.Atoi(k)
		if err == nil && idx > maxIndex {
			maxIndex = idx
		}
	}

	requiredSize := maxIndex + 1
	if requiredSize > 0 && sliceVal.Len() < requiredSize {
		// Append zero values of itemType to reach the required size
		needed := requiredSize - sliceVal.Len()
		sliceVal.Set(reflect.AppendSlice(sliceVal, reflect.MakeSlice(sliceVal.Type(), needed, needed)))
	}

	for key, rawData := range updateMap {
		index, err := strconv.Atoi(key)
		if err != nil || index < 0 {
			fmt.Printf("Warning: Invalid non-integer or negative index '%s' in map-to-slice update.\n", key)
			continue
		}
		if index >= sliceVal.Len() {
			fmt.Printf("Warning: Index %d out of bounds (%d) for map-to-slice update (key '%s'). Slice should have been resized.\n", index, sliceVal.Len(), key)
			continue
		}

		elementPtr := sliceVal.Index(index).Addr().Interface() // Get pointer to T (e.g., *TimeAndLapInfo)

		// Unmarshal the update data into the existing element (merges)
		// TODO: Better handle sector timing segments
		// This produces warnings on every sectorTiming segments.
		if err := json.Unmarshal(rawData, elementPtr); err != nil {
			//fmt.Printf("Warning: Failed to apply map update to slice element at index %d: %v. Data: %s\n", index, err, string(rawData))
		}

		// Special handling for nested Segments within Sectors
		// Check if the element type is SectorTiming
		if itemType == reflect.TypeOf(SectorTiming{}) {
			// Need to check if the rawData contained a "Segments" map update
			type SegmentUpdateWrapper struct {
				Segments map[string]json.RawMessage `json:"Segments"`
			}
			var segmentWrapper SegmentUpdateWrapper
			// Unmarshal rawData *again* just to extract the Segments map
			_ = json.Unmarshal(rawData, &segmentWrapper) // Ignore error

			if len(segmentWrapper.Segments) > 0 {
				// Get pointer to the actual Segments slice within the SectorTiming struct
				sectorElement := sliceVal.Index(index)                                       // Get the SectorTiming struct value
				segmentsSlicePtr := sectorElement.FieldByName("Segments").Addr().Interface() // Get *[]SegmentStatus

				// Recursively apply map updates to the segments slice
				err := applyMapUpdatesToSlice(segmentsSlicePtr, segmentWrapper.Segments)
				if err != nil {
					fmt.Printf("Warning: Error applying nested segment updates for sector index %d: %v\n", index, err)
				}
			}
		}
	}
	return nil
}
