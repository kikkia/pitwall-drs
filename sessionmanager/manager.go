package sessionmanager

import (
	"context"
	"f1sockets/api"
	"f1sockets/f1tvclient"
	"f1sockets/model"
	"f1sockets/valkeyclient"
	"fmt"
	"time"
)

type Manager struct {
	f1tvClient             *f1tvclient.F1TVClient
	seasonLoader           *api.SeasonLoader
	valkey                 *valkeyclient.ValkeyClient
	globalState            **model.GlobalState
	customEventBroadcaster model.CustomEventBroadcaster
	sessionEndedCount      int
}

func NewManager(client *f1tvclient.F1TVClient, loader *api.SeasonLoader, valkey *valkeyclient.ValkeyClient, gs **model.GlobalState, ceb model.CustomEventBroadcaster) *Manager {
	return &Manager{
		f1tvClient:             client,
		seasonLoader:           loader,
		valkey:                 valkey,
		globalState:            gs,
		customEventBroadcaster: ceb,
	}
}

func (m *Manager) Start() {
	fmt.Println("Auto-connect mode enabled. F1TV client will connect/disconnect based on session times.")
	m.checkAndManageConnection()
	go m.manageF1TVConnection()
}

func (m *Manager) manageF1TVConnection() {
	const checkInterval = 2 * time.Minute
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for range ticker.C {
		m.checkAndManageConnection()
	}
}

func (m *Manager) checkAndManageConnection() {
	const bufferDuration = 14 * time.Minute
	now := time.Now()
	schedule := m.seasonLoader.GetSeasonSchedule()

	activeEvent := m.findActiveEvent(&schedule, now, bufferDuration)

	if activeEvent != nil {
		if m.valkey != nil {
			completed, err := m.valkey.IsSessionCompleted(context.Background(), activeEvent.UID)
			if err != nil {
				fmt.Printf("Error checking if session is completed: %v\n", err)
			} else if completed {
				fmt.Printf("Session '%s' is already completed. Skipping connection.\n", activeEvent.Summary)
				if m.f1tvClient.IsRunning() {
					fmt.Println("Client is running for a completed session. Stopping it.")
					m.f1tvClient.Stop()
				}
				return
			}
		}

		if !m.f1tvClient.IsRunning() {
			fmt.Printf("Session '%s' is active. Connecting to F1TV client...\n", activeEvent.Summary)
			m.resetGlobalState()
			m.f1tvClient.Start()
		}

		if m.f1tvClient.IsRunning() {
			if m.handleFinalisedSessionCheck(activeEvent) {
				return
			}
		}

	} else {
		if m.f1tvClient.IsRunning() {
			fmt.Println("No active session. Disconnecting F1TV client...")
			if m.valkey != nil && *m.globalState != nil {
				err := m.valkey.SaveState(context.Background(), *m.globalState)
				if err != nil {
					fmt.Printf("Error saving state to Valkey: %v\n", err)
				} else {
					fmt.Println("Global state saved to Valkey.")
				}
			}
			m.f1tvClient.Stop()
			m.sessionEndedCount = 0
		} else if m.valkey != nil {
			if *m.globalState == nil || (*m.globalState).R.SessionInfo == nil {
				fmt.Println("No active session, loading latest state from Valkey...")
				loadedState, err := m.valkey.LoadLatestState(context.Background())
				if err != nil {
					fmt.Printf("Error loading state from Valkey: %v\n", err)
				} else if loadedState != nil {
					*m.globalState = loadedState
					(*m.globalState).Broadcaster = m.customEventBroadcaster
					fmt.Println("Successfully loaded latest state from Valkey.")
				} else {
					fmt.Println("No previous state found in Valkey.")
				}
			}
		}
	}
}

func (m *Manager) findActiveEvent(schedule *model.SeasonSchedule, now time.Time, buffer time.Duration) *model.Event {
	for _, event := range schedule.Events {
		connectTime := event.StartTime.Add(-buffer)
		disconnectTime := event.EndTime.Add(buffer * 4)

		if now.After(connectTime) && now.Before(disconnectTime) {
			return &event
		}
	}
	return nil
}

func (m *Manager) handleFinalisedSessionCheck(activeEvent *model.Event) bool {
	const endedCountToFinish = 5

	if *m.globalState != nil && (*m.globalState).IsSessionFinished() {
		m.sessionEndedCount++
		fmt.Printf("Session is ended. Check %d of %d.\n", m.sessionEndedCount, endedCountToFinish)
	} else {
		m.sessionEndedCount = 0
	}

	if m.sessionEndedCount >= endedCountToFinish {
		fmt.Println("Session finalised. Disconnecting F1TV client...")

		if m.valkey != nil {
			err := m.valkey.AddCompletedSession(context.Background(), activeEvent.UID)
			if err != nil {
				fmt.Printf("Error adding completed session to Valkey: %v\n", err)
			}

			if *m.globalState != nil {
				err := m.valkey.SaveState(context.Background(), *m.globalState)
				if err != nil {
					fmt.Printf("Error saving state to Valkey: %v\n", err)
				} else {
					fmt.Println("Global state saved to Valkey.")
				}
			}
		}

		m.f1tvClient.Stop()
		m.sessionEndedCount = 0
		return true
	}
	return false
}

func (m *Manager) resetGlobalState() {
	if *m.globalState != nil {
		fmt.Println("Resetting global state for new connection.")
	}
	*m.globalState = model.NewEmptyGlobalState()
	(*m.globalState).Broadcaster = m.customEventBroadcaster
}
