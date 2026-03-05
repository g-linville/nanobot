package artifacts

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func withWorkingDir(t *testing.T, dir string) func() {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current working directory: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change to directory %s: %v", dir, err)
	}
	return func() {
		if err := os.Chdir(original); err != nil {
			t.Fatalf("failed to restore working directory: %v", err)
		}
	}
}

func createTestWorkflow(t *testing.T, baseDir, name string, files map[string]string) {
	t.Helper()
	dir := filepath.Join(baseDir, workflowsDir, name)
	for relPath, content := range files {
		fullPath := filepath.Join(dir, relPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("failed to create directory: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write file %s: %v", relPath, err)
		}
	}
}

func TestParseFrontmatter(t *testing.T) {
	content := "---\nname: Test Workflow\ndescription: A test workflow.\n---\n\n# Steps\n"
	fm, err := parseFrontmatter(content)
	if err != nil {
		t.Fatalf("parseFrontmatter() error: %v", err)
	}
	if fm.Name != "Test Workflow" {
		t.Errorf("name = %q, want %q", fm.Name, "Test Workflow")
	}
	if fm.Description != "A test workflow." {
		t.Errorf("description = %q, want %q", fm.Description, "A test workflow.")
	}
}

func TestParseFrontmatter_NoFrontmatter(t *testing.T) {
	content := "# Just markdown\nNo frontmatter here."
	fm, err := parseFrontmatter(content)
	if err != nil {
		t.Fatalf("parseFrontmatter() error: %v", err)
	}
	if fm.Name != "" || fm.Description != "" {
		t.Errorf("expected empty frontmatter, got name=%q description=%q", fm.Name, fm.Description)
	}
}

func TestCreateZIP(t *testing.T) {
	tempDir := t.TempDir()
	restore := withWorkingDir(t, tempDir)
	defer restore()

	createTestWorkflow(t, tempDir, "my-wf", map[string]string{
		"workflow.md":        "---\nname: My WF\ndescription: desc\n---\n# Steps\n",
		"scripts/analyze.py": "print('hello')\n",
	})

	workflowDir := filepath.Join(tempDir, workflowsDir, "my-wf")
	manifest := artifactManifest{
		Name:         "My WF",
		Description:  "desc",
		ArtifactType: "workflow",
		Files: []manifestFile{
			{Path: "workflow.md", Size: 46},
			{Path: "scripts/analyze.py", Size: 16},
		},
	}

	zipData, err := createZIP(workflowDir, manifest)
	if err != nil {
		t.Fatalf("createZIP() error: %v", err)
	}

	r, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		t.Fatalf("failed to open ZIP: %v", err)
	}

	expectedFiles := map[string]bool{
		"manifest.yaml":      false,
		"workflow.md":        false,
		"scripts/analyze.py": false,
	}

	for _, f := range r.File {
		if _, ok := expectedFiles[f.Name]; !ok {
			t.Errorf("unexpected file in ZIP: %s", f.Name)
		}
		expectedFiles[f.Name] = true
	}

	for name, found := range expectedFiles {
		if !found {
			t.Errorf("expected file not found in ZIP: %s", name)
		}
	}

	// Verify manifest content.
	for _, f := range r.File {
		if f.Name == "manifest.yaml" {
			rc, _ := f.Open()
			data, _ := io.ReadAll(rc)
			rc.Close()

			var m artifactManifest
			if err := yaml.Unmarshal(data, &m); err != nil {
				t.Fatalf("failed to parse manifest from ZIP: %v", err)
			}
			if m.Name != "My WF" {
				t.Errorf("manifest name = %q, want %q", m.Name, "My WF")
			}
			if m.ArtifactType != "workflow" {
				t.Errorf("manifest artifactType = %q, want %q", m.ArtifactType, "workflow")
			}
			if len(m.Files) != 2 {
				t.Errorf("manifest files count = %d, want 2", len(m.Files))
			}
		}
	}
}

func TestPublishArtifact_MissingWorkflow(t *testing.T) {
	tempDir := t.TempDir()
	restore := withWorkingDir(t, tempDir)
	defer restore()

	s := NewServer()
	_, err := s.publishArtifact(nil, publishArtifactParams{WorkflowName: "nonexistent"})
	if err == nil {
		t.Fatal("expected error for missing workflow, got nil")
	}
}

func TestPublishArtifact_EmptyName(t *testing.T) {
	s := NewServer()
	_, err := s.publishArtifact(nil, publishArtifactParams{})
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
}
