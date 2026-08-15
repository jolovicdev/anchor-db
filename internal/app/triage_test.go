package app_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jolovicdev/anchor-db/internal/app"
	"github.com/jolovicdev/anchor-db/internal/domain"
)

// A refactor that renames a function and rewords its body defeats automatic
// resolution, which is exactly when a reviewer needs suggestions.
func TestTriageLoopRelocatesARenamedFunction(t *testing.T) {
	ctx := context.Background()
	source := "package demo\n\nfunc chargeCard(amount int) error {\n\tvalidate(amount)\n\tsubmit(amount)\n\treturn nil\n}\n"
	root := gitRepoWithFile(t, "billing.go", source)
	svc, repo := serviceFor(t, root)

	anchor, err := svc.CreateAnchor(ctx, app.CreateAnchorInput{
		RepoID: repo.ID, Ref: "WORKTREE", Path: "billing.go",
		StartLine: 3, StartCol: 1, EndLine: 7, EndCol: 2,
		Kind: "warning", Title: "not idempotent", Body: "Retrying double-bills.", Author: "a",
	})
	if err != nil {
		t.Fatalf("create anchor: %v", err)
	}

	// Rename the function and move it down; keep the body recognisable.
	refactored := "package demo\n\n// Billing entry points.\n// See INC-4711.\n\nfunc ChargePaymentCard(amount int) error {\n\tvalidate(amount)\n\tsubmit(amount)\n\treturn nil\n}\n"
	if err := os.WriteFile(filepath.Join(root, "billing.go"), []byte(refactored), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if _, err := svc.ResolvePath(ctx, repo.ID, "WORKTREE", "billing.go"); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	current, err := svc.GetAnchor(ctx, anchor.ID)
	if err != nil {
		t.Fatalf("get anchor: %v", err)
	}
	if current.Status != domain.AnchorStatusStale {
		t.Skipf("anchor resolved automatically (status %q); triage path not exercised", current.Status)
	}

	// It should show up in the queue, carrying a reason and a suggestion.
	queue, err := svc.StaleQueue(ctx, repo.ID, 0)
	if err != nil {
		t.Fatalf("stale queue: %v", err)
	}
	if len(queue) != 1 || queue[0].Anchor.ID != anchor.ID {
		t.Fatalf("expected the anchor in the stale queue, got %d entries", len(queue))
	}
	if queue[0].StaleReason == "" {
		t.Error("queue entry should carry the reason it went stale")
	}
	if queue[0].BestCandidate == nil {
		t.Fatal("queue entry should carry a best-guess relocation")
	}

	candidates, err := svc.RelocationCandidates(ctx, anchor.ID)
	if err != nil {
		t.Fatalf("candidates: %v", err)
	}
	if len(candidates) == 0 {
		t.Fatal("expected at least one relocation candidate")
	}
	for idx, candidate := range candidates {
		if candidate.Index != idx {
			t.Errorf("candidate %d reports index %d", idx, candidate.Index)
		}
		if candidate.Confidence <= 0 || candidate.Confidence > 1 {
			t.Errorf("candidate %d confidence %v out of range", idx, candidate.Confidence)
		}
		if candidate.Preview == "" {
			t.Errorf("candidate %d has no preview", idx)
		}
		if candidate.Reason == "" {
			t.Errorf("candidate %d has no reason", idx)
		}
	}
	// Ranked strongest first.
	for idx := 1; idx < len(candidates); idx++ {
		if candidates[idx].Confidence > candidates[idx-1].Confidence {
			t.Errorf("candidates are not ranked: %v before %v",
				candidates[idx-1].Confidence, candidates[idx].Confidence)
		}
	}

	// The best suggestion should be the relocated function body.
	top := candidates[0]
	if !strings.Contains(top.Preview, "validate(amount)") {
		t.Errorf("top candidate preview does not look like the anchored code: %q", top.Preview)
	}

	index := 0
	relocated, err := svc.AcceptRelocation(ctx, app.RelocateInput{AnchorID: anchor.ID, Candidate: &index})
	if err != nil {
		t.Fatalf("accept relocation: %v", err)
	}
	if relocated.Status != domain.AnchorStatusActive {
		t.Errorf("status after relocation = %q, want active", relocated.Status)
	}
	if !strings.Contains(relocated.Binding.SelectedText, "validate(amount)") {
		t.Errorf("relocated text = %q, want the function body", relocated.Binding.SelectedText)
	}
	// The anchor should now be re-pinned inside the renamed function.
	if relocated.Binding.SymbolPath != "ChargePaymentCard" {
		t.Errorf("symbol after relocation = %q, want ChargePaymentCard", relocated.Binding.SymbolPath)
	}
	if relocated.Binding.BaseCommit == "" {
		t.Error("relocation should re-base the anchor onto the current commit")
	}

	// The queue should now be empty.
	queue, err = svc.StaleQueue(ctx, repo.ID, 0)
	if err != nil {
		t.Fatalf("stale queue after relocation: %v", err)
	}
	if len(queue) != 0 {
		t.Errorf("expected an empty queue after relocation, got %d", len(queue))
	}

	// And the relocation should be recorded in the anchor's history.
	events, err := svc.ListAnchorEvents(ctx, anchor.ID)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	var recorded bool
	for _, event := range events {
		if strings.Contains(event.Reason, "relocation accepted") {
			recorded = true
		}
	}
	if !recorded {
		t.Error("accepting a relocation should be recorded in the anchor history")
	}
}

func TestAcceptRelocationWithExplicitSpan(t *testing.T) {
	ctx := context.Background()
	source := "package demo\n\nfunc a() int {\n\treturn 1\n}\n\nfunc b() int {\n\treturn 2\n}\n"
	root := gitRepoWithFile(t, "e.go", source)
	svc, repo := serviceFor(t, root)

	anchor, err := svc.CreateAnchor(ctx, app.CreateAnchorInput{
		RepoID: repo.ID, Ref: "WORKTREE", Path: "e.go",
		StartLine: 3, StartCol: 1, EndLine: 5, EndCol: 2,
		Kind: "warning", Title: "t", Body: "b", Author: "a",
	})
	if err != nil {
		t.Fatalf("create anchor: %v", err)
	}

	moved, err := svc.AcceptRelocation(ctx, app.RelocateInput{
		AnchorID: anchor.ID, StartLine: 7, StartCol: 1, EndLine: 9, EndCol: 2,
	})
	if err != nil {
		t.Fatalf("accept relocation: %v", err)
	}
	if moved.Binding.StartLine != 7 || moved.Binding.EndLine != 9 {
		t.Errorf("span = %d-%d, want 7-9", moved.Binding.StartLine, moved.Binding.EndLine)
	}
	if moved.Binding.SymbolPath != "b" {
		t.Errorf("symbol = %q, want b: the symbol must be re-derived at the new location",
			moved.Binding.SymbolPath)
	}
	if !strings.Contains(moved.Binding.SelectedText, "return 2") {
		t.Errorf("selected text = %q, want it re-read from the file", moved.Binding.SelectedText)
	}
}

func TestAcceptRelocationRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	source := "package demo\n\nfunc a() int {\n\treturn 1\n}\n"
	root := gitRepoWithFile(t, "r.go", source)
	svc, repo := serviceFor(t, root)

	anchor, err := svc.CreateAnchor(ctx, app.CreateAnchorInput{
		RepoID: repo.ID, Ref: "WORKTREE", Path: "r.go",
		StartLine: 3, StartCol: 1, EndLine: 5, EndCol: 2,
		Kind: "warning", Title: "t", Body: "b", Author: "a",
	})
	if err != nil {
		t.Fatalf("create anchor: %v", err)
	}

	outOfRange := 99
	cases := map[string]app.RelocateInput{
		"candidate out of range": {AnchorID: anchor.ID, Candidate: &outOfRange},
		"zero width span":        {AnchorID: anchor.ID, StartLine: 3, StartCol: 1, EndLine: 3, EndCol: 1},
		"inverted span":          {AnchorID: anchor.ID, StartLine: 5, StartCol: 1, EndLine: 3, EndCol: 1},
		"non positive line":      {AnchorID: anchor.ID, StartLine: 0, StartCol: 1, EndLine: 3, EndCol: 1},
		"past end of file":       {AnchorID: anchor.ID, StartLine: 900, StartCol: 1, EndLine: 901, EndCol: 1},
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.AcceptRelocation(ctx, input); err == nil {
				t.Error("expected rejection")
			}
		})
	}
}

// The queue promises "most recently affected first", and a limit must keep that
// end. Truncating an oldest-first list drops exactly the anchors that broke
// most recently, which are the ones a reviewer came for.
func TestStaleQueueReturnsMostRecentlyAffectedFirst(t *testing.T) {
	ctx := context.Background()
	root := gitRepoWithFile(t, "a.py", "def one():\n    return 1\n\n\ndef two():\n    return 2\n")
	svc, repo := serviceFor(t, root)

	var ids []string
	for i, span := range [][2]int{{1, 2}, {5, 6}} {
		anchor, err := svc.CreateAnchor(ctx, app.CreateAnchorInput{
			RepoID: repo.ID, Ref: "WORKTREE", Path: "a.py",
			StartLine: span[0], StartCol: 1, EndLine: span[1], EndCol: 99,
			Kind: "todo", Title: fmt.Sprintf("a%d", i), Body: "b", Author: "x",
		})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		ids = append(ids, anchor.ID)
	}

	// Break both, oldest-created first, so creation order and break order agree.
	if err := os.WriteFile(filepath.Join(root, "a.py"),
		[]byte("def alpha(x):\n    return x * 2\n\n\ndef beta(y):\n    return y * 3\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := svc.SyncRepo(ctx, repo.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}

	entries, err := svc.StaleQueue(ctx, repo.ID, 0)
	if err != nil {
		t.Fatalf("stale queue: %v", err)
	}
	if len(entries) < 2 {
		t.Fatalf("expected both anchors stale, got %d", len(entries))
	}
	for i := 1; i < len(entries); i++ {
		if entries[i].Anchor.UpdatedAt.After(entries[i-1].Anchor.UpdatedAt) {
			t.Errorf("entry %d is newer than entry %d: queue is not most-recent-first", i, i-1)
		}
	}

	// A limit must keep the head of that order, not the tail.
	limited, err := svc.StaleQueue(ctx, repo.ID, 1)
	if err != nil {
		t.Fatalf("stale queue: %v", err)
	}
	if len(limited) != 1 {
		t.Fatalf("limit 1 returned %d entries", len(limited))
	}
	if limited[0].Anchor.ID != entries[0].Anchor.ID {
		t.Errorf("limit kept %s, want the most recently affected %s",
			limited[0].Anchor.ID, entries[0].Anchor.ID)
	}
	_ = ids
}
