package virusscan

import (
	"fmt"
	"strings"
	"time"
)

// parseClamOutput parses clamscan/clamdscan text output.
// Format: "filepath: VirusName FOUND" for infected files.
func parseClamOutput(output string) []ScanResult {
	var results []ScanResult

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "Scanned") {
			continue
		}

		// Infected line: "filepath: VirusName FOUND"
		if strings.HasSuffix(line, " FOUND") {
			// Split on last ": " to handle paths with colons
			idx := strings.LastIndex(line, ": ")
			if idx > 0 {
				file := line[:idx]
				virusPart := line[idx+2:]
				virusName := strings.TrimSuffix(virusPart, " FOUND")
				results = append(results, ScanResult{
					File:      file,
					Infected:  true,
					VirusName: virusName,
				})
			}
			continue
		}

		// Error line: "filepath: Error message ERROR"
		if strings.HasSuffix(line, " ERROR") {
			idx := strings.LastIndex(line, ": ")
			if idx > 0 {
				file := line[:idx]
				errMsg := strings.TrimSuffix(line[idx+2:], " ERROR")
				results = append(results, ScanResult{
					File:  file,
					Error: errMsg,
				})
			}
			continue
		}

		// OK line: "filepath: OK"
		if strings.HasSuffix(line, " OK") {
			idx := strings.LastIndex(line, ": ")
			if idx > 0 {
				file := line[:idx]
				results = append(results, ScanResult{
					File: file,
				})
			}
			continue
		}
	}

	return results
}

// ParseDuration parses a clamscan duration string like "0:03:42"
func ParseDuration(s string) time.Duration {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0
	}
	var h, m, sec int
	fmt.Sscanf(parts[0], "%d", &h)
	fmt.Sscanf(parts[1], "%d", &m)
	fmt.Sscanf(parts[2], "%d", &sec)
	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(sec)*time.Second
}
