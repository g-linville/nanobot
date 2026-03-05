package artifacts

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const workflowsDir = "workflows"

type publishArtifactParams struct {
	WorkflowName string `json:"workflowName"`
}

type publishResult struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version int    `json:"version"`
	Message string `json:"message"`
}

type artifactManifest struct {
	Name         string         `yaml:"name" json:"name"`
	Description  string         `yaml:"description,omitempty" json:"description,omitempty"`
	ArtifactType string         `yaml:"artifactType" json:"artifactType"`
	CreatedAt    string         `yaml:"createdAt,omitempty" json:"createdAt,omitempty"`
	Files        []manifestFile `yaml:"files" json:"files"`
}

type manifestFile struct {
	Path string `yaml:"path" json:"path"`
	Size int64  `yaml:"size" json:"size"`
}

type workflowFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func (s *Server) publishArtifact(ctx context.Context, params publishArtifactParams) (*publishResult, error) {
	if params.WorkflowName == "" {
		return nil, fmt.Errorf("workflowName is required")
	}

	cfg, err := getObotConfig(ctx)
	if err != nil {
		return nil, err
	}

	workflowDir := filepath.Join(".", workflowsDir, params.WorkflowName)
	mainFile := filepath.Join(workflowDir, "workflow.md")

	content, err := os.ReadFile(mainFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read workflow file: %w", err)
	}

	fm, err := parseFrontmatter(string(content))
	if err != nil {
		return nil, fmt.Errorf("failed to parse frontmatter: %w", err)
	}

	name := fm.Name
	if name == "" {
		name = params.WorkflowName
	}

	// Collect all files in the workflow directory.
	var files []manifestFile
	if err := filepath.Walk(workflowDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(workflowDir, path)
		if err != nil {
			return err
		}
		files = append(files, manifestFile{
			Path: filepath.ToSlash(relPath),
			Size: info.Size(),
		})
		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to walk workflow directory: %w", err)
	}

	manifest := artifactManifest{
		Name:         name,
		Description:  fm.Description,
		ArtifactType: "workflow",
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
		Files:        files,
	}

	zipData, err := createZIP(workflowDir, manifest)
	if err != nil {
		return nil, fmt.Errorf("failed to create ZIP: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.baseURL+"/api/published-artifacts", bytes.NewReader(zipData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if cfg.authHeader != "" {
		req.Header.Set("Authorization", cfg.authHeader)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to publish artifact: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("publish failed (status %d): %s", resp.StatusCode, string(body))
	}

	var apiResp struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		LatestVersion int    `json:"latestVersion"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &publishResult{
		ID:      apiResp.ID,
		Name:    apiResp.Name,
		Version: apiResp.LatestVersion,
		Message: fmt.Sprintf("Published %s v%d", apiResp.Name, apiResp.LatestVersion),
	}, nil
}

func createZIP(workflowDir string, manifest artifactManifest) ([]byte, error) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	manifestData, err := yaml.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal manifest: %w", err)
	}

	fw, err := w.Create("manifest.yaml")
	if err != nil {
		return nil, fmt.Errorf("failed to create manifest entry: %w", err)
	}
	if _, err := fw.Write(manifestData); err != nil {
		return nil, fmt.Errorf("failed to write manifest: %w", err)
	}

	for _, f := range manifest.Files {
		filePath := filepath.Join(workflowDir, filepath.FromSlash(f.Path))
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", f.Path, err)
		}

		fw, err := w.Create(f.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to create ZIP entry %s: %w", f.Path, err)
		}
		if _, err := fw.Write(data); err != nil {
			return nil, fmt.Errorf("failed to write ZIP entry %s: %w", f.Path, err)
		}
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("failed to close ZIP: %w", err)
	}

	return buf.Bytes(), nil
}

func parseFrontmatter(content string) (workflowFrontmatter, error) {
	lines := strings.Split(content, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return workflowFrontmatter{}, nil
	}

	endIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			endIdx = i
			break
		}
	}

	if endIdx == -1 {
		return workflowFrontmatter{}, fmt.Errorf("frontmatter missing closing delimiter")
	}

	fmYAML := strings.Join(lines[1:endIdx], "\n")
	var fm workflowFrontmatter
	if err := yaml.Unmarshal([]byte(fmYAML), &fm); err != nil {
		return workflowFrontmatter{}, fmt.Errorf("failed to parse frontmatter: %w", err)
	}

	return fm, nil
}
