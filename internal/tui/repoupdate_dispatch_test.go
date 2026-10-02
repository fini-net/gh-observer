package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// repoModelFrom extracts a *RepoModel from the tea.Model returned by
// Update, which may be either RepoModel or *RepoModel (handlers use
// pointer receivers).
func repoModelFrom(t *testing.T, m tea.Model) *RepoModel {
	t.Helper()
	switch v := m.(type) {
	case RepoModel:
		return &v
	case *RepoModel:
		return v
	default:
		t.Fatalf("Update returned %T, want RepoModel or *RepoModel", m)
		return nil
	}
}

func TestRepoUpdateKeyMsgQuits(t *testing.T) {
	keys := []tea.KeyPressMsg{
		tea.KeyPressMsg(tea.Key{Text: "q", Code: 'q'}),
		tea.KeyPressMsg(tea.Key{Text: "ctrl+c", Code: 3, Mod: tea.ModCtrl}),
	}
	for _, key := range keys {
		t.Run(key.String(), func(t *testing.T) {
			m := makeRepoModelForRender()
			newModel, cmd := m.Update(key)
			rm := repoModelFrom(t, newModel)
			if !rm.quitting {
				t.Error("quitting should be true after key press")
			}
			if cmd == nil {
				t.Error("Update should return a quit command")
			}
		})
	}
}

func TestRepoUpdateTickMsg(t *testing.T) {
	t.Run("normal tick dispatches fetch batch", func(t *testing.T) {
		m := makeRepoModelForRender()
		_, cmd := m.Update(RepoTickMsg{})
		if cmd == nil {
			t.Error("tick should return a fetch batch command")
		}
	})

	t.Run("low rate limit backs off", func(t *testing.T) {
		m := makeRepoModelForRender()
		m.fetchReceived = true
		m.rateLimitRemaining = 5
		_, cmd := m.Update(RepoTickMsg{})
		if cmd == nil {
			t.Error("backoff tick should still schedule the next tick")
		}
	})

	t.Run("zero rate limit before first fetch does not back off", func(t *testing.T) {
		m := makeRepoModelForRender()
		m.fetchReceived = false
		m.rateLimitRemaining = 0
		_, cmd := m.Update(RepoTickMsg{})
		if cmd == nil {
			t.Error("pre-first-fetch tick should dispatch fetches (no backoff)")
		}
	})
}

func TestRepoUpdateSpinnerTick(t *testing.T) {
	m := makeRepoModelForRender()
	cmd := m.spinner.Tick
	msg := cmd()
	newModel, _ := m.Update(msg)
	if newModel == nil {
		t.Error("spinner tick should return a model")
	}
}

func TestRepoUpdateUnknownMsg(t *testing.T) {
	m := makeRepoModelForRender()
	newModel, cmd := m.Update("unknown-message")
	if cmd != nil {
		t.Error("unknown message should return nil command")
	}
	if repoModelFrom(t, newModel).quitting {
		t.Error("unknown message should not quit")
	}
}

func TestRepoUpdateInit(t *testing.T) {
	m := makeRepoModelForRender()
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init should return a batch command")
	}
}

func TestModelInit(t *testing.T) {
	m := NewModel(
		nil,
		"token",
		"github.com",
		"owner",
		"repo",
		1,
		time.Second,
		NewStyles(2, 1, 3, 8),
		false,
		false,
		nil,
		false,
		time.Minute,
		time.Second,
		time.Second,
	)
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init should return a batch command")
	}
}
