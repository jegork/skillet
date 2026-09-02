// Package upstream compares the skill lock file with the GitHub repos the
// vendored skills came from: which skills have newer versions upstream. The
// result is cached on disk for an hour, so the TUI can check in the
// background without hitting the API on every start.
package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jegork/skillet/internal/skill"
)

// TTL is how long a cached repo fetch counts as fresh.
const TTL = time.Hour

const cacheVersion = 1

// State is the outcome of comparing one skill's lock hash with upstream.
type State int

const (
	Unknown  State = iota // no data: never fetched, offline or rate limited
	Current               // upstream folder matches the lock
	Outdated              // upstream folder differs from the lock
)

type Info struct {
	State    State
	Upstream string // upstream folder tree sha, empty when Unknown
	Lock     string // skillFolderHash from the lock file
}

// Entry is one node of a GitHub git tree.
type Entry struct {
	Path string `json:"path"`
	Type string `json:"type"` // "tree" or "blob"
	SHA  string `json:"sha"`
}

// Fetcher reads the recursive git tree of a repo's default branch, one
// request per repo. The root tree comes back as an Entry with path ".".
type Fetcher interface {
	Tree(ctx context.Context, owner, repo string) ([]Entry, error)
}

// treeResp is the GitHub API response of
// GET repos/{owner}/{repo}/git/trees/{ref}?recursive=1. Truncated means the
// repo tree was too large to return in full.
type treeResp struct {
	SHA       string  `json:"sha"`
	Truncated bool    `json:"truncated"`
	Tree      []Entry `json:"tree"`
}

// ParseTree reads a captured trees API response. Truncated responses are an
// error: a folder missing from a half-returned tree would look like an
// upstream deletion and report skills as outdated forever.
func ParseTree(b []byte) ([]Entry, error) {
	var r treeResp
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("github tree: %w", err)
	}
	if r.Truncated {
		return nil, errors.New("github tree truncated")
	}
	entries := append([]Entry{{Path: ".", Type: "tree", SHA: r.SHA}}, r.Tree...)
	return entries, nil
}

// GitHub fetches trees from api.github.com, authenticating with the token of
// a logged in gh CLI when there is one and falling back to anonymous
// requests, which GitHub rate limits to 60 per hour.
type GitHub struct {
	HTTP  *http.Client
	token string
	tried bool
}

func (g *GitHub) Tree(ctx context.Context, owner, repo string) ([]Entry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees/HEAD?recursive=1", owner, repo), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if !g.tried {
		g.token, g.tried = ghToken(), true
	}
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}
	client := g.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api: %s", resp.Status)
	}
	return ParseTree(b)
}

// ghToken returns the token of a logged in gh CLI, or "" when gh is absent
// or not authenticated.
func ghToken() string {
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Repo is one cached fetch: when it happened and the tree sha of every
// folder in the repo.
type Repo struct {
	FetchedAt time.Time         `json:"fetchedAt"`
	Trees     map[string]string `json:"trees"` // folder path -> tree sha
}

// Cache is the on-disk state of the upstream check.
type Cache struct {
	Version int             `json:"version"`
	Repos   map[string]Repo `json:"repos"` // owner/repo -> fetch result
}

// Path is the cache file for home: ~/.cache/skillet/upstream.json.
// XDG_CACHE_HOME is honoured only for the real user home, like config.Path:
// an explicit --home (or a test's temp home) must never read the user's own
// cache.
func Path(home string) string {
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		if real, err := os.UserHomeDir(); err == nil && filepath.Clean(real) == filepath.Clean(home) {
			return filepath.Join(dir, "skillet", "upstream.json")
		}
	}
	return filepath.Join(home, ".cache", "skillet", "upstream.json")
}

// ReadCache loads the cache. A missing, corrupt or stale-versioned file is an
// empty cache: the check degrades to "unknown", never to an error.
func ReadCache(p string) Cache {
	var c Cache
	b, err := os.ReadFile(p)
	if err != nil || json.Unmarshal(b, &c) != nil || c.Version != cacheVersion {
		return Cache{}
	}
	return c
}

// WriteCache saves the cache, creating the directory when needed.
func WriteCache(p string, c Cache) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

// Evaluate compares the lock with the cached upstream trees. Skills without
// cached data get Unknown; the doctor's drift check treats the same way any
// lock hash that is not a git sha.
func Evaluate(lock skill.Lock, c Cache) map[string]Info {
	out := make(map[string]Info, len(lock.Skills))
	for name, e := range lock.Skills {
		info := Info{Lock: e.SkillFolderHash}
		if rc, ok := c.Repos[e.Source]; ok && len(e.SkillFolderHash) == 40 {
			// skillPath points at the SKILL.md file; the lock hashes its folder
			if up, ok := rc.Trees[path.Dir(e.SkillPath)]; ok {
				if up == e.SkillFolderHash {
					info.State = Current
				} else {
					info.State, info.Upstream = Outdated, up
				}
			}
		}
		out[name] = info
	}
	return out
}

// Refresh fetches every lock repo whose cache entry is stale (all of them
// when force) and writes the cache. A repo whose fetch fails keeps its
// previous entry, so being offline or rate limited shows the last known
// state or "unknown" instead of an error. Only cache write failures return
// an error.
func Refresh(ctx context.Context, cachePath string, lock skill.Lock, f Fetcher, force bool) error {
	c := ReadCache(cachePath)
	if c.Repos == nil {
		c.Repos = map[string]Repo{}
	}
	now := time.Now()
	var sources []string
	for _, e := range lock.Skills {
		if strings.Contains(e.Source, "/") && !slices.Contains(sources, e.Source) {
			sources = append(sources, e.Source)
		}
	}
	sort.Strings(sources)
	for _, source := range sources {
		if rc, ok := c.Repos[source]; ok && !force && now.Sub(rc.FetchedAt) < TTL {
			continue
		}
		owner, repo, _ := strings.Cut(source, "/")
		entries, err := f.Tree(ctx, owner, repo)
		if err != nil {
			continue
		}
		trees := make(map[string]string, len(entries))
		for _, e := range entries {
			if e.Type == "tree" {
				trees[e.Path] = e.SHA
			}
		}
		c.Repos[source] = Repo{FetchedAt: now, Trees: trees}
	}
	c.Version = cacheVersion
	return WriteCache(cachePath, c)
}
