package virusscan

// NewScanner creates a Scanner based on the mode.
// mode: "local" (auto-detect clamscan/clamdscan) or "remote" (SSH to host)
func NewScanner(mode, host string, port int, user, sshKeyPath string) Scanner {
	switch mode {
	case "remote":
		return NewRemoteScanner(host, port, user, sshKeyPath)
	default: // "local"
		return NewLocalScanner()
	}
}
