package state

import (
	"context"
	"sync"
	"time"
)

// MockStateStore implements StateStore for testing
type MockStateStore struct {
	lastAppliedIP       string
	lastChangeTime      time.Time
	lastCheckIP         string
	lastCheckTime       time.Time
	updateCount         int
	primaryFailureCount int
	mutex               sync.RWMutex
}

// NewMockStateStore creates a new mock state store
func NewMockStateStore() *MockStateStore {
	return &MockStateStore{}
}

// GetLastAppliedIP returns the last applied IP
func (m *MockStateStore) GetLastAppliedIP(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.lastAppliedIP, nil
}

// SetLastAppliedIP sets the last applied IP
func (m *MockStateStore) SetLastAppliedIP(ctx context.Context, ip string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.lastAppliedIP = ip
	m.lastChangeTime = time.Now()
	m.updateCount++
	return nil
}

// GetLastChangeTime returns the last change time
func (m *MockStateStore) GetLastChangeTime(ctx context.Context) (time.Time, error) {
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}

	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.lastChangeTime, nil
}

// SetLastChangeTime sets the last change time
func (m *MockStateStore) SetLastChangeTime(ctx context.Context, t time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.lastChangeTime = t
	return nil
}

// SetLastCheckInfo sets the last check information
func (m *MockStateStore) SetLastCheckInfo(ctx context.Context, ip string, t time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.lastCheckIP = ip
	m.lastCheckTime = t
	return nil
}

// GetLastCheckInfo returns the last check information
func (m *MockStateStore) GetLastCheckInfo(ctx context.Context) (string, time.Time, error) {
	if err := ctx.Err(); err != nil {
		return "", time.Time{}, err
	}

	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.lastCheckIP, m.lastCheckTime, nil
}

// GetUpdateCount returns the update count
func (m *MockStateStore) GetUpdateCount(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.updateCount, nil
}

// GetPrimaryFailureCount returns the current consecutive failure count for primary IP
func (m *MockStateStore) GetPrimaryFailureCount(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.primaryFailureCount, nil
}

// SetPrimaryFailureCount sets the consecutive failure count for primary IP
func (m *MockStateStore) SetPrimaryFailureCount(ctx context.Context, count int) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.primaryFailureCount = count
	return nil
}

// ResetPrimaryFailureCount resets the consecutive failure count for primary IP
func (m *MockStateStore) ResetPrimaryFailureCount(ctx context.Context) error {
	return m.SetPrimaryFailureCount(ctx, 0)
}
