# skillet

Terminal UI for the agent skills in `~/.agents/skills`: what you have, which
tool sees it, what is yours versus vendored, what drifted, and one key to sync
it all into your dotfiles.

Skills are edited in place. The store is captured from `$HOME`, never edited
directly. Two backends: `chezmoi` (default) or `git`.

## Install

```sh
task install          # builds to ~/.local/bin/skillet
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
skillet store init    # create the git store repo (--git-dir, --remote)
```

`--store git` selects the plain git backend: a repo whose worktree is `$HOME`
with its git dir at `~/.agents/.skillet-store.git`, tracking only the store
roots. Create it once with `skillet store init [--git-dir DIR] [--remote URL]`.

| key | action |
|---|---|
| `j` `k` | move |
| `/` | filter by name, `esc` clears |
| `t` | toggle between the flat list and the tree grouped by origin |
| `←` `→` | collapse / expand a group (also `enter` / `space` on a group row) |
| `e` `enter` | open `SKILL.md` in `$VISUAL` / `$EDITOR` (own skills only) |
| `c` `x` `o` | toggle visibility for claude / codex / omp |
| `n` | rename (own only): dir, frontmatter, cross-references, stubs, README row |
| `p` | refine (own only): pick claude / omp / codex and launch it on the skill with a prefilled message |
| `s` | capture into the store, review the diff, commit, push |
| `d` | doctor report |
| `R` | regenerate the README index, keeping its hand-made sections |
| `i` | search skills.sh, `enter` installs the picked skill via `pnpx skills` |
| `r` | rescan |
| `tab` | scroll the preview |
| `?` | help |
| `q` | quit |

In the sync review: `enter` commits, `ctrl+p` toggles push, `esc` cancels and
leaves the capture staged.

`t` switches the list to a tree: your own skills first, then one collapsible
group per vendored `owner/repo`, groups and children sorted by name. Filtering
keeps working and hides groups without matches.

Columns: origin (`own` or `vend owner/repo` from `~/.agents/.skill-lock.json`),
consumer badges (`C` claude, `X` codex, `O` omp), doctor finding count, last
modified.

Doctor checks: broken consumer stubs, cross-references to unknown skills,
stale README rows, lock entries without a folder, missing SKILL.md or
description, and vendored folders whose git tree hash differs from the lock
(info only, since `pnpx skills` rewrites some frontmatter on install).

## Layout

```
internal/skill      scan skills + lock file
internal/consumer   which tool sees which skill (symlink dirs, omp ignore globs)
internal/doctor     dangling stubs, unknown cross-references, stale README, lock orphans, drift
internal/readme     README index parse and regeneration
internal/rename     rename an own skill and fix everything that pointed at it
internal/store      Store interface: Status / Capture / Diff / Commit / Push
internal/store/chezmoi
internal/store/gitrepo
internal/ui         bubbletea front end
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
