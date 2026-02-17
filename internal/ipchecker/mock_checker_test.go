package ipchecker

import "context"

// MockChecker implements IPChecker for testing
type MockChecker struct {
	ip  string
	err error
}

// NewMockChecker creates a new mock IP checker
func NewMockChecker(ip string, err error) *MockChecker {
	return &MockChecker{
		ip:  ip,
		err: err,
	}
}

// GetCurrentIP returns the mocked IP or error
func (m *MockChecker) GetCurrentIP(ctx context.Context) (string, error) {
	return m.ip, m.err
}

// Name returns the checker name
func (m *MockChecker) Name() string {
	return "mock"
}

// SetIP sets the IP to return (for testing)
func (m *MockChecker) SetIP(ip string) {
	m.ip = ip
}

// SetError sets the error to return (for testing)
func (m *MockChecker) SetError(err error) {
	m.err = err
}
