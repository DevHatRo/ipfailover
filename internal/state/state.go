package state

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/devhat/ipfailover/pkg/errors"
	"go.uber.org/zap"
)

// State represents the application state
type State struct {
	LastAppliedIP  string    `json:"last_applied_ip"`
	LastChangeTime time.Time `json:"last_change_time"`
	LastCheckTime  time.Time `json:"last_check_time"`
	LastCheckIP    string    `json:"last_check_ip"`
	UpdateCount    int       `json:"update_count"`
}

// FileStateStore implements StateStore using a JSON file
type FileStateStore struct {
	filePath string
	logger   *zap.Logger
	mutex    sync.RWMutex
}

// NewFileStateStore creates a new file-based state store
func NewFileStateStore(filePath string, logger *zap.Logger) *FileStateStore {
	return &FileStateStore{
		filePath: filePath,
		logger:   logger,
	}
}

// GetLastAppliedIP returns the last IP that was successfully applied
func (f *FileStateStore) GetLastAppliedIP(ctx context.Context) (string, error) {
	f.mutex.RLock()
	defer f.mutex.RUnlock()

	state, err := f.loadState()
	if err != nil {
		return "", errors.NewStateError("get_last_applied_ip", err)
	}

	return state.LastAppliedIP, nil
}

// SetLastAppliedIP stores the last applied IP
func (f *FileStateStore) SetLastAppliedIP(ctx context.Context, ip string) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	state, err := f.loadState()
	if err != nil {
		// If file doesn't exist or is corrupted, create new state
		state = &State{}
	}

	state.LastAppliedIP = ip
	state.LastChangeTime = time.Now()
	state.UpdateCount++

	if err := f.saveState(state); err != nil {
		return errors.NewStateError("set_last_applied_ip", err)
	}

	f.logger.Info("state updated",
		zap.String("last_applied_ip", ip),
		zap.Time("last_change_time", state.LastChangeTime),
		zap.Int("update_count", state.UpdateCount),
	)

	return nil
}

// GetLastChangeTime returns the timestamp of the last IP change
func (f *FileStateStore) GetLastChangeTime(ctx context.Context) (time.Time, error) {
	f.mutex.RLock()
	defer f.mutex.RUnlock()

	state, err := f.loadState()
	if err != nil {
		return time.Time{}, errors.NewStateError("get_last_change_time", err)
	}

	return state.LastChangeTime, nil
}

// SetLastChangeTime stores the timestamp of the last IP change
func (f *FileStateStore) SetLastChangeTime(ctx context.Context, t time.Time) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	state, err := f.loadState()
	if err != nil {
		// If file doesn't exist or is corrupted, create new state
		state = &State{}
	}

	state.LastChangeTime = t

	if err := f.saveState(state); err != nil {
		return errors.NewStateError("set_last_change_time", err)
	}

	return nil
}

// SetLastCheckInfo stores information about the last IP check
func (f *FileStateStore) SetLastCheckInfo(ctx context.Context, ip string) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	state, err := f.loadState()
	if err != nil {
		// If file doesn't exist or is corrupted, create new state
		state = &State{}
	}

	state.LastCheckTime = time.Now()
	state.LastCheckIP = ip

	if err := f.saveState(state); err != nil {
		return errors.NewStateError("set_last_check_info", err)
	}

	return nil
}

// GetLastCheckInfo returns information about the last IP check
func (f *FileStateStore) GetLastCheckInfo(ctx context.Context) (string, time.Time, error) {
	f.mutex.RLock()
	defer f.mutex.RUnlock()

	state, err := f.loadState()
	if err != nil {
		return "", time.Time{}, errors.NewStateError("get_last_check_info", err)
	}

	return state.LastCheckIP, state.LastCheckTime, nil
}

// GetUpdateCount returns the number of updates performed
func (f *FileStateStore) GetUpdateCount(ctx context.Context) (int, error) {
	f.mutex.RLock()
	defer f.mutex.RUnlock()

	state, err := f.loadState()
	if err != nil {
		return 0, errors.NewStateError("get_update_count", err)
	}

	return state.UpdateCount, nil
}

// loadState loads the state from the file
func (f *FileStateStore) loadState() (*State, error) {
	// Check if file exists
	if _, err := os.Stat(f.filePath); os.IsNotExist(err) {
		return &State{}, nil
	}

	data, err := os.ReadFile(f.filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	return &state, nil
}

// saveState saves the state to the file atomically
func (f *FileStateStore) saveState(state *State) error {
	// Ensure directory exists
	dir := filepath.Dir(f.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	// Marshal state to JSON
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// Write to temporary file first
	tempFile := f.filePath + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temporary state file: %w", err)
	}

	// Atomically rename to final file
	if err := os.Rename(tempFile, f.filePath); err != nil {
		return fmt.Errorf("failed to rename temporary state file: %w", err)
	}

	return nil
}

// MockStateStore implements StateStore for testing
type MockStateStore struct {
	lastAppliedIP  string
	lastChangeTime time.Time
	lastCheckIP    string
	lastCheckTime  time.Time
	updateCount    int
	mutex          sync.RWMutex
}

// NewMockStateStore creates a new mock state store
func NewMockStateStore() *MockStateStore {
	return &MockStateStore{}
}

// GetLastAppliedIP returns the last applied IP
func (m *MockStateStore) GetLastAppliedIP(ctx context.Context) (string, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.lastAppliedIP, nil
}

// SetLastAppliedIP sets the last applied IP
func (m *MockStateStore) SetLastAppliedIP(ctx context.Context, ip string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.lastAppliedIP = ip
	m.lastChangeTime = time.Now()
	m.updateCount++
	return nil
}

// GetLastChangeTime returns the last change time
func (m *MockStateStore) GetLastChangeTime(ctx context.Context) (time.Time, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.lastChangeTime, nil
}

// SetLastChangeTime sets the last change time
func (m *MockStateStore) SetLastChangeTime(ctx context.Context, t time.Time) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.lastChangeTime = t
	return nil
}

// SetLastCheckInfo sets the last check information
func (m *MockStateStore) SetLastCheckInfo(ctx context.Context, ip string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.lastCheckIP = ip
	m.lastCheckTime = time.Now()
	return nil
}

// GetLastCheckInfo returns the last check information
func (m *MockStateStore) GetLastCheckInfo(ctx context.Context) (string, time.Time, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.lastCheckIP, m.lastCheckTime, nil
}

// GetUpdateCount returns the update count
func (m *MockStateStore) GetUpdateCount(ctx context.Context) (int, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.updateCount, nil
}
