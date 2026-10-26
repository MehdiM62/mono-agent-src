//go:build windows

package workflow

import (
	"fmt"
	"os"
)

const maxWorkflowFileBytes = 16 << 20

// readRegularFile reads a bounded regular file.
func readRegularFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	if info.Size() > maxWorkflowFileBytes {
		return nil, fmt.Errorf("%s is larger than %d bytes", path, maxWorkflowFileBytes)
	}
	return os.ReadFile(path)
}
