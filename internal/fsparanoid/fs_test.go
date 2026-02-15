//go:build linux

/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package fsparanoid

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFile_RefusesSymlinkEscape(t *testing.T) {
	root := t.TempDir()

	outsideDir := t.TempDir()
	secretPath := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(secretPath, []byte("secret"), 0o644); err != nil {
		t.Fatalf("WriteFile(secret): %v", err)
	}

	linkPath := filepath.Join(root, "leak")
	if err := os.Symlink(outsideDir, linkPath); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	b, err := ReadFile(root, "leak/secret.txt")
	if err == nil {
		t.Fatalf("expected error, got nil (content=%q)", string(b))
	}
	if strings.Contains(string(b), "secret") {
		t.Fatalf("expected secret not to be read")
	}
}
