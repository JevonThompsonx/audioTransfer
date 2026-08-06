// Package virusscan provides virus scanning for audiobook files using ClamAV.
package virusscan

import (
	"time"

	"github.com/jevonx/audioTransfer/pkg/utils"
)

// Scanner interface for pluggable virus scanning backends.
type Scanner interface {
	MethodName() string
	ScanFiles(paths []string) (*ScanReport, error)
	ScanDir(path string, recursive bool) (*ScanReport, error)
}

// ScanResult holds the result for a single scanned file.
type ScanResult struct {
	File      string
	Infected  bool
	VirusName string
	Error     string
	Duration  time.Duration
}

// ScanReport holds the aggregate result of a scan operation.
type ScanReport struct {
	Total    int
	Clean    int
	Infected int
	Errors   int
	Duration time.Duration
	Results  []ScanResult
}

// PrintSummary prints a summary of the scan report.
func (r *ScanReport) PrintSummary() {
	status := "CLEAN"
	if r.Infected > 0 {
		status = "INFECTED"
	}
	if r.Errors > 0 && r.Infected == 0 {
		status = "ERRORS"
	}
	utils.Info.Printf("Scan %s — %d files, %d clean, %d infected, %d errors, %v elapsed",
		status, r.Total, r.Clean, r.Infected, r.Errors, r.Duration)
}
