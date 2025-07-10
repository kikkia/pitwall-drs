package api

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"context"
	"f1sockets/model"
	"f1sockets/valkeyclient"

	ics "github.com/arran4/golang-ical"
)

const seasonCalendarURL = "https://ics.ecal.com/ecal-sub/660897ca63f9ca0008bcbea6/Formula%201.ics"

type SeasonLoader struct {
	schedule     model.SeasonSchedule
	loadInterval time.Duration
	stopChan     chan struct{}
	wg           sync.WaitGroup
	initialLoad  sync.Once
	readyChan    chan struct{}
	valkey       *valkeyclient.ValkeyClient
}

func NewSeasonLoader(interval time.Duration, valkey *valkeyclient.ValkeyClient) *SeasonLoader {
	return &SeasonLoader{
		loadInterval: interval,
		stopChan:     make(chan struct{}),
		readyChan:    make(chan struct{}),
		valkey:       valkey,
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
	defer s.initialLoad.Do(func() { close(s.readyChan) })

	if s.valkey != nil {
		cachedSchedule, err := s.valkey.LoadSeasonSchedule(context.Background())
		if err != nil {
			fmt.Printf("Error loading season schedule from Valkey: %v\n", err)
		} else if cachedSchedule != nil {
			s.schedule = *cachedSchedule
			fmt.Printf("Successfully loaded %d F1 events from cache.\n", len(s.schedule.Events))
			return
		}
	}

	fmt.Println("Fetching F1 season data from source...")
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
		title = strings.ReplaceAll(title, " FORMULA 1", "") // Replace some of the cruft in these names
		title = strings.ReplaceAll(title, " 2025", "")      // Replace the 2025 in the name
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

	if s.valkey != nil {
		err := s.valkey.SaveSeasonSchedule(context.Background(), &s.schedule)
		if err != nil {
			fmt.Printf("Error saving season schedule to Valkey: %v\n", err)
		} else {
			fmt.Println("Season schedule saved to Valkey.")
		}
	}
}

func (s *SeasonLoader) GetSeasonSchedule() model.SeasonSchedule {
	return s.schedule
}

func (s *SeasonLoader) WaitUntilReady() {
	<-s.readyChan
}
