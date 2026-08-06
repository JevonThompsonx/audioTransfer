package virusscan

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jevonx/audioTransfer/pkg/utils"
)

// RemoteScanner runs clamscan on a remote host via SSH.
type RemoteScanner struct {
	Host          string
	Port          int
	User          string
	SSHKeyPath    string
	controlSocket string
	sshPath       string
}

// NewRemoteScanner creates a remote scanner for the given host.
func NewRemoteScanner(host string, port int, user, sshKeyPath string) *RemoteScanner {
	s := &RemoteScanner{
		Host:       host,
		Port:       port,
		User:       user,
		SSHKeyPath: sshKeyPath,
	}
	s.sshPath, _ = exec.LookPath("ssh")
	return s
}

func (s *RemoteScanner) MethodName() string { return "remote-clamscan" }

// Preflight tests SSH connectivity and clamscan availability on remote.
func (s *RemoteScanner) Preflight() (bool, string) {
	if s.sshPath == "" {
		return false, "ssh not found in PATH"
	}

	// Set up mux socket
	socketDir := filepath.Join(os.TempDir(), "audioTransfer-ssh")
	if err := os.MkdirAll(socketDir, 0700); err == nil {
		s.controlSocket = filepath.Join(socketDir, fmt.Sprintf("scan_%s_%s_%d", s.User, s.Host, s.Port))
	}

	// Test connection
	cmd := s.buildSSHCmd("which clamscan && echo ok")
	out, err := s.runSSH(cmd, 15*time.Second)
	if err != nil || !strings.Contains(out, "ok") {
		return false, "clamscan not available on remote host"
	}
	return true, fmt.Sprintf("remote clamscan on %s", s.Host)
}

// ScanFiles scans files on the remote host by running clamscan via SSH.
// Note: files must already exist on the remote host.
func (s *RemoteScanner) ScanFiles(paths []string) (*ScanReport, error) {
	start := time.Now()
	report := &ScanReport{Total: len(paths)}

	if len(paths) == 0 {
		return report, nil
	}

	// For remote files, scan their parent directories
	dirs := map[string]bool{}
	for _, p := range paths {
		dirs[filepath.Dir(p)] = true
	}

	// Run clamscan on each directory
	for dir := range dirs {
		cmd := s.buildSSHCmd(fmt.Sprintf("clamscan --infected --no-summary -r %s 2>/dev/null", escapePath(dir)))
		out, err := s.runSSH(cmd, 30*time.Minute)
		if err != nil {
			// Exit code 1 = infected, not an error
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 1 {
				utils.Warn.Printf("Remote scan error for %s: %v", dir, err)
				continue
			}
		}

		results := parseClamOutput(out)
		// Filter to only the requested files
		fileSet := map[string]bool{}
		for _, p := range paths {
			fileSet[p] = true
		}
		for _, r := range results {
			if fileSet[r.File] || len(paths) == 0 {
				report.Results = append(report.Results, r)
			}
		}
	}

	// If no results from directory scan, mark all as clean
	if len(report.Results) == 0 {
		for _, p := range paths {
			report.Results = append(report.Results, ScanResult{File: p})
		}
	}

	// Aggregate
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

// ScanDir scans a directory on the remote host.
func (s *RemoteScanner) ScanDir(path string, recursive bool) (*ScanReport, error) {
	start := time.Now()
	report := &ScanReport{}

	args := "clamscan --infected --no-summary"
	if recursive {
		args += " -r"
	}
	args += " " + escapePath(path)

	cmd := s.buildSSHCmd(args)
	out, err := s.runSSH(cmd, 6*time.Hour) // large library can take hours

	// Exit code 1 = infected found (not an error)
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 1 {
		return nil, fmt.Errorf("remote clamscan failed: %v", err)
	}

	results := parseClamOutput(out)
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

	report.Duration = time.Since(start)
	return report, nil
}

// Disconnect closes the SSH mux connection.
func (s *RemoteScanner) Disconnect() {
	if s.controlSocket == "" {
		return
	}
	cmd := []string{
		s.sshPath,
		"-o", fmt.Sprintf("ControlPath=%s", s.controlSocket),
		"-O", "exit",
		fmt.Sprintf("%s@%s", s.User, s.Host),
	}
	exec.Command(cmd[0], cmd[1:]...).Run()
	os.Remove(s.controlSocket)
	s.controlSocket = ""
}

func (s *RemoteScanner) buildSSHCmd(remoteCmd string) []string {
	cmd := []string{
		s.sshPath,
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "LogLevel=ERROR",
	}
	if s.controlSocket != "" {
		cmd = append(cmd,
			"-o", "ControlMaster=auto",
			"-o", fmt.Sprintf("ControlPath=%s", s.controlSocket),
			"-o", "ControlPersist=600",
		)
	}
	if s.SSHKeyPath != "" {
		cmd = append(cmd, "-i", s.SSHKeyPath)
	}
	if s.Port != 22 {
		cmd = append(cmd, "-p", fmt.Sprintf("%d", s.Port))
	}
	cmd = append(cmd, fmt.Sprintf("%s@%s", s.User, s.Host), remoteCmd)
	return cmd
}

func (s *RemoteScanner) runSSH(args []string, timeout time.Duration) (string, error) {
	cmd := exec.Command(args[0], args[1:]...)
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		output := strings.TrimSpace(outBuf.String())
		if err != nil {
			return output, fmt.Errorf("%s: %s", err.Error(), strings.TrimSpace(errBuf.String()))
		}
		return output, nil
	case <-time.After(timeout):
		cmd.Process.Kill()
		return "", fmt.Errorf("timeout after %v", timeout)
	}
}

func escapePath(path string) string {
	escaped := strings.ReplaceAll(path, "'", "'\\''")
	return "'" + escaped + "'"
}
