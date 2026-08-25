package main

import "testing"

func TestResolveDeletion(t *testing.T) {
	tests := []struct {
		name            string
		deleteSource    bool
		keepSource      bool
		keepSourceShort bool
		verify          bool
		verifyShort     bool
		wantDelete      bool
		wantVerify      bool
		wantErr         bool
	}{
		// Default (no flags): deletion OFF, verify OFF — safe.
		{name: "default-no-flags", wantDelete: false, wantVerify: false},
		// Explicit --delete-source WITHOUT --verify: refused.
		{name: "delete-no-verify", deleteSource: true, wantErr: true},
		// --delete-source WITH --verify: allowed.
		{name: "delete-with-verify", deleteSource: true, verify: true, wantDelete: true, wantVerify: true},
		// --delete-source with -V short verify: allowed.
		{name: "delete-with-verify-short", deleteSource: true, verifyShort: true, wantDelete: true, wantVerify: true},
		// --keep-source overrides --delete-source (no verify needed).
		{name: "keep-source-overrides", deleteSource: true, keepSource: true, verify: false, wantDelete: false, wantVerify: false},
		{name: "keep-source-short-overrides", deleteSource: true, keepSourceShort: true, wantDelete: false},
		// --verify alone never triggers deletion.
		{name: "verify-alone", verify: true, wantDelete: false, wantVerify: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotDel, gotVer, err := resolveDeletion(tt.deleteSource, tt.keepSource, tt.keepSourceShort, tt.verify, tt.verifyShort)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (delete=%v verify=%v)", gotDel, gotVer)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotDel != tt.wantDelete {
				t.Errorf("deleteRequested = %v, want %v", gotDel, tt.wantDelete)
			}
			if gotVer != tt.wantVerify {
				t.Errorf("verifyRequested = %v, want %v", gotVer, tt.wantVerify)
			}
		})
	}
}
