// Package config reads and writes ~/.config/skillet/config.yml. Values are
// read and written through a yaml.v3 node round trip so comments survive an
// edit, like the omp adapter in internal/consumer.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type GitStore struct {
	Dir    string
	Remote string
}

type Projects struct {
	Roots []string
	Paths []string
}

type Config struct {
	Store    string
	GitStore GitStore
	Projects Projects
}

// KnownKeys lists the keys accepted by get/set.
var KnownKeys = []string{"store", "git_store.dir", "git_store.remote", "projects.roots", "projects.paths"}

var (
	scalarKeys = map[string]bool{
		"store": true, "git_store.dir": true, "git_store.remote": true,
	}
	listKeys = map[string]bool{
		"projects.roots": true, "projects.paths": true,
	}
)

func Path(home string) string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "skillet", "config.yml")
	}
	return filepath.Join(home, ".config", "skillet", "config.yml")
}

// Load reads the config. A missing or empty file means a zero config, which
// keeps today's built-in defaults in place. Relative "~" paths expand to home.
func Load(home string) (Config, error) {
	var cfg Config
	doc, err := readDoc(Path(home))
	if err != nil || doc == nil {
		return cfg, err
	}
	root := doc.Content[0]
	cfg.Store = childScalar(root, "store")
	if gs := child(root, "git_store", yaml.MappingNode); gs != nil {
		cfg.GitStore.Dir = expand(childScalar(gs, "dir"), home)
		cfg.GitStore.Remote = childScalar(gs, "remote")
	}
	if p := child(root, "projects", yaml.MappingNode); p != nil {
		cfg.Projects.Roots = expandAll(childList(p, "roots"), home)
		cfg.Projects.Paths = expandAll(childList(p, "paths"), home)
	}
	return cfg, nil
}

// Get returns the raw values stored under key, unexpanded: one entry for a
// scalar, one per item for a list. A missing file or absent key yields no
// values.
func Get(home, key string) ([]string, error) {
	if !scalarKeys[key] && !listKeys[key] {
		return nil, fmt.Errorf("unknown config key %q (want %s)", key, strings.Join(KnownKeys, ", "))
	}
	doc, err := readDoc(Path(home))
	if err != nil || doc == nil {
		return nil, err
	}
	parts := strings.Split(key, ".")
	node := doc.Content[0]
	for _, part := range parts[:len(parts)-1] {
		if node = child(node, part, yaml.MappingNode); node == nil {
			return nil, nil
		}
	}
	if node = child(node, parts[len(parts)-1], 0); node == nil {
		return nil, nil
	}
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Tag == "!!null" {
			return nil, nil
		}
		return []string{node.Value}, nil
	case yaml.SequenceNode:
		values := make([]string, 0, len(node.Content))
		for _, c := range node.Content {
			if c.Tag != "!!null" {
				values = append(values, c.Value)
			}
		}
		return values, nil
	default:
		return nil, fmt.Errorf("%s is a mapping, not a value", key)
	}
}

// Set stores values under key, creating the file when missing. Scalars take
// exactly one value; lists take any number, zero clears the list.
func Set(home, key string, values []string) error {
	if !scalarKeys[key] && !listKeys[key] {
		return fmt.Errorf("unknown config key %q (want %s)", key, strings.Join(KnownKeys, ", "))
	}
	if scalarKeys[key] && len(values) != 1 {
		return fmt.Errorf("%s takes exactly one value", key)
	}
	if key == "store" && values[0] != "chezmoi" && values[0] != "git" {
		return fmt.Errorf("store must be chezmoi or git, got %q", values[0])
	}
	path := Path(home)
	doc, err := readDoc(path)
	if err != nil {
		return err
	}
	if doc == nil {
		doc = &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	node := doc.Content[0]
	parts := strings.Split(key, ".")
	for _, part := range parts[:len(parts)-1] {
		node = childForce(node, part, yaml.MappingNode)
	}
	last := parts[len(parts)-1]
	if listKeys[key] {
		seq := &yaml.Node{Kind: yaml.SequenceNode}
		for _, v := range values {
			seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v})
		}
		replace(node, last, seq)
	} else {
		replace(node, last, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: values[0]})
	}
	return save(path, doc)
}

// Ensure creates the config file and its parent dirs when missing.
func Ensure(home string) error {
	path := Path(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	return f.Close()
}

// readDoc parses the file as a node tree, or returns nil when it does not
// exist or is empty.
func readDoc(path string) (*yaml.Node, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return nil, nil
	}
	doc := &yaml.Node{}
	if err := yaml.Unmarshal(b, doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(doc.Content) == 0 {
		return nil, nil
	}
	if doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: top level is not a mapping", path)
	}
	return doc, nil
}

// replace swaps the value under key, creating it when absent. A scalar is
// overwritten in place so its inline comment survives.
func replace(m *yaml.Node, key string, val *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value != key {
			continue
		}
		old := m.Content[i+1]
		if val.Kind == yaml.ScalarNode && old.Kind == yaml.ScalarNode {
			old.Tag = val.Tag
			old.Value = val.Value
			old.Style = val.Style
			return
		}
		m.Content[i+1] = val
		return
	}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, val)
}

func childScalar(m *yaml.Node, key string) string {
	n := child(m, key, yaml.ScalarNode)
	if n == nil || n.Tag == "!!null" {
		return ""
	}
	return n.Value
}

// child returns the named child when it already has the wanted kind;
// a zero kind accepts any kind.
func child(m *yaml.Node, key string, kind yaml.Kind) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			if kind != 0 && m.Content[i+1].Kind != kind {
				return nil
			}
			return m.Content[i+1]
		}
	}
	return nil
}

func childForce(m *yaml.Node, key string, kind yaml.Kind) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			if m.Content[i+1].Kind != kind {
				m.Content[i+1] = &yaml.Node{Kind: kind}
			}
			return m.Content[i+1]
		}
	}
	c := &yaml.Node{Kind: kind}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, c)
	return c
}

func childList(m *yaml.Node, key string) []string {
	seq := child(m, key, yaml.SequenceNode)
	if seq == nil {
		return nil
	}
	var out []string
	for _, c := range seq.Content {
		if c.Tag != "!!null" {
			out = append(out, c.Value)
		}
	}
	return out
}

// expand turns a leading ~ into home; "~user" and inner tildes stay untouched.
func expand(p, home string) string {
	switch {
	case p == "~":
		return home
	case strings.HasPrefix(p, "~/"):
		return filepath.Join(home, p[2:])
	}
	return p
}

func expandAll(ps []string, home string) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = expand(p, home)
	}
	return out
}

func save(path string, doc *yaml.Node) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	return os.WriteFile(path, buf.Bytes(), mode)
}
