// Package store persists a fixed set of $HOME paths somewhere durable.
// It is sync only: what changed, capture it, record it, publish it.
package store

import "context"

// Root is a $HOME-relative path the store tracks. Exclude lists child
// basenames that must never be captured.
type Root struct {
	Rel     string
	Exclude []string
}

type ChangeKind int

const (
	Modified ChangeKind = iota
	Added
	Deleted
)

func (k ChangeKind) String() string {
	switch k {
	case Modified:
		return "M"
	case Added:
		return "A"
	case Deleted:
		return "D"
	}
	return "?"
}

// Change is a $HOME-relative path whose working copy differs from the store.
type Change struct {
	Path string
	Kind ChangeKind
}

type Status struct {
	Uncaptured  []Change // $HOME differs from the store working copy
	Uncommitted []string // store working copy differs from its last commit
	Branch      string
	Ahead       int // commits not yet pushed; -1 when there is no upstream
}

func (s Status) Clean() bool {
	return len(s.Uncaptured) == 0 && len(s.Uncommitted) == 0
}

type Store interface {
	Status(ctx context.Context) (Status, error)
	Capture(ctx context.Context) error
	Diff(ctx context.Context) (string, error)
	Commit(ctx context.Context, message string) error
	Push(ctx context.Context) error
}
