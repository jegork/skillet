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
```

| key | action |
|---|---|
| `j` `k` | move |
| `/` | filter by name, `esc` clears |
| `e` `enter` | open `SKILL.md` in `$VISUAL` / `$EDITOR` (own skills only) |
| `s` | capture into the store, review the diff, commit, push |
| `d` | doctor report |
| `r` | rescan |
| `tab` | scroll the preview |
| `?` | help |
| `q` | quit |

In the sync review: `enter` commits, `ctrl+p` toggles push, `esc` cancels and
leaves the capture staged.

Columns: origin (`own` or `vend owner/repo` from `~/.agents/.skill-lock.json`),
consumer badges (`C` claude, `X` codex, `O` omp), doctor finding count, last
modified.

## Layout

```
internal/skill      scan skills + lock file
internal/consumer   which tool sees which skill (symlink dirs, omp ignore globs)
internal/doctor     dangling stubs, unknown cross-references, stale README, lock orphans
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
