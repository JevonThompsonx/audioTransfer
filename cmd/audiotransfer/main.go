// Command audiotransfer organizes and transfers audiobooks to Audiobookshelf.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jevonx/audioTransfer/pkg/organizer"
	"github.com/jevonx/audioTransfer/pkg/transfer"
	"github.com/jevonx/audioTransfer/pkg/utils"
)

func main() {
	sourceDir := flag.String("source", mustExpand("~/qbit"), "Source directory with audiobooks")
	destDir := flag.String("dest", mustExpand("~/qbit/organized"), "Destination directory (for local copy)")
	host := flag.String("host", "audiobookshelf", "Remote hostname")
	targetBase := flag.String("target", "/audiobooks", "Remote target base path")
	sshKey := flag.String("ssh-key", "", "Path to SSH private key")
	dryRun := flag.Bool("dry-run", false, "Preview plan without transferring")
	dryRunShort := flag.Bool("n", false, "Preview plan (short)")
	force := flag.Bool("force", false, "Skip confirmation prompts")
	forceShort := flag.Bool("f", false, "Skip confirmations (short)")
	interactive := flag.Bool("interactive", false, "Confirm each book interactively")
	interactiveShort := flag.Bool("i", false, "Confirm each book (short)")
	verify := flag.Bool("verify", false, "Verify transfers after completion")
	verifyShort := flag.Bool("V", false, "Verify transfers (short)")
	localOnly := flag.Bool("local", false, "Local copy only, no SSH")
	localOnlyShort := flag.Bool("L", false, "Local only (short)")
	verbose := flag.Bool("verbose", false, "Verbose debug output")
	verboseShort := flag.Bool("v", false, "Verbose (short)")
	methods := flag.String("methods", "", "Transfer methods (comma-separated: native-ssh,local)")
	flag.Parse()

	// Handle short flags
	if *dryRunShort {
		*dryRun = true
	}
	if *forceShort {
		*force = true
	}
	if *interactiveShort {
		*interactive = true
	}
	if *verifyShort {
		*verify = true
	}
	if *localOnlyShort {
		*localOnly = true
	}
	if *verboseShort {
		*verbose = true
	}

	// Set log output
	if *verbose {
		utils.Debug.SetOutput(os.Stderr)
	}

	fmt.Println("audioTransfer — Audiobook organizer & transfer tool")
	fmt.Printf("Source: %s\n", *sourceDir)
	if *localOnly {
		fmt.Printf("Dest:   %s (local)\n", *destDir)
	} else {
		fmt.Printf("Target: %s@%s:%s\n", transfer.DefaultUser, *host, *targetBase)
		fmt.Printf("Local fallback: %s\n", *destDir)
	}

	// Validate source
	srcInfo, err := os.Stat(*sourceDir)
	if err != nil || !srcInfo.IsDir() {
		fmt.Fprintf(os.Stderr, "ERROR: Source directory not found: %s\n", *sourceDir)
		os.Exit(1)
	}

	// Parse methods
	var methodList []string
	if *methods != "" {
		for _, m := range strings.Split(*methods, ",") {
			m = strings.TrimSpace(m)
			if m == "native-ssh" || m == "local" {
				methodList = append(methodList, m)
			} else {
				fmt.Fprintf(os.Stderr, "Unknown transfer method: %s\n", m)
				os.Exit(1)
			}
		}
	}

	// Determine interactive mode
	isInteractive := *interactive || (!*dryRun && !*force)

	report := organizer.RunTransfer(organizer.Config{
		SourceDir:   *sourceDir,
		DestDir:     *destDir,
		Host:        *host,
		TargetBase:  *targetBase,
		SSHKeyPath:  *sshKey,
		DryRun:      *dryRun || *dryRunShort,
		Verbose:     *verbose || *verboseShort,
		Force:       *force || *forceShort,
		Interactive: isInteractive,
		Verify:      *verify || *verifyShort,
		LocalOnly:   *localOnly || *localOnlyShort,
		Methods:     methodList,
	})

	if report.Failed > 0 {
		os.Exit(1)
	}
}

func mustExpand(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}
