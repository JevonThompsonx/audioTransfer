// Package virusscan provides virus scanning for audiobook files using ClamAV.
package virusscan

import (
	"fmt"
	"os/exec"
	"strings"
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
	clamdscanPath, _ := exec.LookPath("clamdscan")
	clamscanPath, _ := exec.LookPath("clamscan")

	// Prefer clamdscan (daemon mode — faster for bulk), but only when the
	// daemon is actually responsive. Selecting clamdscan while clamd is down
	// makes every batch fail with per-file errors ("0 clean, 0 infected,
	// N errors") while the caller proceeds as if scanning happened — silent
	// no-scan. Test the daemon before committing to it.
	daemonUp := false
	if clamdscanPath != "" {
		// clamdscan 1.4 requires attempts:interval for --ping; bare --ping is a parse error (exit 2)
		if probe := exec.Command(clamdscanPath, "-p", "1:1"); probe.Run() == nil {
			daemonUp = true
		} else {
			utils.Warn.Printf("clamd not responding — falling back to clamscan")
		}
	}

	s.BinPath, s.UseDaemon = selectBinary(clamdscanPath, clamscanPath, daemonUp)
}

// selectBinary picks between clamdscan (daemon, requires a live clamd) and
// clamscan (standalone) given which binaries exist and whether the daemon
// responded to a probe.
func selectBinary(clamdscanPath, clamscanPath string, daemonUp bool) (bin string, useDaemon bool) {
	if clamdscanPath != "" && daemonUp {
		return clamdscanPath, true
	}
	if clamscanPath != "" {
		return clamscanPath, false
	}
	return "clamscan", false
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
		// clamdscan 1.4 requires attempts:interval for --ping; bare --ping is a parse error (exit 2)
		cmd := exec.Command(s.BinPath, "-p", "1:1")
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
			}
			continue
		}
		report.Results = append(report.Results, results...)
	}

	// clamscan/clamdscan with --infected only prints non-clean files, so a
	// clean batch yields no per-file output. Treat files with no parsed result
	// and no scan error as clean (mirrors the remote scanner's behavior).
	fillCleanResults(paths, report)

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

// fillCleanResults marks scanned paths that produced no parsed output as clean.
// clamscan/clamdscan with --infected prints only non-clean files, so a fully
// clean batch yields no lines; without this, reports would show 0 clean files.
// Paths already represented in report.Results (infected or error) are left as-is.
func fillCleanResults(paths []string, report *ScanReport) {
	if len(report.Results) != 0 || report.Errors != 0 {
		return
	}
	for _, p := range paths {
		report.Results = append(report.Results, ScanResult{File: p})
	}
}

// scanBatch runs clamscan/clamdscan on a batch of files.
func (s *LocalScanner) scanBatch(files []string) ([]ScanResult, error) {
	args := s.batchArgs(files)

	cmd := exec.Command(s.BinPath, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()

	// Exit code 1 = infected files found (not an error)
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() != 1 {
			return nil, fmt.Errorf("clam exit code %d: %s", exitErr.ExitCode(), strings.TrimSpace(stderr.String()))
		}
	}

	return parseClamOutput(string(out)), nil
}

// batchArgs builds the argument list for a batch scan. clamdscan does NOT
// support the symlink-follow options (clamscan-only); it follows symlinks by
// default, which is acceptable — only pass the flags in clamscan mode.
//
// Note: ClamAV >= 1.4 removed the legacy --no-follow-symlinks flag; the
// supported spelling is --follow-{file,dir}-symlinks=0 ("never follow").
func (s *LocalScanner) batchArgs(files []string) []string {
	args := []string{"--infected", "--no-summary"}
	if !s.UseDaemon {
		args = append(args, "--follow-file-symlinks=0", "--follow-dir-symlinks=0")
	}
	return append(args, files...)
}
