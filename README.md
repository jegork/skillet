# skillet

Terminal UI for the agent skills in `~/.agents/skills` and inside your
projects: what you have, which tool sees it, what is yours versus vendored,
what drifted, and one key to sync it all into your dotfiles.

Skills are edited in place. The store is captured from `$HOME`, never edited
directly. Two backends: `chezmoi` (default) or `git`.

## Install

```sh
brew install jegork/tap/skillet   # macOS and Linux, follows releases
task install                      # from source, builds to ~/.local/bin/skillet
```

Releases are cut by Release Please: merging its release PR tags the version,
writes the changelog, and GoReleaser attaches darwin/linux binaries.

## Use

skillet               # the TUI
skillet doctor        # findings as text, exit 1 on errors
skillet status        # store drift: uncaptured, uncommitted, ahead
skillet readme        # regenerate the README index
skillet install owner/repo [--skill NAME]
                      # install from skills.sh via pnpx skills
skillet explore [owner/repo]
                      # list what your vendored repos ship
skillet outdated      # vendored skills with upstream changes
skillet store init    # create the git store repo (--git-dir, --remote)
skillet config        # path, get/set keys, edit in $EDITOR
```

`--store git` selects the plain git backend: a repo whose worktree is `$HOME`
with its git dir at `~/.agents/.skillet-store.git`, tracking only the store
roots. Create it once with `skillet store init [--git-dir DIR] [--remote URL]`.
`--store` and the git store dir/remote come from the config when the flag is
absent: flag, then config, then built-in default. `skillet store init` writes
the git dir and remote it used back into the config.

## Config

`~/.config/skillet/config.yml` (respects `$XDG_CONFIG_HOME`), edited with
`skillet config edit` or the `E` key in the TUI. Comments survive
`skillet config set`. A missing file keeps the built-in defaults.

```yaml
store: chezmoi            # or git
git_store:
  dir: ~/.agents/.skillet-store.git
  remote: git@github.com:you/skills-store.git
projects:
  roots: [~/Documents/projects, ~/orca/workspaces/*]   # dirs whose children are probed
  paths: []                                             # explicit extra projects
```

Keys: `store`, `git_store.dir`, `git_store.remote`, `projects.roots`,
`projects.paths`. Leading `~` expands to the managed home.

```sh
skillet config path                          # where the file lives
skillet config get [key]                     # one key or the whole file
skillet config set projects.roots ~/a ~/b    # repeatable values for lists
skillet config edit
```

| key | action |
|---|---|
| `j` `k` | move |
| `/` | filter by name, `esc` clears |
| `t` | toggle between the flat list and the tree grouped by origin |
| `←` `→` | collapse / expand a group (also `enter` / `space` on a group row) |
| `e` `enter` | open `SKILL.md` in `$VISUAL` / `$EDITOR` (own skills only) |
| `E` | open the config file in `$VISUAL` / `$EDITOR` |
| `c` `x` `o` | toggle visibility for claude / codex / omp |
| `u` | check upstream: fetch the repos in the lock and mark outdated skills |
| `U` | update the picked vendored skill with `pnpx skills update` (outdated only) |
| `n` | rename (own only): dir, frontmatter, cross-references, stubs, README row |
| `p` | refine (own only): pick claude / omp / codex and launch it on the skill with a prefilled message |
| `m` | move between scopes: global or one of the discovered projects |
| `s` | capture into the store, review the diff, commit, push |
| `d` | doctor report |
| `R` | regenerate the README index, keeping its hand-made sections |
| `i` | search skills.sh, `enter` installs the picked skill via `pnpx skills` |
| `a` | explore the skills your vendored repos ship, `enter` installs |
| `r` | rescan |
| `tab` | scroll the preview |
| `?` | help |
| `q` | quit |

In the sync review: `enter` commits, `ctrl+p` toggles push, `esc` cancels and
leaves the capture staged.

`t` switches the list to a tree: your own skills first, then one collapsible
group per vendored `owner/repo`, then one per project, groups and children
sorted by name. Filtering keeps working and hides groups without matches.

Project skills come from the `projects.roots` and `projects.paths` config:
skillet probes each project's `.agents/skills`, `.claude/skills`,
`.codex/skills` and `skills-lock.json` without crawling. In the mirror layout
(`.agents/skills` canonical with `.claude/skills` stubs) toggling works as at
home; a bare `.claude/skills` dir with real skill folders is shown but its
visibility cannot be toggled. Project skills appear in the flat list with an
`@project` origin marker. `m` moves a skill between global and a project in
both directions: the dir is relocated, stubs are fixed per consumer, a
vendored skill takes its lock entry along (tree hash at home, computedHash in
the project), and the README row follows. Moves that would clash with an
existing name or shadow a global skill are refused. Doctor flags project
skills shadowing a global skill of the same name, broken project stubs,
project lock entries without a folder, and a skills dir whose content hash
differs from the project lock's `computedHash`. Sync stays global-only for
now.

Columns: origin (`own` or `vend owner/repo` from `~/.agents/.skill-lock.json`,
project skills suffixed ` @project`), `upd` marker when the upstream repo has
newer changes than the lock (press `U` to update), consumer badges (`C` claude,
`X` codex, `O` omp), doctor finding count, last modified.

Doctor checks: broken consumer stubs, cross-references to unknown skills,
stale README rows, lock entries without a folder, missing SKILL.md or
description, vendored folders whose git tree hash differs from the lock
(info only, since `pnpx skills` rewrites some frontmatter on install), skills
whose upstream repo moved since the lock was written (info), and —
for project skills — shadowed global names, broken project stubs, project
lock orphans and project skills dir drift against `computedHash`.

The upstream check asks GitHub for one recursive tree per repo (token from
`gh auth token` when available, anonymous otherwise) and caches the folder
hashes in `~/.cache/skillet/upstream.json` for an hour. Offline or rate
limited: skills read as "unknown" and the last cache stays.

The same cache drives the explore view (`a` in the TUI, `skillet explore`
for scripts): one group per vendored `owner/repo` — home lock plus every
project lock — listing each skill folder the repo tree contains, marked
installed when a locked skill matches, otherwise available. `enter` on an
available skill installs it through the same path as `i`.

## Layout

```
internal/skill      scan skills + lock files
internal/move        move a skill between global and a project scope, fix stubs/locks/README
internal/project    project discovery from projects.roots/paths
internal/consumer   which tool sees which skill (symlink dirs, omp ignore globs)
internal/doctor     dangling stubs, unknown cross-references, stale README, lock orphans, drift
internal/project    project discovery: roots, paths, fixed probes
internal/readme     README index parse and regeneration
internal/rename     rename an own skill and fix everything that pointed at it
internal/config     read/write ~/.config/skillet/config.yml, comments intact
internal/store      Store interface: Status / Capture / Diff / Commit / Push
internal/store/chezmoi
internal/registry   search skills.sh and install through pnpx skills
internal/explore    vendor listing from the upstream cache
internal/store/gitrepo
internal/ui         bubbletea front end
internal/upstream   upstream update checks: GitHub trees, ~/.cache/skillet/upstream.json
internal/registry   search skills.sh and install through pnpx skills
```

The chezmoi store only ever adds children of a tracked root, never the root
directory itself: on chezmoi 2.72 re-adding a directory drops its `exact_`
attribute.

The git store's git dir may live inside `$HOME` (the default is
`~/.agents/.skillet-store.git`): it sits outside the tracked roots, and every
git operation is pathspec-scoped to those roots, so nothing else in `$HOME` is
ever staged, diffed, committed or pushed.

## Develop
```sh
task test             # chezmoi integration tests run when chezmoi is on PATH
task lint
task --list
```
