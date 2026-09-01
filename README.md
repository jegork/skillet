# skillet

Terminal UI for the agent skills in `~/.agents/skills`: what you have, which
tool sees it, what is yours versus vendored, what drifted, and one key to sync
it all into your dotfiles.

Skills are edited in place. The store (chezmoi today) is captured from `$HOME`,
never edited directly.

## Install

```sh
make install          # builds to ~/.local/bin/skillet
```

Releases are cut by Release Please: merging its release PR tags the version,
writes the changelog, and GoReleaser attaches darwin/linux binaries.

## Use

```sh
skillet               # the TUI
skillet doctor        # findings as text, exit 1 on errors
skillet status        # store drift: uncaptured, uncommitted, ahead
skillet readme        # regenerate the README index
```

| key | action |
|---|---|
| `j` `k` | move |
| `/` | filter by name, `esc` clears |
| `e` `enter` | open `SKILL.md` in `$VISUAL` / `$EDITOR` (own skills only) |
| `c` `x` `o` | toggle visibility for claude / codex / omp |
| `p` | refine (own only): pick claude / omp / codex and launch it on the skill with a prefilled message |
| `s` | capture into the store, review the diff, commit, push |
| `d` | doctor report |
| `R` | regenerate the README index, keeping its hand-made sections |
| `r` | rescan |
| `tab` | scroll the preview |
| `?` | help |
| `q` | quit |

In the sync review: `enter` commits, `ctrl+p` toggles push, `esc` cancels and
leaves the capture staged.

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
internal/ui         bubbletea front end
```

The chezmoi store only ever adds children of a tracked root, never the root
directory itself: on chezmoi 2.72 re-adding a directory drops its `exact_`
attribute.

## Develop

```sh
make test             # chezmoi integration tests run when chezmoi is on PATH
make lint
```
