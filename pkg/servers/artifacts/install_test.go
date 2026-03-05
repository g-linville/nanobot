package artifacts

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gopkg.in/yaml.v3"
)

func createTestZIP(t *testing.T, manifest artifactManifest, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	manifestData, err := yaml.Marshal(manifest)
	if err != nil {
		t.Fatalf("failed to marshal manifest: %v", err)
	}
	fw, err := w.Create("manifest.yaml")
	if err != nil {
		t.Fatalf("failed to create manifest entry: %v", err)
	}
	if _, err := fw.Write(manifestData); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	for name, content := range files {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatalf("failed to create entry %s: %v", name, err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatalf("failed to write entry %s: %v", name, err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("failed to close ZIP: %v", err)
	}
	return buf.Bytes()
}

func skipIfWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("unzip not available on Windows")
	}
}

func TestExtractZIP(t *testing.T) {
	skipIfWindows(t)

	manifest := artifactManifest{
		Name:         "test-wf",
		ArtifactType: "workflow",
		Files:        []manifestFile{{Path: "workflow.md", Size: 10}},
	}
	zipData := createTestZIP(t, manifest, map[string]string{
		"workflow.md": "# Test WF\n",
	})

	targetDir := filepath.Join(t.TempDir(), "extracted")
	installed, err := extractZIP(context.Background(), zipData, targetDir)
	if err != nil {
		t.Fatalf("extractZIP() error: %v", err)
	}

	if len(installed) != 2 {
		t.Errorf("installed = %v, want [manifest.yaml workflow.md]", installed)
	}

	content, err := os.ReadFile(filepath.Join(targetDir, "workflow.md"))
	if err != nil {
		t.Fatalf("failed to read extracted file: %v", err)
	}
	if string(content) != "# Test WF\n" {
		t.Errorf("content = %q, want %q", string(content), "# Test WF\n")
	}
}

func TestExtractZIP_NestedDirectories(t *testing.T) {
	skipIfWindows(t)

	manifest := artifactManifest{
		Name:         "nested-wf",
		ArtifactType: "workflow",
	}
	zipData := createTestZIP(t, manifest, map[string]string{
		"workflow.md":         "# Nested\n",
		"scripts/analyze.py":  "print('hi')\n",
		"scripts/lib/util.py": "pass\n",
	})

	targetDir := filepath.Join(t.TempDir(), "extracted")
	installed, err := extractZIP(context.Background(), zipData, targetDir)
	if err != nil {
		t.Fatalf("extractZIP() error: %v", err)
	}

	if len(installed) != 4 {
		t.Errorf("installed count = %d, want 4", len(installed))
	}

	content, err := os.ReadFile(filepath.Join(targetDir, "scripts", "lib", "util.py"))
	if err != nil {
		t.Fatalf("failed to read nested file: %v", err)
	}
	if string(content) != "pass\n" {
		t.Errorf("content = %q, want %q", string(content), "pass\n")
	}
}

func TestExtractZIP_IncludesManifest(t *testing.T) {
	skipIfWindows(t)

	manifest := artifactManifest{
		Name:         "with-manifest",
		ArtifactType: "workflow",
	}
	zipData := createTestZIP(t, manifest, map[string]string{
		"workflow.md": "# Test\n",
	})

	targetDir := filepath.Join(t.TempDir(), "extracted")
	installed, err := extractZIP(context.Background(), zipData, targetDir)
	if err != nil {
		t.Fatalf("extractZIP() error: %v", err)
	}

	var foundManifest bool
	for _, f := range installed {
		if f == "manifest.yaml" {
			foundManifest = true
		}
	}
	if !foundManifest {
		t.Error("manifest.yaml should be in installed files")
	}

	if _, err := os.Stat(filepath.Join(targetDir, "manifest.yaml")); err != nil {
		t.Errorf("manifest.yaml should exist on disk: %v", err)
	}
}

func TestReadManifestFromZIP(t *testing.T) {
	manifest := artifactManifest{
		Name:         "read-test",
		Description:  "A test",
		ArtifactType: "workflow",
	}
	zipData := createTestZIP(t, manifest, map[string]string{
		"workflow.md": "# Test\n",
	})

	got, err := readManifestFromZIP(zipData)
	if err != nil {
		t.Fatalf("readManifestFromZIP() error: %v", err)
	}
	if got.Name != "read-test" {
		t.Errorf("name = %q, want %q", got.Name, "read-test")
	}
	if got.ArtifactType != "workflow" {
		t.Errorf("artifactType = %q, want %q", got.ArtifactType, "workflow")
	}
}
