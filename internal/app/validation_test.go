package app_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jolovicdev/anchor-db/internal/app"
	"github.com/jolovicdev/anchor-db/internal/domain"
)

const sampleModule = `from uuid import uuid4


class Task:
    def __init__(self, title):
        self.id = str(uuid4())
        self.title = title
`

func moduleService(t *testing.T) (*app.Service, domain.Repo) {
	t.Helper()
	return serviceFor(t, gitRepoWithFile(t, "models.py", sampleModule))
}

func createInput(repoID string) app.CreateAnchorInput {
	return app.CreateAnchorInput{
		RepoID: repoID, Ref: "WORKTREE", Path: "models.py",
		StartLine: 1, StartCol: 1, EndLine: 1, EndCol: 23,
		Kind: "todo", Title: "t", Body: "b", Author: "human://test",
	}
}

// An unrecognized kind used to be replaced by the default, so a typo was stored
// as a "warning" and the mistake was invisible from then on.
func TestCreateAnchorRejectsUnknownKind(t *testing.T) {
	ctx := context.Background()
	svc, repo := moduleService(t)

	input := createInput(repo.ID)
	input.Kind = "banana"
	_, err := svc.CreateAnchor(ctx, input)
	if err == nil {
		t.Fatal("an unknown kind was accepted")
	}
	for _, want := range []string{"banana", "warning", "todo"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got %q", want, err)
		}
	}
}

func TestUpdateAnchorRejectsUnknownKind(t *testing.T) {
	ctx := context.Background()
	svc, repo := moduleService(t)

	anchor, err := svc.CreateAnchor(ctx, createInput(repo.ID))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.UpdateAnchor(ctx, app.UpdateAnchorInput{ID: anchor.ID, Kind: "banana3"}); err == nil {
		t.Fatal("an unknown kind was accepted on update")
	}
	// The stored kind must be untouched by the rejected update.
	after, err := svc.GetAnchor(ctx, anchor.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if after.Kind != domain.AnchorKindTodo {
		t.Errorf("kind changed to %q despite the update failing", after.Kind)
	}
}

func TestCreateAnchorAcceptsEveryValidKind(t *testing.T) {
	ctx := context.Background()
	svc, repo := moduleService(t)

	for _, kind := range domain.AnchorKinds {
		input := createInput(repo.ID)
		input.Kind = string(kind)
		anchor, err := svc.CreateAnchor(ctx, input)
		if err != nil {
			t.Fatalf("kind %q rejected: %v", kind, err)
		}
		if anchor.Kind != kind {
			t.Errorf("kind %q stored as %q", kind, anchor.Kind)
		}
	}
}

// An explicit symbol was stored without checking it existed, so an anchor could
// claim a symbol defined in a different file. Symbol matching would then move
// the anchor onto whatever that name later resolved to.
func TestCreateAnchorRejectsSymbolThatIsNotInTheFile(t *testing.T) {
	ctx := context.Background()
	svc, repo := moduleService(t)

	input := createInput(repo.ID)
	input.Symbol = "JsonStorage.save"
	_, err := svc.CreateAnchor(ctx, input)
	if err == nil {
		t.Fatal("a symbol from another file was accepted")
	}
	if !strings.Contains(err.Error(), "JsonStorage.save") || !strings.Contains(err.Error(), "models.py") {
		t.Errorf("error should name the symbol and the file, got %q", err)
	}
}

func TestCreateAnchorAcceptsSymbolThatIsInTheFile(t *testing.T) {
	ctx := context.Background()
	svc, repo := moduleService(t)

	input := createInput(repo.ID)
	input.Symbol = "Task"
	anchor, err := svc.CreateAnchor(ctx, input)
	if err != nil {
		t.Fatalf("a symbol defined in the file was rejected: %v", err)
	}
	if anchor.Binding.SymbolPath != "Task" {
		t.Errorf("symbol stored as %q, want Task", anchor.Binding.SymbolPath)
	}
}

// Registering the same checkout twice produced two repo records, splitting one
// repository's anchors across IDs that no query joins back together.
func TestRegisterRepoIsIdempotentForTheSamePath(t *testing.T) {
	ctx := context.Background()
	root := gitRepoWithFile(t, "models.py", sampleModule)
	svc, first := serviceFor(t, root)

	second, err := svc.RegisterRepo(ctx, "different-name", root)
	if err != nil {
		t.Fatalf("re-register: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("re-registering created repo %s, want the existing %s", second.ID, first.ID)
	}
	repos, err := svc.ListRepos(ctx)
	if err != nil {
		t.Fatalf("list repos: %v", err)
	}
	if len(repos) != 1 {
		t.Errorf("got %d repo records, want 1", len(repos))
	}
}

// A missing parent used to fail on the database foreign key, which surfaced a
// SQLite constraint number rather than saying what was wrong.
func TestCommentRejectsUnknownParentPlainly(t *testing.T) {
	ctx := context.Background()
	svc, repo := moduleService(t)

	anchor, err := svc.CreateAnchor(ctx, createInput(repo.ID))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = svc.CreateComment(ctx, anchor.ID, "comment-ghost", "human://test", "hi")
	if err == nil {
		t.Fatal("a reply to a non-existent comment was accepted")
	}
	if !strings.Contains(err.Error(), "parent comment") {
		t.Errorf("error should say the parent was not found, got %q", err)
	}
	for _, leak := range []string{"FOREIGN KEY", "constraint", "787"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("error leaks the storage layer (%q): %q", leak, err)
		}
	}
}

func TestCommentAcceptsRealParent(t *testing.T) {
	ctx := context.Background()
	svc, repo := moduleService(t)

	anchor, err := svc.CreateAnchor(ctx, createInput(repo.ID))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	parent, err := svc.CreateComment(ctx, anchor.ID, "", "human://test", "top")
	if err != nil {
		t.Fatalf("create parent comment: %v", err)
	}
	reply, err := svc.CreateComment(ctx, anchor.ID, parent.ID, "human://test", "reply")
	if err != nil {
		t.Fatalf("reply to a real parent was rejected: %v", err)
	}
	if reply.ParentID != parent.ID {
		t.Errorf("reply parent = %q, want %q", reply.ParentID, parent.ID)
	}
}

// Passing neither a candidate nor a range reported that line and column values
// must be positive, sending callers hunting for a bad number they never gave.
func TestRelocateWithoutTargetSaysWhatIsMissing(t *testing.T) {
	ctx := context.Background()
	svc, repo := moduleService(t)

	anchor, err := svc.CreateAnchor(ctx, createInput(repo.ID))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = svc.AcceptRelocation(ctx, app.RelocateInput{AnchorID: anchor.ID})
	if err == nil {
		t.Fatal("relocating to nothing was accepted")
	}
	if !strings.Contains(err.Error(), "candidate") {
		t.Errorf("error should name the missing arguments, got %q", err)
	}
	if strings.Contains(err.Error(), "must be positive") {
		t.Errorf("error still blames the values that were never supplied: %q", err)
	}
}
