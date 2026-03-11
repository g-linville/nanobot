package installzip

import (
	"archive/zip"
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nanobot-ai/nanobot/pkg/skillformat"
)

func createZIP(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("failed to create %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close ZIP: %v", err)
	}
	return buf.Bytes()
}

func TestNormalizeArchiveSlashes(t *testing.T) {
	if got := normalizeArchiveSlashes(`nested\dir/SKILL.md`); got != "nested/dir/SKILL.md" {
		t.Fatalf("normalizeArchiveSlashes() = %q", got)
	}
}

func TestSanitizeArchivePathRejectsAbsoluteAndVolumePaths(t *testing.T) {
	tests := []string{
		"/tmp/SKILL.md",
		`C:\tmp\SKILL.md`,
		`C:/tmp/SKILL.md`,
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			_, err := sanitizeArchivePath(input)
			if err == nil {
				t.Fatal("expected sanitizeArchivePath() to fail")
			}
			if !strings.Contains(err.Error(), "absolute paths") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestExtractRejectsWindowsStyleTraversal(t *testing.T) {
	zipData := createZIP(t, map[string]string{
		`..\evil.sh`: "echo bad\n",
	})

	_, err := Extract(zipData, t.TempDir())
	if err == nil {
		t.Fatal("expected Extract() to fail")
	}
	if !strings.Contains(err.Error(), "path traversal") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureWithinBaseRejectsNonAbsoluteTargets(t *testing.T) {
	baseDir := t.TempDir()
	err := ensureWithinBase(baseDir, filepath.Join("relative", skillformat.SkillMainFile))
	if err == nil {
		t.Fatal("expected ensureWithinBase() to fail")
	}
	if !strings.Contains(err.Error(), "must be absolute") {
		t.Fatalf("unexpected error: %v", err)
	}
}
