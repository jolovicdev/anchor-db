package domain

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

var ErrNotFound = errors.New("not found")

type BindingType string

const (
	BindingTypeFile   BindingType = "file"
	BindingTypeSpan   BindingType = "span"
	BindingTypeSymbol BindingType = "symbol"
)

type AnchorKind string

const (
	AnchorKindWarning   AnchorKind = "warning"
	AnchorKindTodo      AnchorKind = "todo"
	AnchorKindHandoff   AnchorKind = "handoff"
	AnchorKindRationale AnchorKind = "rationale"
	AnchorKindInvariant AnchorKind = "invariant"
	AnchorKindQuestion  AnchorKind = "question"
)

type AnchorStatus string

const (
	AnchorStatusActive   AnchorStatus = "active"
	AnchorStatusResolved AnchorStatus = "resolved"
	AnchorStatusStale    AnchorStatus = "stale"
	AnchorStatusArchived AnchorStatus = "archived"
)

type AnchorEventType string

const (
	AnchorEventCreated AnchorEventType = "created"
	AnchorEventUpdated AnchorEventType = "updated"
	AnchorEventMoved   AnchorEventType = "moved"
	AnchorEventStale   AnchorEventType = "stale"
)

type Repo struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	RootPath   string    `json:"root_path"`
	DefaultRef string    `json:"default_ref"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Binding struct {
	Type             BindingType `json:"type"`
	Ref              string      `json:"ref"`
	Path             string      `json:"path"`
	Language         string      `json:"language"`
	SymbolPath       string      `json:"symbol_path,omitempty"`
	StartLine        int         `json:"start_line"`
	StartCol         int         `json:"start_col"`
	EndLine          int         `json:"end_line"`
	EndCol           int         `json:"end_col"`
	SelectedText     string      `json:"selected_text"`
	SelectedTextHash string      `json:"selected_text_hash"`
	BeforeContext    string      `json:"before_context,omitempty"`
	BeforeHash       string      `json:"before_hash,omitempty"`
	AfterContext     string      `json:"after_context,omitempty"`
	AfterHash        string      `json:"after_hash,omitempty"`
	Confidence       float64     `json:"confidence"`
}

type Anchor struct {
	ID        string       `json:"id"`
	RepoID    string       `json:"repo_id"`
	Kind      AnchorKind   `json:"kind"`
	Status    AnchorStatus `json:"status"`
	Title     string       `json:"title"`
	Body      string       `json:"body"`
	Author    string       `json:"author"`
	SourceRef string       `json:"source_ref"`
	Tags      []string     `json:"tags"`
	Binding   Binding      `json:"binding"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

type Symbol struct {
	Path       string `json:"path"`
	Language   string `json:"language"`
	Kind       string `json:"kind"`
	SymbolPath string `json:"symbol_path"`
	StartLine  int    `json:"start_line"`
	StartCol   int    `json:"start_col"`
	EndLine    int    `json:"end_line"`
	EndCol     int    `json:"end_col"`
}

type AnchorFilter struct {
	RepoID     string
	Path       string
	SymbolPath string
	Status     AnchorStatus
}

type AnchorEvent struct {
	ID          string          `json:"id"`
	AnchorID    string          `json:"anchor_id"`
	Type        AnchorEventType `json:"type"`
	Reason      string          `json:"reason"`
	Confidence  float64         `json:"confidence"`
	FromBinding *Binding        `json:"from_binding,omitempty"`
	ToBinding   *Binding        `json:"to_binding,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

type Comment struct {
	ID        string    `json:"id"`
	AnchorID  string    `json:"anchor_id"`
	ParentID  string    `json:"parent_id,omitempty"`
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

func NewID(prefix string) string {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return prefix + "-fallback"
	}
	return prefix + "-" + hex.EncodeToString(bytes)
}
