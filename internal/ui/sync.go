package ui

import (
	"context"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/jegork/skillet/internal/store"
)

type syncStep int

const (
	syncCapturing syncStep = iota
	syncReview
	syncCommitting
	syncPushing
)

type syncState struct {
	step syncStep
	diff viewport.Model
	msg  textinput.Model
	push bool
}

type capturedMsg struct {
	diff string
	err  error
}
type committedMsg struct{ err error }
type pushedMsg struct{ err error }
type statusMsg struct {
	status store.Status
	err    error
}

const defaultCommitMessage = "chore(skills): sync"

func newSyncState() syncState {
	ti := textinput.New()
	ti.Prompt = "message: "
	ti.SetValue(defaultCommitMessage)
	ti.CursorEnd()
	return syncState{diff: viewport.New(), msg: ti, push: true}
}

func loadStatus(s store.Store) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		st, err := s.Status(ctx)
		return statusMsg{st, err}
	}
}

func captureAndDiff(s store.Store) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := s.Capture(ctx); err != nil {
			return capturedMsg{err: err}
		}
		diff, err := s.Diff(ctx)
		return capturedMsg{diff: diff, err: err}
	}
}

func commit(s store.Store, message string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return committedMsg{s.Commit(ctx, message)}
	}
}

func push(s store.Store) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		return pushedMsg{s.Push(ctx)}
	}
}
