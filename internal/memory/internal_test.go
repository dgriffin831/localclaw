package memory

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDiscoverMemoryFilesDeterministicAcrossPathForms(t *testing.T) {
	workspace := t.TempDir()

	mustWriteFile(t, filepath.Join(workspace, "MEMORY.md"), "root memory")
	mustWriteFile(t, filepath.Join(workspace, "memory", "alpha.md"), "alpha")
	mustWriteFile(t, filepath.Join(workspace, "memory", "nested", "beta.md"), "beta")
	mustWriteFile(t, filepath.Join(workspace, "notes.txt"), "ignore")

	outside := filepath.Join(t.TempDir(), "outside.md")
	mustWriteFile(t, outside, "outside")

	filesA, err := DiscoverMemoryFiles(workspace, []string{
		"memory",
		"./memory/../memory/nested/beta.md",
		outside,
	})
	if err != nil {
		t.Fatalf("discover files A: %v", err)
	}

	filesB, err := DiscoverMemoryFiles(filepath.Join(workspace, "."), []string{
		"./memory",
		"memory/nested/../nested",
		filepath.Join(filepath.Dir(outside), ".", "outside.md"),
	})
	if err != nil {
		t.Fatalf("discover files B: %v", err)
	}

	if !reflect.DeepEqual(filesA, filesB) {
		t.Fatalf("expected deterministic file discovery across path forms\nA=%v\nB=%v", filesA, filesB)
	}

	gotRel := relativePaths(filesA)
	wantRel := []string{"MEMORY.md", "memory/alpha.md", "memory/nested/beta.md", ""}
	if !reflect.DeepEqual(gotRel, wantRel) {
		t.Fatalf("unexpected relative paths: got %v want %v", gotRel, wantRel)
	}
}

func TestDiscoverMemoryFilesIgnoresSymlinks(t *testing.T) {
	workspace := t.TempDir()

	mustWriteFile(t, filepath.Join(workspace, "MEMORY.md"), "root")
	mustWriteFile(t, filepath.Join(workspace, "memory", "real.md"), "real")

	symlinkTargetFile := filepath.Join(workspace, "linked-target.md")
	mustWriteFile(t, symlinkTargetFile, "target")
	if err := os.Symlink(symlinkTargetFile, filepath.Join(workspace, "memory", "link.md")); err != nil {
		t.Fatalf("create symlink file: %v", err)
	}

	realDir := filepath.Join(workspace, "memory", "real-dir")
	mustWriteFile(t, filepath.Join(realDir, "child.md"), "child")
	if err := os.Symlink(realDir, filepath.Join(workspace, "memory", "linked-dir")); err != nil {
		t.Fatalf("create symlink dir: %v", err)
	}

	extraDir := t.TempDir()
	mustWriteFile(t, filepath.Join(extraDir, "extra.md"), "extra")
	extraLinkedDir := filepath.Join(filepath.Dir(extraDir), "extra-linked")
	if err := os.Symlink(extraDir, extraLinkedDir); err != nil {
		t.Fatalf("create extra dir symlink: %v", err)
	}

	files, err := DiscoverMemoryFiles(workspace, []string{extraDir, extraLinkedDir})
	if err != nil {
		t.Fatalf("discover files: %v", err)
	}

	gotRel := relativePaths(files)
	wantRel := []string{"MEMORY.md", "memory/real-dir/child.md", "memory/real.md", ""}
	if !reflect.DeepEqual(gotRel, wantRel) {
		t.Fatalf("unexpected files after symlink filtering: got %v want %v", gotRel, wantRel)
	}

	for _, file := range files {
		if filepath.Base(file.AbsolutePath) == "link.md" {
			t.Fatalf("symlink file should be ignored: %v", file)
		}
		if filepath.Base(file.AbsolutePath) == "linked-dir" {
			t.Fatalf("symlink directory should be ignored: %v", file)
		}
	}
}

func TestDiscoverMemoryFilesIgnoresFilesWithinSymlinkedDirectories(t *testing.T) {
	workspace := t.TempDir()
	mustWriteFile(t, filepath.Join(workspace, "MEMORY.md"), "root")

	targetDir := t.TempDir()
	targetFile := filepath.Join(targetDir, "nested.md")
	mustWriteFile(t, targetFile, "from symlinked dir")

	symlinkDir := filepath.Join(workspace, "memory-link")
	if err := os.Symlink(targetDir, symlinkDir); err != nil {
		t.Fatalf("create symlink dir: %v", err)
	}

	files, err := DiscoverMemoryFiles(workspace, []string{filepath.Join("memory-link", "nested.md")})
	if err != nil {
		t.Fatalf("discover files: %v", err)
	}

	gotRel := relativePaths(files)
	wantRel := []string{"MEMORY.md"}
	if !reflect.DeepEqual(gotRel, wantRel) {
		t.Fatalf("unexpected files for symlinked parent path: got %v want %v", gotRel, wantRel)
	}
}

func TestChunkTextOverlapAndHash(t *testing.T) {
	chunks := ChunkText("abcdefghijklmnopqrstuvwxyz", 2, 1)

	got := make([]string, 0, len(chunks))
	for _, c := range chunks {
		got = append(got, c.Text)
		if c.Hash != HashText(c.Text) {
			t.Fatalf("chunk hash mismatch for %q", c.Text)
		}
	}

	want := []string{"abcdefgh", "efghijkl", "ijklmnop", "mnopqrst", "qrstuvwx", "uvwxyz"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected chunk overlap behavior: got %v want %v", got, want)
	}

	empty := ChunkText(" \n\t ", 5, 1)
	if len(empty) != 0 {
		t.Fatalf("expected empty chunks to be filtered, got %v", empty)
	}
}

func TestPathNormalizationHelpers(t *testing.T) {
	workspace := t.TempDir()

	rel, err := NormalizeRelativePath("memory/../memory/nested/./note.md")
	if err != nil {
		t.Fatalf("normalize relative path: %v", err)
	}
	if rel != "memory/nested/note.md" {
		t.Fatalf("unexpected normalized rel path %q", rel)
	}

	if _, err := NormalizeRelativePath("../escape.md"); err == nil {
		t.Fatalf("expected traversal path rejection")
	}

	if _, err := NormalizeRelativePath(filepath.Join(string(filepath.Separator), "abs.md")); err == nil {
		t.Fatalf("expected absolute path rejection")
	}

	inside := filepath.Join(workspace, "memory", "a.md")
	mustWriteFile(t, inside, "a")

	safeRel, err := SafeRelativePath(workspace, inside)
	if err != nil {
		t.Fatalf("safe relative path inside root: %v", err)
	}
	if safeRel != "memory/a.md" {
		t.Fatalf("unexpected safe relative path %q", safeRel)
	}

	outside := filepath.Join(t.TempDir(), "outside.md")
	mustWriteFile(t, outside, "outside")
	if _, err := SafeRelativePath(workspace, outside); err == nil {
		t.Fatalf("expected outside-root rejection")
	}
}

func relativePaths(files []MemoryFile) []string {
	out := make([]string, 0, len(files))
	for _, file := range files {
		out = append(out, file.RelativePath)
	}
	return out
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("mkdir %q: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}
