package state_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devhat/ipfailover/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestFileStateStore_GetLastAppliedIP(t *testing.T) {
	t.Run("file does not exist", func(t *testing.T) {
		tempDir := t.TempDir()
		stateFile := filepath.Join(tempDir, "state.json")

		logger := zap.NewNop()
		store := state.NewFileStateStore(stateFile, logger)

		ip, err := store.GetLastAppliedIP(context.Background())

		assert.NoError(t, err)
		assert.Empty(t, ip)
	})

	t.Run("file exists with data", func(t *testing.T) {
		tempDir := t.TempDir()
		stateFile := filepath.Join(tempDir, "state.json")

		// Create initial state file
		initialState := `{
			"last_applied_ip": "203.0.113.10",
			"last_change_time": "2023-01-01T00:00:00Z",
			"last_check_time": "2023-01-01T00:00:00Z",
			"last_check_ip": "203.0.113.10",
			"update_count": 1
		}`
		require.NoError(t, os.WriteFile(stateFile, []byte(initialState), 0644))

		logger := zap.NewNop()
		store := state.NewFileStateStore(stateFile, logger)

		ip, err := store.GetLastAppliedIP(context.Background())

		assert.NoError(t, err)
		assert.Equal(t, "203.0.113.10", ip)
	})
}

func TestFileStateStore_SetLastAppliedIP(t *testing.T) {
	tempDir := t.TempDir()
	stateFile := filepath.Join(tempDir, "state.json")

	logger := zap.NewNop()
	store := state.NewFileStateStore(stateFile, logger)

	err := store.SetLastAppliedIP(context.Background(), "203.0.113.10")

	assert.NoError(t, err)

	// Verify the IP was stored
	ip, err := store.GetLastAppliedIP(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "203.0.113.10", ip)
}

func TestFileStateStore_GetLastChangeTime(t *testing.T) {
	tempDir := t.TempDir()
	stateFile := filepath.Join(tempDir, "state.json")

	logger := zap.NewNop()
	store := state.NewFileStateStore(stateFile, logger)

	// Set a specific time
	expectedTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	err := store.SetLastChangeTime(context.Background(), expectedTime)
	require.NoError(t, err)

	// Get the time back
	actualTime, err := store.GetLastChangeTime(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, expectedTime, actualTime)
}

func TestFileStateStore_SetLastCheckInfo(t *testing.T) {
	tempDir := t.TempDir()
	stateFile := filepath.Join(tempDir, "state.json")

	logger := zap.NewNop()
	store := state.NewFileStateStore(stateFile, logger)

	err := store.SetLastCheckInfo(context.Background(), "198.51.100.77")
	assert.NoError(t, err)

	ip, checkTime, err := store.GetLastCheckInfo(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "198.51.100.77", ip)
	assert.True(t, time.Since(checkTime) < time.Second)
}

func TestFileStateStore_GetUpdateCount(t *testing.T) {
	tempDir := t.TempDir()
	stateFile := filepath.Join(tempDir, "state.json")

	logger := zap.NewNop()
	store := state.NewFileStateStore(stateFile, logger)

	// Set IP multiple times to increment counter
	err := store.SetLastAppliedIP(context.Background(), "203.0.113.10")
	require.NoError(t, err)

	err = store.SetLastAppliedIP(context.Background(), "198.51.100.77")
	require.NoError(t, err)

	count, err := store.GetUpdateCount(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestFileStateStore_AtomicWrite(t *testing.T) {
	tempDir := t.TempDir()
	stateFile := filepath.Join(tempDir, "state.json")

	logger := zap.NewNop()
	store := state.NewFileStateStore(stateFile, logger)

	// Write state
	err := store.SetLastAppliedIP(context.Background(), "203.0.113.10")
	assert.NoError(t, err)

	// Verify file exists and is valid JSON
	data, err := os.ReadFile(stateFile)
	assert.NoError(t, err)
	assert.NotEmpty(t, data)

	// Verify it's valid JSON by parsing it
	var stateData map[string]interface{}
	err = json.Unmarshal(data, &stateData)
	assert.NoError(t, err)
	assert.Equal(t, "203.0.113.10", stateData["last_applied_ip"])
}

func TestMockStateStore(t *testing.T) {
	t.Run("GetLastAppliedIP", func(t *testing.T) {
		store := state.NewMockStateStore()
		ip, err := store.GetLastAppliedIP(context.Background())
		assert.NoError(t, err)
		assert.Empty(t, ip)
	})

	t.Run("SetLastAppliedIP", func(t *testing.T) {
		store := state.NewMockStateStore()
		err := store.SetLastAppliedIP(context.Background(), "203.0.113.10")
		assert.NoError(t, err)

		ip, err := store.GetLastAppliedIP(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, "203.0.113.10", ip)
	})

	t.Run("GetLastChangeTime", func(t *testing.T) {
		store := state.NewMockStateStore()
		expectedTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
		err := store.SetLastChangeTime(context.Background(), expectedTime)
		assert.NoError(t, err)

		actualTime, err := store.GetLastChangeTime(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, expectedTime, actualTime)
	})

	t.Run("SetLastCheckInfo", func(t *testing.T) {
		store := state.NewMockStateStore()
		err := store.SetLastCheckInfo(context.Background(), "198.51.100.77")
		assert.NoError(t, err)

		ip, checkTime, err := store.GetLastCheckInfo(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, "198.51.100.77", ip)
		assert.True(t, time.Since(checkTime) < time.Second)
	})

	t.Run("GetUpdateCount", func(t *testing.T) {
		store := state.NewMockStateStore()
		err := store.SetLastAppliedIP(context.Background(), "203.0.113.10")
		assert.NoError(t, err)

		err = store.SetLastAppliedIP(context.Background(), "198.51.100.77")
		assert.NoError(t, err)

		count, err := store.GetUpdateCount(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, 2, count)
	})
}

func TestFileStateStore_CorruptedFile(t *testing.T) {
	tempDir := t.TempDir()
	stateFile := filepath.Join(tempDir, "state.json")

	// Create corrupted JSON file
	require.NoError(t, os.WriteFile(stateFile, []byte("invalid json"), 0644))

	logger := zap.NewNop()
	store := state.NewFileStateStore(stateFile, logger)

	// Should handle corrupted file gracefully
	err := store.SetLastAppliedIP(context.Background(), "203.0.113.10")
	assert.NoError(t, err)

	ip, err := store.GetLastAppliedIP(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "203.0.113.10", ip)
}
