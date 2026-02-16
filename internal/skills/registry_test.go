package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotContainsEligibleSkillsOnly(t *testing.T) {
	reg := NewLocalRegistry()
	workspace := t.TempDir()

	writeSkill(t, workspace, "alpha", `---
name: alpha
description: Alpha summary
---
# Alpha skill`)
	writeSkill(t, workspace, "beta", `---
name: beta
description: Beta summary
---
# Beta skill`)

	snapshot, err := reg.Snapshot(context.Background(), SnapshotRequest{
		WorkspacePath: workspace,
		Disabled:      []string{"beta"},
	})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	if len(snapshot.Skills) != 1 {
		t.Fatalf("expected one eligible skill, got %d", len(snapshot.Skills))
	}
	if snapshot.Skills[0].Name != "alpha" {
		t.Fatalf("expected alpha skill, got %q", snapshot.Skills[0].Name)
	}
}

func TestRenderSnapshotPromptOmitsDisableModelInvocation(t *testing.T) {
	reg := NewLocalRegistry()
	workspace := t.TempDir()

	writeSkill(t, workspace, "alpha", `---
name: alpha
description: Alpha summary
disable-model-invocation: false
---
# Alpha skill`)
	writeSkill(t, workspace, "hidden", `---
name: hidden
description: Hidden summary
disable-model-invocation: true
---
# Hidden skill`)

	snapshot, err := reg.Snapshot(context.Background(), SnapshotRequest{
		WorkspacePath: workspace,
	})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	prompt := RenderSnapshotPrompt(snapshot)
	if !strings.Contains(prompt, "alpha") {
		t.Fatalf("expected prompt to include model-invocable skill")
	}
	if strings.Contains(prompt, "hidden") {
		t.Fatalf("expected prompt to exclude disable-model-invocation skills")
	}
}

func TestSnapshotParsesUserInvocableDefault(t *testing.T) {
	reg := NewLocalRegistry()
	workspace := t.TempDir()

	writeSkill(t, workspace, "alpha", `---
name: alpha
description: Alpha summary
---
# Alpha skill`)

	snapshot, err := reg.Snapshot(context.Background(), SnapshotRequest{
		WorkspacePath: workspace,
	})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(snapshot.Skills) != 1 {
		t.Fatalf("expected one skill, got %d", len(snapshot.Skills))
	}
	if !snapshot.Skills[0].UserInvocable {
		t.Fatalf("expected user-invocable default true")
	}
}

func writeSkill(t *testing.T, workspacePath, skillName, body string) {
	t.Helper()

	dir := filepath.Join(workspacePath, "skills", skillName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatalf("write skill file: %v", err)
	}
}
