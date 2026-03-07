package artifacts

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"

	"gopkg.in/yaml.v3"
)

const maxDownloadBytes = 100 * 1024 * 1024 // 100 MB

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

type installArtifactParams struct {
	ID      string `json:"id"`
	Version *int   `json:"version,omitempty"`
}

type installResult struct {
	Name           string   `json:"name"`
	Path           string   `json:"path"`
	InstalledFiles []string `json:"installedFiles"`
	Message        string   `json:"message"`
}

func (s *Server) installArtifact(ctx context.Context, params installArtifactParams) (*installResult, error) {
	// We rely on the `unzip` command to extract the artifact onto the local filesystem.
	// People should never be using this part of Nanobot outside of the container anyway,
	// so just block Windows.
	if runtime.GOOS == "windows" {
		return nil, fmt.Errorf("artifact installation is not supported on Windows")
	}

	if params.ID == "" {
		return nil, fmt.Errorf("id is required")
	}

	cfg, err := getObotConfig(ctx)
	if err != nil {
		return nil, err
	}

	downloadURL := cfg.baseURL + "/api/published-artifacts/" + params.ID + "/download"
	if params.Version != nil {
		downloadURL += "?version=" + strconv.Itoa(*params.Version)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if cfg.authHeader != "" {
		req.Header.Set("Authorization", cfg.authHeader)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download artifact: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("download failed (status %d): %s", resp.StatusCode, string(body))
	}

	zipData, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read artifact data: %w", err)
	}
	if len(zipData) > maxDownloadBytes {
		return nil, fmt.Errorf("artifact exceeds maximum size of %d bytes", maxDownloadBytes)
	}

	manifest, err := readManifestFromZIP(zipData)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest from ZIP: %w", err)
	}

	if manifest.Name == "" {
		return nil, fmt.Errorf("manifest name is empty")
	}
	if filepath.Base(manifest.Name) != manifest.Name || manifest.Name == "." || manifest.Name == ".." {
		return nil, fmt.Errorf("invalid artifact name: %s", manifest.Name)
	}

	var targetDir string
	switch manifest.ArtifactType {
	case "workflow":
		targetDir = filepath.Join(".", workflowsDir, manifest.Name)
	default:
		return nil, fmt.Errorf("unsupported artifact type: %s", manifest.ArtifactType)
	}

	// Remove existing directory to allow overwrite.
	if err := os.RemoveAll(targetDir); err != nil {
		return nil, fmt.Errorf("failed to remove existing directory: %w", err)
	}

	installedFiles, err := extractZIP(ctx, zipData, targetDir)
	if err != nil {
		return nil, fmt.Errorf("failed to extract artifact: %w", err)
	}

	return &installResult{
		Name:           manifest.Name,
		Path:           targetDir,
		InstalledFiles: installedFiles,
		Message:        fmt.Sprintf("Installed %s into %s (%d files)", manifest.Name, targetDir, len(installedFiles)),
	}, nil
}

func readManifestFromZIP(data []byte) (artifactManifest, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return artifactManifest{}, fmt.Errorf("invalid ZIP archive: %w", err)
	}

	for _, f := range r.File {
		if f.Name == "manifest.yaml" {
			rc, err := f.Open()
			if err != nil {
				return artifactManifest{}, fmt.Errorf("failed to open manifest: %w", err)
			}
			defer rc.Close()

			manifestData, err := io.ReadAll(rc)
			if err != nil {
				return artifactManifest{}, fmt.Errorf("failed to read manifest: %w", err)
			}

			var manifest artifactManifest
			if err := yaml.Unmarshal(manifestData, &manifest); err != nil {
				return artifactManifest{}, fmt.Errorf("failed to parse manifest: %w", err)
			}
			return manifest, nil
		}
	}

	return artifactManifest{}, fmt.Errorf("manifest.yaml not found in ZIP")
}

// extractZIP writes the ZIP data to a temp file and uses the system `unzip`
// command to extract it into targetDir (excluding manifest.yaml).
func extractZIP(ctx context.Context, data []byte, targetDir string) ([]string, error) {
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create target directory: %w", err)
	}

	tmpFile, err := os.CreateTemp("", "artifact-*.zip")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("failed to write temp file: %w", err)
	}
	tmpFile.Close()

	cmd := exec.CommandContext(ctx, "unzip", "-o", tmpFile.Name(), "-d", targetDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("unzip failed: %w\n%s", err, string(output))
	}

	// Walk the extracted directory to build the installed files list.
	var installed []string
	if err := filepath.Walk(targetDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(targetDir, path)
		if err != nil {
			return err
		}
		installed = append(installed, filepath.ToSlash(relPath))
		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to list extracted files: %w", err)
	}

	return installed, nil
}
