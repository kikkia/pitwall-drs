package valkeyclient

import (
	"context"
	"encoding/json"
	"f1sockets/model"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type ValkeyClient struct {
	client *redis.Client
}

func NewValkeyClient(addr string) *ValkeyClient {
	rdb := redis.NewClient(&redis.Options{
		Addr: addr,
	})
	return &ValkeyClient{client: rdb}
}

func (vc *ValkeyClient) SaveState(ctx context.Context, state *model.GlobalState) error {
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	timestamp := time.Now().Unix()
	key := fmt.Sprintf("f1_global_state:%d", timestamp)

	err = vc.client.Set(ctx, key, stateJSON, 0*time.Hour).Err()
	if err != nil {
		return fmt.Errorf("failed to save state to Valkey: %w", err)
	}
	return nil
}

func (vc *ValkeyClient) LoadLatestState(ctx context.Context) (*model.GlobalState, error) {
	keys, err := vc.client.Keys(ctx, "f1_global_state:*").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get keys from Valkey: %w", err)
	}

	if len(keys) == 0 {
		return nil, nil // No state saved yet
	}

	latestKey := keys[0]
	for _, key := range keys {
		if key > latestKey {
			latestKey = key
		}
	}

	stateJSON, err := vc.client.Get(ctx, latestKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get latest state from Valkey: %w", err)
	}

	var state model.GlobalState
	if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	return &state, nil
}

func (vc *ValkeyClient) SaveSeasonSchedule(ctx context.Context, schedule *model.SeasonSchedule) error {
	scheduleJSON, err := json.Marshal(schedule)
	if err != nil {
		return fmt.Errorf("failed to marshal season schedule: %w", err)
	}

	err = vc.client.Set(ctx, "f1_season_schedule", scheduleJSON, 24*time.Hour).Err()
	if err != nil {
		return fmt.Errorf("failed to save season schedule to Valkey: %w", err)
	}
	return nil
}

func (vc *ValkeyClient) LoadSeasonSchedule(ctx context.Context) (*model.SeasonSchedule, error) {
	scheduleJSON, err := vc.client.Get(ctx, "f1_season_schedule").Result()
	if err == redis.Nil {
		return nil, nil // Cache miss
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get season schedule from Valkey: %w", err)
	}

	var schedule model.SeasonSchedule
	if err := json.Unmarshal([]byte(scheduleJSON), &schedule); err != nil {
		return nil, fmt.Errorf("failed to unmarshal season schedule: %w", err)
	}

	return &schedule, nil
}
