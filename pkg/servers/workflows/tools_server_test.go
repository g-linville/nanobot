package workflows

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nanobot-ai/nanobot/pkg/mcp"
	"github.com/nanobot-ai/nanobot/pkg/types"
)

func TestRecordWorkflowRun_DeduplicatesURI(t *testing.T) {
	s := NewToolsServer()
	ctx := context.Background()
	session := mcp.NewEmptySession(ctx)
	ctx = mcp.WithSession(ctx, session)

	uri := "workflow:///test-workflow"
	if _, err := s.recordWorkflowRun(ctx, struct {
		URI string `json:"uri"`
	}{URI: uri}); err != nil {
		t.Fatalf("first recordWorkflowRun() failed: %v", err)
	}

	if _, err := s.recordWorkflowRun(ctx, struct {
		URI string `json:"uri"`
	}{URI: uri}); err != nil {
		t.Fatalf("second recordWorkflowRun() failed: %v", err)
	}

	var uris []string
	session.Root().Get(types.WorkflowURIsSessionKey, &uris)
	if len(uris) != 1 {
		t.Fatalf("expected one recorded URI, got %d: %v", len(uris), uris)
	}
	if uris[0] != uri {
		t.Errorf("recorded URI = %q, want %q", uris[0], uri)
	}
}

func TestDeleteWorkflow_RemovesDirectory(t *testing.T) {
	tempDir := t.TempDir()
	restore := withWorkingDir(t, tempDir)
	defer restore()

	workflowDir := filepath.Join(tempDir, workflowsDir, "to-delete")
	if err := os.MkdirAll(workflowDir, 0755); err != nil {
		t.Fatalf("failed to create workflow directory: %v", err)
	}

	workflowFile := filepath.Join(workflowDir, "workflow.md")
	if err := os.WriteFile(workflowFile, []byte("# test"), 0644); err != nil {
		t.Fatalf("failed to write workflow file: %v", err)
	}

	s := NewToolsServer()
	if _, err := s.deleteWorkflow(context.Background(), struct {
		URI string `json:"uri"`
	}{URI: "workflow:///to-delete"}); err != nil {
		t.Fatalf("deleteWorkflow() failed: %v", err)
	}

	if _, err := os.Stat(workflowDir); !os.IsNotExist(err) {
		t.Fatalf("expected workflow directory to be deleted, stat err: %v", err)
	}
}
