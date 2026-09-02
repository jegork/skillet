# Changelog

## [0.4.0](https://github.com/jegork/skillet/compare/v0.3.0...v0.4.0) (2026-09-02)


### Features

* discover project-level skills from configured roots ([#18](https://github.com/jegork/skillet/issues/18)) ([c89e605](https://github.com/jegork/skillet/commit/c89e605cd029db2f1108d39dbb500c3738493db8)), closes [#12](https://github.com/jegork/skillet/issues/12)
* move skills between global and project scope ([#19](https://github.com/jegork/skillet/issues/19)) ([bc62378](https://github.com/jegork/skillet/commit/bc623786bb514808ee34996a2a5586529b289d21))
* search and install skills from registries ([#16](https://github.com/jegork/skillet/issues/16)) ([06152d8](https://github.com/jegork/skillet/commit/06152d87bd14de3fb234dc90929d2ded3a774da1)), closes [#3](https://github.com/jegork/skillet/issues/3)
* show which vendored skills have upstream updates ([#20](https://github.com/jegork/skillet/issues/20)) ([c7165d5](https://github.com/jegork/skillet/commit/c7165d51afd7674e1d27915bc464a91acd5ff07e)), closes [#13](https://github.com/jegork/skillet/issues/13)


### Bug Fixes

* **config:** honour XDG_CONFIG_HOME only for the real user home ([b1747b3](https://github.com/jegork/skillet/commit/b1747b3a2ceea5400c070ff9be1546524f6f5baf))

## [0.3.0](https://github.com/jegork/skillet/compare/v0.2.0...v0.3.0) (2026-09-02)


### Features

* **config:** config file with get/set/edit ([#15](https://github.com/jegork/skillet/issues/15)) ([662149d](https://github.com/jegork/skillet/commit/662149dddb5b06c2c16428d61f6cff9da5160ffd)), closes [#11](https://github.com/jegork/skillet/issues/11)
* **doctor:** flag own skills with vendor markers missing from the lock ([ce6c0b2](https://github.com/jegork/skillet/commit/ce6c0b27945832aefb997f6af9c389a70c2630f3))
* refine an own skill in claude/omp/codex with a prefilled prompt ([#8](https://github.com/jegork/skillet/issues/8)) ([55babed](https://github.com/jegork/skillet/commit/55babed762f5ff7e39a7128d109d3f7957a78d63))
* **store:** add plain git store backend ([3c9883e](https://github.com/jegork/skillet/commit/3c9883e01092fee4fc0340f32004e6d8d5836d93))
* **store:** add plain git store backend ([b48ac3e](https://github.com/jegork/skillet/commit/b48ac3ec2fe57293636bc32d4200b1a726d60558))
* **ui:** tree view grouping skills by vendor ([72abeca](https://github.com/jegork/skillet/commit/72abecafe3468e2850a7e184e7e41804c538a047))
* **ui:** tree view grouping skills by vendor ([47c44ae](https://github.com/jegork/skillet/commit/47c44aef20e7080827e401c0ab3491b9741c59e4))


### Bug Fixes

* **ui:** restore q/ctrl+c quit and the filter row lost in the tree view change ([af15ce3](https://github.com/jegork/skillet/commit/af15ce3ee4ec044520e8969de8b10b725169c066))

## [0.2.0](https://github.com/jegork/skillet/compare/v0.1.0...v0.2.0) (2026-09-01)


### Features

* regenerate the README index from the scan, keeping hand-made sections ([5f78547](https://github.com/jegork/skillet/commit/5f785474bb31eaf2127aeb2bb0bc2574da7d9d3e))
* rename own skills with frontmatter, cross-reference, stub and README fix-ups ([95294a4](https://github.com/jegork/skillet/commit/95294a424a31d53b5f6d22462116cc49e745fdf6))
* report vendored folders that drifted from the lock file's tree hash ([de0c96b](https://github.com/jegork/skillet/commit/de0c96b261072602a39bcdad363adb1b8c3296a1))
* toggle skill visibility per consumer with c/x/o, omp config joins the store ([9c6d62e](https://github.com/jegork/skillet/commit/9c6d62e85bcff017156c2c1c2b4b26daac04c981))

## 0.1.0 (2026-09-01)


### Features

* skills manager tui with chezmoi-backed store, doctor and release pipeline ([230c799](https://github.com/jegork/skillet/commit/230c799539cff5055a0e57a294886a0b7731b6b8))
