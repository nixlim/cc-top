//go:build darwin

package correlator

import (
	"github.com/nixlim/cc-top/internal/scanner"
)

// scannerPortMapper adapts scanner.ProcessAPI to the PortMapper interface.
type scannerPortMapper struct {
	api scanner.ProcessAPI
}

// NewScannerPortMapper creates a PortMapper that uses the scanner's ProcessAPI
// to query open sockets via proc_pidfdinfo on macOS.
func NewScannerPortMapper(api scanner.ProcessAPI) PortMapper {
	return &scannerPortMapper{api: api}
}

// GetOpenPorts delegates to the scanner ProcessAPI.
func (s *scannerPortMapper) GetOpenPorts(pid int) ([][2]int, error) {
	return s.api.GetOpenPorts(pid)
}
