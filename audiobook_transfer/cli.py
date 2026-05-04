"""Command-line interface for audioTransfer tool."""
import argparse
import sys
from pathlib import Path
from .utils import setup_logging, must_expand
from .organizer import run_transfer
from .transfer import TRANSFER_METHODS


def main():
    parser = argparse.ArgumentParser(
        description="Organize and transfer audiobooks to Audiobookshelf",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  audiotransfer                            # Use default source (~/qbit)
  audiotransfer --source /path/to/books    # Specific source
  audiotransfer --dry-run                  # Preview only
  audiotransfer --local                    # Organize locally, no SSH
  audiotransfer --force                    # Skip confirmations
  audiotransfer --methods native-ssh,local # Try specific methods
"""
    )

    parser.add_argument('--source', '-s', default=must_expand('~/qbit'),
                       help='Source directory (default: ~/qbit)')
    parser.add_argument('--host', '-H', default='audiobookshelf',
                       help='Remote hostname (default: audiobookshelf)')
    parser.add_argument('--target', '-t', default='/audiobooks',
                       help='Target path (default: /audiobooks)')
    parser.add_argument('--ssh-key', '-k', default=None,
                       help='SSH private key path (auto-detected if not set)')
    parser.add_argument('--dry-run', '-n', action='store_true',
                       help='Preview plan without transferring')
    parser.add_argument('--force', '-f', action='store_true',
                       help='Skip confirmation prompts')
    parser.add_argument('--verify', '-V', action='store_true',
                       help='Verify transfers after completion')
    parser.add_argument('--interactive', '-i', action='store_true',
                       help='Confirm each book match individually')
    parser.add_argument('--verbose', '-v', action='store_true',
                       help='Verbose debug output')
    parser.add_argument('--quiet', '-q', action='store_true',
                       help='Minimal output')
    parser.add_argument('--log-file', '-l', default=None,
                       help='Write log to file')
    parser.add_argument('--local', '-L', action='store_true',
                       help='Local copy only, no SSH')
    parser.add_argument('--methods', '-m', default=None,
                       help=f'Transfer methods (comma-separated): {", ".join(TRANSFER_METHODS)}')

    args = parser.parse_args()

    if not args.quiet:
        setup_logging(verbose=args.verbose, log_file=args.log_file)

    source_dir = Path(args.source)
    if not source_dir.exists() or not source_dir.is_dir():
        print(f"Error: Source directory not found: {source_dir}")
        sys.exit(1)

    methods = None
    if args.methods:
        methods = [m.strip() for m in args.methods.split(',')]
        invalid = [m for m in methods if m not in TRANSFER_METHODS]
        if invalid:
            print(f"Error: Unknown method(s): {', '.join(invalid)}")
            print(f"Available: {', '.join(TRANSFER_METHODS)}")
            sys.exit(1)

    interactive = args.interactive or (not args.force and not args.dry_run)

    report = run_transfer(
        source_dir=source_dir,
        dry_run=args.dry_run,
        interactive=interactive,
        host=args.host,
        target_base=args.target,
        force=args.force,
        verify=args.verify,
        ssh_key_path=args.ssh_key,
        local_only=args.local,
        methods=methods,
    )

    if report.failed > 0:
        sys.exit(1)
    sys.exit(0)


if __name__ == '__main__':
    main()
