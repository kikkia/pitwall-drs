package model

import (
	"regexp"
	"strconv"
	"strings"
)

type IncidentParser struct{}

func NewIncidentParser() *IncidentParser {
	return &IncidentParser{}
}

// take a RaceControlMessage and attempt to convert it into a structured IncidentEvent.
// returns the parsed incident and a slice of driver numbers involved.
func (p *IncidentParser) Parse(msg RaceControlMessage) (*IncidentEvent, []string) {
	// Quickly discard messages that are not incidents we are interested in.
	if !strings.Contains(msg.Message, "INCIDENT") &&
		!strings.Contains(msg.Message, "PENALTY") &&
		!strings.Contains(msg.Message, "DELETED") &&
		!strings.Contains(msg.Message, "FLAG") {
		return nil, nil
	}

	incident := &IncidentEvent{
		UTC:      msg.Utc,
		Message:  msg.Message,
		Category: msg.Category,
	}

	// Find all driver numbers and TLAs, e.g., "1 (VER)"
	driverRegex := regexp.MustCompile(`(\d+)\s\(([A-Z]{3})\)`)
	driverMatches := driverRegex.FindAllStringSubmatch(msg.Message, -1)
	if len(driverMatches) == 0 {
		return nil, nil
	}

	var driverNumbers []string
	var driverTlas []string
	for _, match := range driverMatches {
		driverNumbers = append(driverNumbers, match[1])
		driverTlas = append(driverTlas, match[2])
	}
	incident.Drivers = driverTlas

	trackLimitsRegex := regexp.MustCompile(`DELETED - TRACK LIMITS AT (TURN \d+) LAP (\d+)`)
	trackLimitsMatches := trackLimitsRegex.FindStringSubmatch(msg.Message)
	if len(trackLimitsMatches) > 2 {
		incident.Infringement = "Track Limits"
		incident.Location = trackLimitsMatches[1]
		if lap, err := strconv.Atoi(trackLimitsMatches[2]); err == nil {
			incident.LapNumber = lap
		}
	}

	if regexp.MustCompile(`BLACK AND WHITE FLAG`).MatchString(msg.Message) {
		incident.Infringement = "Black and White Flag"
	}

	penaltyRegex := regexp.MustCompile(`(\d+ SECOND(?:S)? TIME|DRIVE THROUGH) PENALTY`)
	penaltyMatches := penaltyRegex.FindStringSubmatch(msg.Message)
	if len(penaltyMatches) > 1 {
		incident.PenaltyType = strings.TrimSpace(penaltyMatches[1])
	}

	if incident.Location == "" {
		locationRegex := regexp.MustCompile(`(?:LAP (\d+) )?TURN (\d+)`)
		locationMatches := locationRegex.FindStringSubmatch(msg.Message)
		if len(locationMatches) > 2 {
			incident.Location = "Turn " + locationMatches[2]
			if incident.LapNumber == 0 && locationMatches[1] != "" {
				if lap, err := strconv.Atoi(locationMatches[1]); err == nil {
					incident.LapNumber = lap
				}
			}
		}
	}

	if incident.Infringement == "" {
		infringementRegex := regexp.MustCompile(` - (.*)`)
		infringementMatches := infringementRegex.FindStringSubmatch(msg.Message)
		if len(infringementMatches) > 1 {
			incident.Infringement = strings.TrimSpace(infringementMatches[1])
		}
	}

	return incident, driverNumbers
}
