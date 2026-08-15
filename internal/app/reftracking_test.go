package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jolovicdev/anchor-db/internal/app"
	"github.com/jolovicdev/anchor-db/internal/domain"
)

// The shape reported from a real session: a private helper gets a new sibling
// above it and its body is rewritten, so the anchored symbol both moves and
// changes.
const serviceBefore = `from pathlib import Path

from .models import Task, TaskNotFound
from .storage import JsonStorage


class TodoService:
    def __init__(self, path: Path | str = "tasks.json"):
        self.storage = JsonStorage(Path(path))
        self.tasks = self.storage.load()

    def _find(self, task_id: str) -> Task:
        for task in self.tasks:
            if task.id == task_id:
                return task
        raise TaskNotFound(task_id)

    def add(self, title: str) -> Task:
        task = Task(title=title)
        self.tasks.append(task)
        return task
`

const serviceAfter = `from pathlib import Path

from .models import Task, TaskNotFound
from .storage import JsonStorage


class TodoService:
    def __init__(self, path: Path | str = "tasks.json"):
        self.storage = JsonStorage(Path(path))
        self.tasks = self.storage.load()

    def stats(self) -> dict:
        total = len(self.tasks)
        done = sum(1 for task in self.tasks if task.done)
        return {"total": total, "done": done, "open": total - done}

    def _find(self, task_id: str) -> Task:
        for candidate in self.tasks:
            if candidate.id == task_id:
                return candidate
        raise TaskNotFound(task_id)

    def add(self, title: str) -> Task:
        task = Task(title=title)
        self.tasks.append(task)
        return task
`

// _find occupies lines 12-16 before the edit and lines 17-21 after it.
const (
	findStartBefore, findEndBefore = 12, 16
	findStartAfter, findEndAfter   = 17, 21
)

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func anchorOnFind(t *testing.T, svc *app.Service, repoID, ref string) domain.Anchor {
	t.Helper()
	anchor, err := svc.CreateAnchor(context.Background(), app.CreateAnchorInput{
		RepoID: repoID, Ref: ref, Path: "service.py",
		StartLine: findStartBefore, StartCol: 1, EndLine: findEndBefore, EndCol: 36,
		Kind: "warning", Title: "_find must raise, never return None",
		Body:   "Callers rely on the exception; returning None silently corrupts state.",
		Author: "human://test",
	})
	if err != nil {
		t.Fatalf("create anchor: %v", err)
	}
	if !strings.Contains(anchor.Binding.SelectedText, "def _find") {
		t.Fatalf("anchor did not capture _find, got:\n%s", anchor.Binding.SelectedText)
	}
	return anchor
}

// reloaded fetches the anchor as stored, which is what the viewer and the stale
// queue both read.
func reloaded(t *testing.T, svc *app.Service, id string) domain.Anchor {
	t.Helper()
	anchor, err := svc.GetAnchor(context.Background(), id)
	if err != nil {
		t.Fatalf("get anchor: %v", err)
	}
	return anchor
}

// An anchor created against a commit was afterwards re-resolved against that
// same commit, where it trivially still matched. The edit was invisible: the
// anchor stayed active on line numbers that now belong to a different function,
// and never entered the stale queue.
func TestSyncFollowsTheWorkingTreeNotTheCreationRef(t *testing.T) {
	ctx := context.Background()
	root := gitRepoWithFile(t, "service.py", serviceBefore)
	svc, repo := serviceFor(t, root)

	// The commit HEAD pointed at when the repo was registered -- the natural
	// thing for a caller to pass as the ref it read the file at.
	anchor := anchorOnFind(t, svc, repo.ID, repo.DefaultRef)

	writeFile(t, root, "service.py", serviceAfter)
	runGit(t, root, "commit", "-am", "refactor _find, add stats")

	if _, err := svc.SyncRepo(ctx, repo.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}

	got := reloaded(t, svc, anchor.ID)
	if got.Status == domain.AnchorStatusActive &&
		got.Binding.StartLine == findStartBefore {
		t.Fatalf("anchor stayed active on its original lines %d-%d, which now hold stats(); "+
			"it was re-resolved against the creation ref instead of the working tree",
			got.Binding.StartLine, got.Binding.EndLine)
	}
	if got.Status == domain.AnchorStatusActive && got.Binding.StartLine != findStartAfter {
		t.Fatalf("anchor is active at lines %d-%d, want %d-%d",
			got.Binding.StartLine, got.Binding.EndLine, findStartAfter, findEndAfter)
	}
	if !strings.Contains(got.Binding.SelectedText, "def _find") {
		t.Fatalf("anchor no longer covers _find:\n%s", got.Binding.SelectedText)
	}
}

// The same edit left uncommitted. Nothing in git has changed, so an anchor that
// resolves against a ref cannot see it at all.
func TestSyncSeesUncommittedWorktreeDrift(t *testing.T) {
	ctx := context.Background()
	root := gitRepoWithFile(t, "service.py", serviceBefore)
	svc, repo := serviceFor(t, root)

	// The commit HEAD pointed at when the repo was registered -- the natural
	// thing for a caller to pass as the ref it read the file at.
	anchor := anchorOnFind(t, svc, repo.ID, repo.DefaultRef)

	// Edited but deliberately not committed.
	writeFile(t, root, "service.py", serviceAfter)

	if _, err := svc.SyncRepo(ctx, repo.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}

	got := reloaded(t, svc, anchor.ID)
	if got.Status == domain.AnchorStatusActive && got.Binding.StartLine == findStartBefore {
		t.Fatalf("uncommitted drift went unnoticed: anchor still active on lines %d-%d, "+
			"which now hold stats()", got.Binding.StartLine, got.Binding.EndLine)
	}
	if !strings.Contains(got.Binding.SelectedText, "def _find") {
		t.Fatalf("anchor no longer covers _find:\n%s", got.Binding.SelectedText)
	}
}

// Relocation candidates are read from the same content resolution uses. Against
// a frozen ref they describe a file nobody is editing any more, so the offered
// range is the one the anchor already has.
func TestRelocationCandidatesComeFromTheWorkingTree(t *testing.T) {
	ctx := context.Background()
	root := gitRepoWithFile(t, "service.py", serviceBefore)
	svc, repo := serviceFor(t, root)

	// The commit HEAD pointed at when the repo was registered -- the natural
	// thing for a caller to pass as the ref it read the file at.
	anchor := anchorOnFind(t, svc, repo.ID, repo.DefaultRef)

	writeFile(t, root, "service.py", serviceAfter)
	runGit(t, root, "commit", "-am", "refactor _find, add stats")

	candidates, err := svc.RelocationCandidates(ctx, anchor.ID)
	if err != nil {
		t.Fatalf("candidates: %v", err)
	}
	if len(candidates) == 0 {
		t.Fatal("no relocation candidates offered")
	}
	// Checking the preview mentions _find is not enough: the pre-edit snapshot
	// contains _find too, so a suggestion read from the creation ref would look
	// right while naming the range the anchor already has.
	for _, candidate := range candidates {
		if candidate.StartLine == findStartAfter && strings.Contains(candidate.Preview, "def _find") {
			return
		}
	}
	for _, candidate := range candidates {
		t.Logf("candidate: lines %d-%d confidence %.2f", candidate.StartLine, candidate.EndLine, candidate.Confidence)
	}
	t.Fatalf("no candidate points at _find's current lines %d-%d; the best offer is lines %d-%d",
		findStartAfter, findEndAfter, candidates[0].StartLine, candidates[0].EndLine)
}

// Accepting a relocation re-reads the span from the file. Reading it from a
// frozen ref silently binds the anchor to whatever occupied those line numbers
// back then, which is how a note about _find ended up on add().
func TestAcceptRelocationReadsTheWorkingTree(t *testing.T) {
	ctx := context.Background()
	root := gitRepoWithFile(t, "service.py", serviceBefore)
	svc, repo := serviceFor(t, root)

	// The commit HEAD pointed at when the repo was registered -- the natural
	// thing for a caller to pass as the ref it read the file at.
	anchor := anchorOnFind(t, svc, repo.ID, repo.DefaultRef)

	writeFile(t, root, "service.py", serviceAfter)
	runGit(t, root, "commit", "-am", "refactor _find, add stats")

	moved, err := svc.AcceptRelocation(ctx, app.RelocateInput{
		AnchorID:  anchor.ID,
		StartLine: findStartAfter, StartCol: 1,
		EndLine: findEndAfter, EndCol: 36,
	})
	if err != nil {
		t.Fatalf("relocate: %v", err)
	}
	if !strings.Contains(moved.Binding.SelectedText, "def _find") {
		t.Fatalf("explicit re-pin to lines %d-%d bound the wrong code:\n%s",
			findStartAfter, findEndAfter, moved.Binding.SelectedText)
	}
	if moved.Status != domain.AnchorStatusActive {
		t.Fatalf("relocated anchor status = %s, want active", moved.Status)
	}
}
