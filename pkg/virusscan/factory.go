package virusscan

// NewScanner creates a local Scanner, auto-detecting the best ClamAV binary
// (clamdscan when a responsive daemon is present, otherwise clamscan).
//
// Historically this factory also exposed a "remote" mode that ran clamscan on
// another host over SSH. That path was never exercised by the organizer (which
// always constructs a "local" scanner at runPreTransferScan) and has been
// removed — see git history for the deleted RemoteScanner.
func NewScanner() Scanner {
	return NewLocalScanner()
}
