package ui

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
)

// every keyMap field must carry keys: bindings have been dropped by accident
// more than once while other fields were added next to them
func TestEveryBindingHasKeys(t *testing.T) {
	km := newKeyMap()
	v := reflect.ValueOf(km)
	for i := 0; i < v.NumField(); i++ {
		b, ok := v.Field(i).Interface().(key.Binding)
		if !ok {
			continue
		}
		if len(b.Keys()) == 0 {
			t.Errorf("keyMap.%s has no keys bound", v.Type().Field(i).Name)
		}
		if b.Help().Key == "" {
			t.Errorf("keyMap.%s has no help text", v.Type().Field(i).Name)
		}
	}
}

// the help screen is built from FullHelp: a binding missing there is invisible
// to the user even when it works. Confirm and TogglePush only exist inside
// the sync review, which shows its own key line.
func TestFullHelpListsEveryBinding(t *testing.T) {
	km := newKeyMap()
	contextual := map[string]bool{"Confirm": true, "TogglePush": true}
	listed := map[string]bool{}
	for _, row := range km.FullHelp() {
		for _, b := range row {
			listed[b.Help().Key] = true
		}
	}
	v := reflect.ValueOf(km)
	for i := 0; i < v.NumField(); i++ {
		b, ok := v.Field(i).Interface().(key.Binding)
		if !ok || b.Help().Key == "" || contextual[v.Type().Field(i).Name] {
			continue
		}
		if !listed[b.Help().Key] {
			t.Errorf("keyMap.%s (%s) is not in FullHelp", v.Type().Field(i).Name, b.Help().Key)
		}
	}
}

// the README keymap table is hand-maintained and rows have been dropped in
// merges: every binding's help key must appear in the table's key column.
// Confirm, TogglePush and Back are described in prose under the table.
func TestReadmeListsEveryBinding(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	inTable := false
	keys := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		switch {
		case strings.HasPrefix(line, "| key | action |"):
			inTable = true
		case inTable && !strings.HasPrefix(line, "|"):
			inTable = false
		case inTable:
			cell := strings.SplitN(strings.TrimPrefix(line, "|"), "|", 2)[0]
			for _, m := range regexp.MustCompile("`([^`]+)`").FindAllStringSubmatch(cell, -1) {
				keys[m[1]] = true
			}
		}
	}
	if len(keys) == 0 {
		t.Fatal("no keymap table found in README")
	}
	skip := map[string]bool{"Confirm": true, "TogglePush": true, "Back": true}
	km := newKeyMap()
	v := reflect.ValueOf(km)
	for i := 0; i < v.NumField(); i++ {
		bnd, ok := v.Field(i).Interface().(key.Binding)
		name := v.Type().Field(i).Name
		if !ok || skip[name] {
			continue
		}
		// help keys like "c/x/o" list alternatives; "/" itself is a key
		found := keys[bnd.Help().Key]
		for _, k := range strings.Split(bnd.Help().Key, "/") {
			if keys[k] {
				found = true
			}
		}
		if !found {
			t.Errorf("keyMap.%s (%s) has no row in the README keymap table", name, bnd.Help().Key)
		}
	}
}
