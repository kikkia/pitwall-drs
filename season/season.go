package season

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"f1sockets/model"

	ics "github.com/arran4/golang-ical"
)

const seasonCalendarURL = "https://ics.ecal.com/ecal-sub/660897ca63f9ca0008bcbea6/Formula%201.ics"

type SeasonLoader struct {
	schedule     model.SeasonSchedule
	loadInterval time.Duration
	stopChan     chan struct{}
	wg           sync.WaitGroup
}

func NewSeasonLoader(interval time.Duration) *SeasonLoader {
	return &SeasonLoader{
		loadInterval: interval,
		stopChan:     make(chan struct{}),
	}
}

func (s *SeasonLoader) Start() {
	s.wg.Add(1)
	go s.run()
}

func (s *SeasonLoader) Stop() {
	close(s.stopChan)
	s.wg.Wait()
	fmt.Println("Season Loader stopped.")
}

func (s *SeasonLoader) run() {
	defer s.wg.Done()

	s.loadData()

	ticker := time.NewTicker(s.loadInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.loadData()
		case <-s.stopChan:
			return
		}
	}
}

func (s *SeasonLoader) loadData() {
	fmt.Println("Fetching F1 season data...")
	resp, err := http.Get(seasonCalendarURL)
	if err != nil {
		fmt.Printf("Error fetching season data: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Error fetching season data: status code %d\n", resp.StatusCode)
		return
	}

	cal, err := ics.ParseCalendar(resp.Body)
	if err != nil {
		fmt.Printf("Error parsing ICS data: %v\n", err)
		return
	}

	newSchedule := model.SeasonSchedule{Events: []model.Event{}}
	for _, component := range cal.Events() {
		title := component.GetProperty(ics.ComponentPropertySummary).Value
		titleParts := strings.Split(title, " - ")
		eventType := titleParts[len(titleParts)-1]
		event := model.Event{
			UID:         component.GetProperty(ics.ComponentPropertyUniqueId).Value,
			Location:    component.GetProperty(ics.ComponentPropertyLocation).Value,
			Summary:     title,
			Description: eventType,
		}

		dtStartProp := component.GetProperty(ics.ComponentPropertyDtStart)
		if dtStartProp != nil {
			startTime, err := component.GetStartAt()
			if err == nil {
				event.StartTime = startTime
			} else {
				fmt.Printf("Error parsing start time for event %s: %v\n", event.UID, err)
			}
		}

		dtEndProp := component.GetProperty(ics.ComponentPropertyDtEnd)
		if dtEndProp != nil {
			endTime, err := component.GetEndAt()
			if err == nil {
				event.EndTime = endTime
			} else {
				fmt.Printf("Error parsing end time for event %s: %v\n", event.UID, err)
			}
		}
		newSchedule.Events = append(newSchedule.Events, event)
	}

	s.schedule = newSchedule
	fmt.Printf("Successfully loaded %d F1 events.\n", len(s.schedule.Events))
}

func (s *SeasonLoader) GetSeasonSchedule() model.SeasonSchedule {
	return s.schedule
}
