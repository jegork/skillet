package ui

import (
	"reflect"
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
