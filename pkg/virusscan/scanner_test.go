package virusscan

import (
	"testing"
)

func TestParseClamOutput_Clean(t *testing.T) {
	output := `/tmp/test1.mp3: OK
/tmp/test2.mp3: OK
----------- SCAN SUMMARY -----------
Infected files: 0`
	results := parseClamOutput(output)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Infected {
			t.Errorf("file %s should not be infected", r.File)
		}
	}
}

func TestParseClamOutput_Infected(t *testing.T) {
	output := `/tmp/bad.mp3: Win.Test.EICAR_HDB-1 FOUND
/tmp/good.mp3: OK
----------- SCAN SUMMARY -----------
Infected files: 1`
	results := parseClamOutput(output)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if !results[0].Infected {
		t.Error("first file should be infected")
	}
	if results[0].VirusName != "Win.Test.EICAR_HDB-1" {
		t.Errorf("wrong virus name: %s", results[0].VirusName)
	}
	if results[1].Infected {
		t.Error("second file should not be infected")
	}
}

func TestParseClamOutput_Error(t *testing.T) {
	output := `/tmp/bad.mp3: Permission denied ERROR
/tmp/good.mp3: OK`
	results := parseClamOutput(output)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Error != "Permission denied" {
		t.Errorf("wrong error: %s", results[0].Error)
	}
}

func TestParseClamOutput_Empty(t *testing.T) {
	output := `----------- SCAN SUMMARY -----------
Infected files: 0`
	results := parseClamOutput(output)
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestParseClamOutput_PathWithSpaces(t *testing.T) {
	output := `/path/with spaces/file.mp3: Virus.Name FOUND`
	results := parseClamOutput(output)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].File != "/path/with spaces/file.mp3" {
		t.Errorf("wrong file path: %s", results[0].File)
	}
	if results[0].VirusName != "Virus.Name" {
		t.Errorf("wrong virus name: %s", results[0].VirusName)
	}
}

func TestSelectBinary_DaemonUp(t *testing.T) {
	bin, daemon := selectBinary("/usr/bin/clamdscan", "/usr/bin/clamscan", true)
	if !daemon || bin != "/usr/bin/clamdscan" {
		t.Errorf("expected clamdscan when daemon up, got %s daemon=%v", bin, daemon)
	}
}

func TestSelectBinary_DaemonDown_FallsBackToClamscan(t *testing.T) {
	// Regression: clamd installed but not running must NOT select clamdscan —
	// that produced silent "0 clean, 0 infected, N errors" scans.
	bin, daemon := selectBinary("/usr/bin/clamdscan", "/usr/bin/clamscan", false)
	if daemon || bin != "/usr/bin/clamscan" {
		t.Errorf("expected clamscan fallback when daemon down, got %s daemon=%v", bin, daemon)
	}
}

func TestSelectBinary_OnlyClamscan(t *testing.T) {
	bin, daemon := selectBinary("", "/usr/bin/clamscan", false)
	if daemon || bin != "/usr/bin/clamscan" {
		t.Errorf("expected clamscan when it is the only option, got %s daemon=%v", bin, daemon)
	}
}

func TestSelectBinary_Neither(t *testing.T) {
	bin, daemon := selectBinary("", "", false)
	if daemon || bin != "" {
		t.Errorf("expected empty binary when nothing found, got %q daemon=%v", bin, daemon)
	}
}

func TestParseScanSummary(t *testing.T) {
	output := `----------- SCAN SUMMARY -----------
Known viruses: 3287027
Engine version: 1.4.2
Scanned directories: 3
Scanned files: 42
Infected files: 1
Data scanned: 100.0 MB
Time: 10.0 sec`
	if n := parseScanSummary(output); n != 42 {
		t.Errorf("expected 42, got %d", n)
	}
	if n := parseScanSummary("no summary here"); n != 0 {
		t.Errorf("expected 0 for absent summary, got %d", n)
	}
}

func TestFillCleanResults_EmptyResults(t *testing.T) {
	// Clean scan (--infected prints nothing) must still count files as clean.
	report := &ScanReport{Total: 2, Results: []ScanResult{}}
	fillCleanResults([]string{"/a.mp3", "/b.mp3"}, report)
	if len(report.Results) != 2 {
		t.Fatalf("expected 2 clean results, got %d", len(report.Results))
	}
	for _, r := range report.Results {
		if r.Infected || r.Error != "" {
			t.Errorf("expected clean result, got %+v", r)
		}
	}
}

func TestFillCleanResults_KeepsExistingResults(t *testing.T) {
	// An infected file found in one batch must be preserved, while clean files
	// from other batches (which --infected prints nothing for) are still added.
	report := &ScanReport{Total: 2, Results: []ScanResult{{File: "/a.mp3", Infected: true, VirusName: "EICAR"}}}
	fillCleanResults([]string{"/a.mp3", "/b.mp3"}, report)
	if len(report.Results) != 2 {
		t.Fatalf("expected infected + clean, got %d results: %+v", len(report.Results), report.Results)
	}
	if !report.Results[0].Infected {
		t.Error("existing infected result must be preserved")
	}
	clean := report.Results[1]
	if clean.File != "/b.mp3" || clean.Infected || clean.Error != "" {
		t.Errorf("expected /b.mp3 marked clean, got %+v", clean)
	}
}

func TestFillCleanResults_KeepsErrors(t *testing.T) {
	// Batch failures carry Error results; do not mask them with clean marks.
	report := &ScanReport{Total: 1, Errors: 1, Results: []ScanResult{{File: "/a.mp3", Error: "boom"}}}
	fillCleanResults([]string{"/a.mp3"}, report)
	if len(report.Results) != 1 || report.Results[0].Error != "boom" {
		t.Errorf("error results must be preserved, got %+v", report.Results)
	}
}

func TestNewScanner_Local(t *testing.T) {
	s := NewScanner("local", "", 22, "", "")
	if s.MethodName() != "local-clamscan" && s.MethodName() != "local-clamdscan" {
		t.Errorf("unexpected method: %s", s.MethodName())
	}
}

func TestNewScanner_Remote(t *testing.T) {
	s := NewScanner("remote", "100.116.138.103", 22, "root", "")
	if s.MethodName() != "remote-clamscan" {
		t.Errorf("unexpected method: %s", s.MethodName())
	}
}

func TestLocalScanner_Preflight(t *testing.T) {
	s := NewLocalScanner()
	ok, msg := s.Preflight()
	if !ok {
		t.Skipf("clamscan not available: %s", msg)
	}
}

func TestScanReport_PrintSummary(t *testing.T) {
	report := &ScanReport{
		Total:    10,
		Clean:    8,
		Infected: 1,
		Errors:   1,
	}
	// Should not panic
	report.PrintSummary()
}

func TestScanBatchArgs_DaemonMode(t *testing.T) {
	s := &LocalScanner{BinPath: "/usr/bin/clamdscan", UseDaemon: true}
	args := s.batchArgs([]string{"/tmp/a.mp3"})
	if args[0] != "--infected" || args[1] != "--no-summary" {
		t.Fatalf("unexpected leading args: %v", args)
	}
	for _, a := range args {
		if a == "--no-follow-symlinks" {
			t.Fatal("clamdscan mode must never receive --no-follow-symlinks")
		}
	}
	if len(args) < 3 || args[len(args)-1] != "/tmp/a.mp3" {
		t.Errorf("file args must be appended last: %v", args)
	}
}

func TestScanBatchArgs_ClamscanMode(t *testing.T) {
	s := &LocalScanner{BinPath: "/usr/bin/clamscan", UseDaemon: false}
	args := s.batchArgs([]string{"/tmp/a.mp3"})
	// ClamAV >= 1.4: legacy --no-follow-symlinks is gone; use the supported form.
	for _, a := range args {
		if a == "--no-follow-symlinks" {
			t.Error("legacy --no-follow-symlinks is unsupported in ClamAV 1.4+ and must not be passed")
		}
	}
	found := map[string]bool{}
	for _, a := range args {
		if a == "--follow-file-symlinks=no" || a == "--follow-dir-symlinks=no" {
			found[a] = true
		}
	}
	if !found["--follow-file-symlinks=no"] || !found["--follow-dir-symlinks=no"] {
		t.Errorf("clamscan mode must pass the supported no-follow flags: %v", args)
	}
}
