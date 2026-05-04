// Package transfer provides multi-method file transfer capability.
// Supports: native SSH/SCP, local copy, with automatic fallback.
package transfer

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jevonx/audioTransfer/pkg/utils"
)

// Default connection settings.
const (
	DefaultHost       = "audiobookshelf"
	DefaultPort       = 22
	DefaultUser       = "root"
	DefaultTargetBase = "/audiobooks"
)

// Transfer methods in priority order.
var TransferMethods = []string{"native-ssh", "local"}

// TransferClient interface for pluggable transfer backends.
type TransferClient interface {
	MethodName() string
	Preflight() (bool, string)
	Connect() bool
	Disconnect()
	TransferBook(audioFiles, coverFiles []string, targetSubpath string) bool
	VerifyTransfer(remoteSubpath string) map[string]interface{}
}

// NativeSSHClient uses the system's ssh/scp commands.
type NativeSSHClient struct {
	Host       string
	Port       int
	User       string
	TargetBase string
	sshKeyPath string
	sshPath    string
	scpPath    string
}

// NewNativeSSHClient creates a new native SSH transfer client.
func NewNativeSSHClient(host string, port int, user, targetBase, sshKeyPath string) *NativeSSHClient {
	return &NativeSSHClient{
		Host:       host,
		Port:       port,
		User:       user,
		TargetBase: strings.TrimRight(targetBase, "/"),
		sshKeyPath: sshKeyPath,
	}
}

func (c *NativeSSHClient) MethodName() string { return "native-ssh" }

func (c *NativeSSHClient) findBinaries() {
	if c.sshPath == "" {
		p, _ := exec.LookPath("ssh")
		c.sshPath = p
	}
	if c.scpPath == "" {
		p, _ := exec.LookPath("scp")
		c.scpPath = p
	}
}

func (c *NativeSSHClient) Preflight() (bool, string) {
	c.findBinaries()
	if c.sshPath == "" {
		return false, "ssh command not found in PATH"
	}
	if c.scpPath == "" {
		return false, "scp command not found in PATH"
	}
	if ok, msg := checkHostname(c.Host, c.Port); !ok {
		return false, msg
	}
	return true, fmt.Sprintf("SSH ready: %s", c.sshPath)
}

func (c *NativeSSHClient) Connect() bool {
	ok, _ := c.Preflight()
	if !ok {
		return false
	}

	cmd := c.buildSSHCmd("echo ok")
	result, err := runCmd(cmd, 15*time.Second)
	if err != nil || !strings.Contains(result, "ok") {
		utils.Warn.Printf("SSH connection test failed: %v", err)
		return false
	}
	utils.Info.Printf("Connected to %s@%s via native SSH", c.User, c.Host)
	return true
}

func (c *NativeSSHClient) Disconnect() {}

func (c *NativeSSHClient) TransferBook(audioFiles, coverFiles []string, targetSubpath string) bool {
	targetSubpath, err := validateSubpath(targetSubpath)
	if err != nil {
		utils.Error.Printf("  Invalid path: %v", err)
		return false
	}
	remoteDir := c.TargetBase + "/" + targetSubpath
	utils.Info.Printf("  Target: %s", remoteDir)

	if !c.ensureRemoteDir(remoteDir) {
		return false
	}

	allFiles := append(audioFiles, coverFiles...)
	if len(allFiles) == 0 {
		utils.Warn.Printf("  No files to transfer")
		return false
	}

	transferred := 0
	for _, f := range allFiles {
		if _, err := os.Stat(f); err != nil {
			utils.Warn.Printf("  File not found: %s", f)
			continue
		}
		fi, _ := os.Stat(f)
		utils.Info.Printf("  Transferring: %s (%s)", filepath.Base(f), formatSize(fi.Size()))

		if c.transferFile(f, remoteDir) {
			transferred++
		}
	}

	success := transferred == len(allFiles)
	if success {
		utils.Info.Printf("  OK: %d/%d files transferred", transferred, len(allFiles))
	} else {
		utils.Warn.Printf("  Partial: %d/%d files transferred", transferred, len(allFiles))
	}
	return success
}

func (c *NativeSSHClient) VerifyTransfer(remoteSubpath string) map[string]interface{} {
	result := map[string]interface{}{
		"path":       c.TargetBase + "/" + remoteSubpath,
		"exists":     false,
		"files":      []map[string]interface{}{},
		"total_size": int64(0),
	}

	remotePath := c.TargetBase + "/" + remoteSubpath
	cmd := c.buildSSHCmd(fmt.Sprintf("ls -la %s 2>/dev/null || echo 'MISSING'", escapeSSH(remotePath)))
	output, err := runCmd(cmd, 15*time.Second)
	if err != nil || strings.Contains(output, "MISSING") {
		result["error"] = "Remote path not found"
		return result
	}

	result["exists"] = true
	var totalSize int64
	var files []map[string]interface{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "total") || strings.HasPrefix(line, "d") || strings.Contains(line, "MISSING") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 9 {
			var size int64
			fmt.Sscanf(parts[4], "%d", &size)
			files = append(files, map[string]interface{}{
				"name": parts[8],
				"size": size,
			})
			totalSize += size
		}
	}
	result["files"] = files
	result["total_size"] = totalSize
	return result
}

func (c *NativeSSHClient) buildSSHCmd(remoteCmd string) []string {
	cmd := []string{
		c.sshPath,
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "LogLevel=ERROR",
	}
	if c.sshKeyPath != "" {
		cmd = append(cmd, "-i", c.sshKeyPath)
	}
	if c.Port != 22 {
		cmd = append(cmd, "-p", fmt.Sprintf("%d", c.Port))
	}
	cmd = append(cmd, fmt.Sprintf("%s@%s", c.User, c.Host), remoteCmd)
	return cmd
}

func (c *NativeSSHClient) buildSCPCmd(localFile, remoteDir string) []string {
	cmd := []string{
		c.scpPath,
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "LogLevel=ERROR",
	}
	if c.sshKeyPath != "" {
		cmd = append(cmd, "-i", c.sshKeyPath)
	}
	if c.Port != 22 {
		cmd = append(cmd, "-P", fmt.Sprintf("%d", c.Port))
	}
	cmd = append(cmd, localFile, fmt.Sprintf("%s@%s:%s/", c.User, c.Host, escapeSSH(strings.TrimRight(remoteDir, "/"))))
	return cmd
}

func (c *NativeSSHClient) ensureRemoteDir(remotePath string) bool {
	cmd := c.buildSSHCmd(fmt.Sprintf("mkdir -p %s", escapeSSH(remotePath)))
	_, err := runCmd(cmd, 15*time.Second)
	if err != nil {
		utils.Error.Printf("Failed to create remote dir: %s", remotePath)
		return false
	}
	utils.Debug.Printf("  Created remote dir: %s", remotePath)
	return true
}

func (c *NativeSSHClient) transferFile(localFile, remoteDir string) bool {
	cmd := c.buildSCPCmd(localFile, remoteDir)
	_, err := runCmd(cmd, 10*time.Minute)
	if err != nil {
		utils.Error.Printf("  Failed to transfer %s: %v", filepath.Base(localFile), err)
		return false
	}
	return true
}

// LocalClient copies files to a local directory.
type LocalClient struct {
	TargetBase string
}

// NewLocalClient creates a new local transfer client.
func NewLocalClient(targetBase string) *LocalClient {
	return &LocalClient{TargetBase: targetBase}
}

func (c *LocalClient) MethodName() string { return "local" }

func (c *LocalClient) Preflight() (bool, string) {
	abs, _ := filepath.Abs(c.TargetBase)
	if err := os.MkdirAll(abs, 0755); err != nil {
		return false, fmt.Sprintf("Cannot create target dir: %v", err)
	}
	return true, fmt.Sprintf("Local dir ready: %s", abs)
}

func (c *LocalClient) Connect() bool {
	ok, _ := c.Preflight()
	if !ok {
		return false
	}
	return true
}

func (c *LocalClient) Disconnect() {}

func (c *LocalClient) TransferBook(audioFiles, coverFiles []string, targetSubpath string) bool {
	targetSubpath, err := validateSubpath(targetSubpath)
	if err != nil {
		utils.Error.Printf("  Invalid path: %v", err)
		return false
	}
	localDir := filepath.Join(c.TargetBase, targetSubpath)
	utils.Info.Printf("  Target: %s", localDir)

	if err := os.MkdirAll(localDir, 0755); err != nil {
		utils.Error.Printf("Failed to create local dir: %v", err)
		return false
	}

	allFiles := append(audioFiles, coverFiles...)
	transferred := 0
	for _, f := range allFiles {
		dest := filepath.Join(localDir, filepath.Base(f))
		if err := copyFile(f, dest); err != nil {
			utils.Error.Printf("  Failed to copy %s: %v", filepath.Base(f), err)
			continue
		}
		transferred++
	}

	success := transferred == len(allFiles)
	if success {
		utils.Info.Printf("  OK: %d/%d files copied", transferred, len(allFiles))
	}
	return success
}

func (c *LocalClient) VerifyTransfer(remoteSubpath string) map[string]interface{} {
	result := map[string]interface{}{
		"path":       filepath.Join(c.TargetBase, remoteSubpath),
		"exists":     false,
		"files":      []map[string]interface{}{},
		"total_size": int64(0),
	}

	localPath := filepath.Join(c.TargetBase, remoteSubpath)
	info, err := os.Stat(localPath)
	if err != nil {
		result["error"] = "Local path not found"
		return result
	}
	if !info.IsDir() {
		result["error"] = "Not a directory"
		return result
	}

	result["exists"] = true
	entries, _ := os.ReadDir(localPath)
	var totalSize int64
	var files []map[string]interface{}
	for _, e := range entries {
		fi, _ := e.Info()
		if fi != nil {
			sz := fi.Size()
			files = append(files, map[string]interface{}{
				"name": e.Name(),
				"size": sz,
			})
			totalSize += sz
		}
	}
	result["files"] = files
	result["total_size"] = totalSize
	return result
}

// Factory function.
func NewClient(method, host, targetBase, sshKeyPath string, port int) TransferClient {
	switch method {
	case "local":
		return NewLocalClient(targetBase)
	default:
		return NewNativeSSHClient(host, port, DefaultUser, targetBase, sshKeyPath)
	}
}

// --- Helpers ---

func checkHostname(host string, port int) (bool, string) {
	// Simple TCP dial check
	cmd := exec.Command("nc", "-z", "-w", "5", host, fmt.Sprintf("%d", port))
	err := cmd.Run()
	if err != nil {
		// Try ping as fallback
		pingCmd := exec.Command("ping", "-c", "1", "-W", "3", host)
		if pingErr := pingCmd.Run(); pingErr != nil {
			return false, fmt.Sprintf("Host %s not reachable", host)
		}
		return true, fmt.Sprintf("Host %s reachable via ping", host)
	}
	return true, fmt.Sprintf("Host %s:%d reachable", host, port)
}

func runCmd(args []string, timeout time.Duration) (string, error) {
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
		if err != nil {
			return "", fmt.Errorf("%s: %s", err.Error(), strings.TrimSpace(errBuf.String()))
		}
		return strings.TrimSpace(outBuf.String()), nil
	case <-time.After(timeout):
		cmd.Process.Kill()
		return "", fmt.Errorf("timeout after %v", timeout)
	}
}

func escapeSSH(path string) string {
	escaped := strings.ReplaceAll(path, "'", "'\\''")
	return "'" + escaped + "'"
}

func validateSubpath(subpath string) (string, error) {
	subpath = strings.TrimSpace(subpath)
	clean := filepath.Clean(subpath)
	if strings.Contains(clean, "..") {
		return "", fmt.Errorf("path traversal detected: %s", subpath)
	}
	if filepath.IsAbs(clean) {
		clean = clean[1:]
	}
	return clean, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
