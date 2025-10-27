package state

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	pkgerrors "github.com/devhat/ipfailover/pkg/errors"
	"go.uber.org/zap"
)

// State represents the application state
type State struct {
	LastAppliedIP       string    `json:"last_applied_ip"`
	LastChangeTime      time.Time `json:"last_change_time"`
	LastCheckTime       time.Time `json:"last_check_time"`
	LastCheckIP         string    `json:"last_check_ip"`
	UpdateCount         int       `json:"update_count"`
	PrimaryFailureCount int       `json:"primary_failure_count"`
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
	if err := ctx.Err(); err != nil {
		return "", err
	}

	f.mutex.RLock()
	defer f.mutex.RUnlock()

	state, err := f.loadState(ctx)
	if err != nil {
		if pkgerrors.IsNotFoundError(err) {
			return "", err // Return the not found error directly
		}
		return "", pkgerrors.NewStateError("get_last_applied_ip", err)
	}

	return state.LastAppliedIP, nil
}

// SetLastAppliedIP stores the last applied IP
func (f *FileStateStore) SetLastAppliedIP(ctx context.Context, ip string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	f.mutex.Lock()
	defer f.mutex.Unlock()

	state, err := f.loadState(ctx)
	if err != nil {
		if pkgerrors.IsNotFoundError(err) {
			// If file doesn't exist, create new state
			state = &State{}
		} else {
			// For other errors (like corrupted files), also create new state
			state = &State{}
		}
	}

	state.LastAppliedIP = ip
	state.LastChangeTime = time.Now()
	state.UpdateCount++

	if err := f.saveState(ctx, state); err != nil {
		return pkgerrors.NewStateError("set_last_applied_ip", err)
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
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}

	f.mutex.RLock()
	defer f.mutex.RUnlock()

	state, err := f.loadState(ctx)
	if err != nil {
		if pkgerrors.IsNotFoundError(err) {
			return time.Time{}, err // Return the not found error directly
		}
		return time.Time{}, pkgerrors.NewStateError("get_last_change_time", err)
	}

	return state.LastChangeTime, nil
}

// SetLastChangeTime stores the timestamp of the last IP change
func (f *FileStateStore) SetLastChangeTime(ctx context.Context, t time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	f.mutex.Lock()
	defer f.mutex.Unlock()

	state, err := f.loadState(ctx)
	if err != nil {
		if pkgerrors.IsNotFoundError(err) {
			// If file doesn't exist, create new state
			state = &State{}
		} else {
			// For other errors (like corrupted files), also create new state
			state = &State{}
		}
	}

	state.LastChangeTime = t

	if err := f.saveState(ctx, state); err != nil {
		return pkgerrors.NewStateError("set_last_change_time", err)
	}

	return nil
}

// SetLastCheckInfo stores information about the last IP check
func (f *FileStateStore) SetLastCheckInfo(ctx context.Context, ip string, t time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	f.mutex.Lock()
	defer f.mutex.Unlock()

	state, err := f.loadState(ctx)
	if err != nil {
		if pkgerrors.IsNotFoundError(err) {
			// If file doesn't exist, create new state
			state = &State{}
		} else {
			// For other errors (like corrupted files), also create new state
			state = &State{}
		}
	}

	state.LastCheckTime = t
	state.LastCheckIP = ip

	if err := f.saveState(ctx, state); err != nil {
		return pkgerrors.NewStateError("set_last_check_info", err)
	}

	return nil
}

// GetLastCheckInfo returns information about the last IP check
func (f *FileStateStore) GetLastCheckInfo(ctx context.Context) (string, time.Time, error) {
	if err := ctx.Err(); err != nil {
		return "", time.Time{}, err
	}

	f.mutex.RLock()
	defer f.mutex.RUnlock()

	state, err := f.loadState(ctx)
	if err != nil {
		if pkgerrors.IsNotFoundError(err) {
			return "", time.Time{}, err // Return the not found error directly
		}
		return "", time.Time{}, pkgerrors.NewStateError("get_last_check_info", err)
	}

	return state.LastCheckIP, state.LastCheckTime, nil
}

// GetUpdateCount returns the number of updates performed
func (f *FileStateStore) GetUpdateCount(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	f.mutex.RLock()
	defer f.mutex.RUnlock()

	state, err := f.loadState(ctx)
	if err != nil {
		if pkgerrors.IsNotFoundError(err) {
			return 0, err // Return the not found error directly
		}
		return 0, pkgerrors.NewStateError("get_update_count", err)
	}

	return state.UpdateCount, nil
}

// loadState loads the state from the file
func (f *FileStateStore) loadState(ctx context.Context) (*State, error) {
	// Check if file exists
	if _, err := os.Stat(f.filePath); os.IsNotExist(err) {
		return nil, pkgerrors.NewNotFoundError("state file", err)
	}

	// Use a channel to receive the result from the goroutine
	type result struct {
		data []byte
		err  error
	}

	resultChan := make(chan result, 1)

	// Perform file read in a goroutine
	go func() {
		data, err := os.ReadFile(f.filePath)
		resultChan <- result{data: data, err: err}
	}()

	// Wait for either the file read to complete or context cancellation
	select {
	case res := <-resultChan:
		if res.err != nil {
			return nil, fmt.Errorf("failed to read state file: %w", res.err)
		}

		var state State
		if err := json.Unmarshal(res.data, &state); err != nil {
			return nil, fmt.Errorf("failed to unmarshal state: %w", err)
		}

		return &state, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// saveState saves the state to the file atomically
func (f *FileStateStore) saveState(ctx context.Context, state *State) error {
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

	// Use a channel to receive the result from the goroutine
	type result struct {
		err error
	}

	resultChan := make(chan result, 1)

	// Perform file write operations in a goroutine
	go func() {
		// Write to temporary file first
		tempFile := f.filePath + ".tmp"
		if err := os.WriteFile(tempFile, data, 0644); err != nil {
			resultChan <- result{err: fmt.Errorf("failed to write temporary state file: %w", err)}
			return
		}

		// Atomically rename to final file
		if err := os.Rename(tempFile, f.filePath); err != nil {
			// Attempt to clean up the temporary file on rename failure
			if removeErr := os.Remove(tempFile); removeErr != nil {
				f.logger.Warn("failed to remove temporary file after rename failure",
					zap.String("temp_file", tempFile),
					zap.Error(removeErr),
				)
			}
			resultChan <- result{err: fmt.Errorf("failed to rename temporary state file: %w", err)}
			return
		}

		resultChan <- result{err: nil}
	}()

	// Wait for either the file write to complete or context cancellation
	select {
	case res := <-resultChan:
		return res.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

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

// GetPrimaryFailureCount returns the current consecutive failure count for primary IP
func (f *FileStateStore) GetPrimaryFailureCount(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	f.mutex.RLock()
	defer f.mutex.RUnlock()

	state, err := f.loadState(ctx)
	if err != nil {
		if pkgerrors.IsNotFoundError(err) {
			return 0, nil // Return 0 for new state
		}
		return 0, err
	}

	return state.PrimaryFailureCount, nil
}

// SetPrimaryFailureCount sets the consecutive failure count for primary IP
func (f *FileStateStore) SetPrimaryFailureCount(ctx context.Context, count int) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	f.mutex.Lock()
	defer f.mutex.Unlock()

	state, err := f.loadState(ctx)
	if err != nil && !pkgerrors.IsNotFoundError(err) {
		return err
	}

	if state == nil {
		state = &State{}
	}

	state.PrimaryFailureCount = count

	return f.saveState(ctx, state)
}

// ResetPrimaryFailureCount resets the consecutive failure count for primary IP
func (f *FileStateStore) ResetPrimaryFailureCount(ctx context.Context) error {
	return f.SetPrimaryFailureCount(ctx, 0)
}
