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
	seen := false
	for _, a := range args {
		if a == "--no-follow-symlinks" {
			seen = true
		}
	}
	if !seen {
		t.Error("clamscan mode must include --no-follow-symlinks")
	}
}
