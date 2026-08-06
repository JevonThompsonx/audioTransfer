// Package virusscan provides virus scanning for audiobook files using ClamAV.
package virusscan

import (
	"fmt"
	"os/exec"
	"time"

	"github.com/jevonx/audioTransfer/pkg/utils"
)

// LocalScanner runs clamscan/clamdscan on the local machine.
type LocalScanner struct {
	BinPath   string
	UseDaemon bool
	Workers   int
}

// NewLocalScanner creates a local scanner, auto-detecting the best binary.
func NewLocalScanner() *LocalScanner {
	s := &LocalScanner{Workers: 4}
	s.detectBinary()
	return s
}

func (s *LocalScanner) detectBinary() {
	// Prefer clamdscan (daemon mode — faster for bulk)
	if p, err := exec.LookPath("clamdscan"); err == nil {
		s.BinPath = p
		s.UseDaemon = true
		return
	}
	// Fall back to clamscan
	if p, err := exec.LookPath("clamscan"); err == nil {
		s.BinPath = p
		s.UseDaemon = false
		return
	}
	s.BinPath = "clamscan"
	s.UseDaemon = false
}

func (s *LocalScanner) MethodName() string {
	if s.UseDaemon {
		return "local-clamdscan"
	}
	return "local-clamscan"
}

// Preflight checks if the scanner binary exists and daemon is responsive.
func (s *LocalScanner) Preflight() (bool, string) {
	if _, err := exec.LookPath(s.BinPath); err != nil {
		return false, fmt.Sprintf("%s not found in PATH", s.BinPath)
	}
	if s.UseDaemon {
		cmd := exec.Command(s.BinPath, "--ping")
		if err := cmd.Run(); err != nil {
			return false, "clamd not responding, falling back to clamscan"
		}
	}
	return true, s.BinPath
}

// ScanFiles scans a list of local file paths.
func (s *LocalScanner) ScanFiles(paths []string) (*ScanReport, error) {
	start := time.Now()
	report := &ScanReport{Total: len(paths)}

	if len(paths) == 0 {
		return report, nil
	}

	// Batch files for clamscan (no daemon) — clamdscan can handle more
	batchSize := 200
	if s.UseDaemon {
		batchSize = 500
	}

	for i := 0; i < len(paths); i += batchSize {
		end := i + batchSize
		if end > len(paths) {
			end = len(paths)
		}
		batch := paths[i:end]

		results, err := s.scanBatch(batch)
		if err != nil {
			utils.Warn.Printf("Scan batch error: %v", err)
			for _, p := range batch {
				report.Results = append(report.Results, ScanResult{
					File:  p,
					Error: err.Error(),
				})
				report.Errors++
			}
			continue
		}
		report.Results = append(report.Results, results...)
	}

	// Aggregate counts
	for _, r := range report.Results {
		if r.Infected {
			report.Infected++
		} else if r.Error != "" {
			report.Errors++
		} else {
			report.Clean++
		}
	}

	report.Duration = time.Since(start)
	return report, nil
}

// ScanDir scans a directory recursively.
func (s *LocalScanner) ScanDir(path string, recursive bool) (*ScanReport, error) {
	start := time.Now()
	report := &ScanReport{}

	args := []string{"--infected", "--no-summary"}
	if recursive {
		args = append(args, "-r")
	}
	args = append(args, path)

	cmd := exec.Command(s.BinPath, args...)
	out, err := cmd.Output()

	// clamscan exit codes: 0=clean, 1=infected, 2=error
	exitCode := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	}

	results := parseClamOutput(string(out))
	report.Results = results
	report.Total = len(results)

	for _, r := range results {
		if r.Infected {
			report.Infected++
		} else if r.Error != "" {
			report.Errors++
		} else {
			report.Clean++
		}
	}

	if exitCode == 2 {
		report.Errors++
	}

	report.Duration = time.Since(start)
	return report, nil
}

// scanBatch runs clamscan on a batch of files.
func (s *LocalScanner) scanBatch(files []string) ([]ScanResult, error) {
	args := []string{"--infected", "--no-summary", "--no-follow-symlinks"}
	args = append(args, files...)

	cmd := exec.Command(s.BinPath, args...)
	out, err := cmd.Output()

	// Exit code 1 = infected files found (not an error)
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() != 1 {
			return nil, fmt.Errorf("clamscan exit code %d: %s", exitErr.ExitCode(), string(exitErr.Stderr))
		}
	}

	return parseClamOutput(string(out)), nil
}
