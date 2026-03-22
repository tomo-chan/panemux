# Changelog

## [0.5.0](https://github.com/tomo-chan/panemux/compare/v0.4.0...v0.5.0) (2026-03-22)


### Features

* full-window drag in edit mode; lock terminal input during edit ([#31](https://github.com/tomo-chan/panemux/issues/31)) ([87ec740](https://github.com/tomo-chan/panemux/commit/87ec74058cba1895a4aa85dca19c938031f7c0ae))
* SSH connection management via ~/.ssh/config (VSCode-style) ([#29](https://github.com/tomo-chan/panemux/issues/29)) ([2056411](https://github.com/tomo-chan/panemux/commit/2056411b6ee5c9262618c4895fd59b80c54c811c))

## [0.4.0](https://github.com/tomo-chan/panemux/compare/v0.3.0...v0.4.0) (2026-03-22)


### Features

* add pane settings dialog for UI-based connection configuration ([#25](https://github.com/tomo-chan/panemux/issues/25)) ([45406a8](https://github.com/tomo-chan/panemux/commit/45406a8cef9587492e1df8c9608d55dc782b5041))
* drag & drop pane reordering in edit mode ([#27](https://github.com/tomo-chan/panemux/issues/27)) ([4b4ddac](https://github.com/tomo-chan/panemux/commit/4b4ddacbab22995fddec3301e21f6ed969bef8a7))

## [0.3.0](https://github.com/tomo-chan/panemux/compare/v0.2.0...v0.3.0) (2026-03-21)


### Features

* add edit mode toggle to gate config file persistence ([#22](https://github.com/tomo-chan/panemux/issues/22)) ([d414aaf](https://github.com/tomo-chan/panemux/commit/d414aaff6cda4ad5e1bb134570da48f0d619bf40))
* default REPO to tomo-chan/panemux ([#24](https://github.com/tomo-chan/panemux/issues/24)) ([7b92ecb](https://github.com/tomo-chan/panemux/commit/7b92ecb60ad8cb4db775e91b7a9c569d0245c639))


### Bug Fixes

* make install.sh POSIX sh compatible ([#23](https://github.com/tomo-chan/panemux/issues/23)) ([4cafd59](https://github.com/tomo-chan/panemux/commit/4cafd5939717d843fad475b740b636301ece8f58))
* show error message and suppress write spam when tmux session exits ([#20](https://github.com/tomo-chan/panemux/issues/20)) ([5bfff9a](https://github.com/tomo-chan/panemux/commit/5bfff9a2d9303d3d41e7fc1b6744c43276962523))
* trigger release workflow on release published event ([#17](https://github.com/tomo-chan/panemux/issues/17)) ([ceef281](https://github.com/tomo-chan/panemux/commit/ceef2819e5a7a5d85b82f3a4c10f858bc526169c))

## [0.2.0](https://github.com/tomo-chan/panemux/compare/v0.1.0...v0.2.0) (2026-03-21)


### Features

* add automated release pipeline ([#3](https://github.com/tomo-chan/panemux/issues/3)) ([a533d64](https://github.com/tomo-chan/panemux/commit/a533d649970644244c04914477ad5a6effb00468))
* add GitHub Actions CI workflow with test reporting ([#2](https://github.com/tomo-chan/panemux/issues/2)) ([ddbecbe](https://github.com/tomo-chan/panemux/commit/ddbecbe84598a0b22a20f761addf3a40c9162476))
* add pane maximize/restore feature ([#1](https://github.com/tomo-chan/panemux/issues/1)) ([405aad7](https://github.com/tomo-chan/panemux/commit/405aad79c61b28fac2f8c653660462f69f0dd58f))
* enlarge pane header buttons for easier clicking ([#14](https://github.com/tomo-chan/panemux/issues/14)) ([29b5bd3](https://github.com/tomo-chan/panemux/commit/29b5bd3d4379a87e47a18798ec39b7544c7d17c0))


### Bug Fixes

* break CodeQL taint chain for go/command-injection alert ([#15](https://github.com/tomo-chan/panemux/issues/15)) ([6b4563d](https://github.com/tomo-chan/panemux/commit/6b4563d4eb11a130b90aa22b2e91e6a9ecf75bca))
* hide split divider when a pane is maximized ([#13](https://github.com/tomo-chan/panemux/issues/13)) ([ff3b94e](https://github.com/tomo-chan/panemux/commit/ff3b94ea52f2b787bcbb2762aa90517f3205020d))
* make check build frontend first so it always tests latest code ([#16](https://github.com/tomo-chan/panemux/issues/16)) ([c6000fc](https://github.com/tomo-chan/panemux/commit/c6000fc5344a407677be6c87bc82f83fc6689a42))
* resolve 4 GitHub code scanning security alerts ([#6](https://github.com/tomo-chan/panemux/issues/6)) ([e43be25](https://github.com/tomo-chan/panemux/commit/e43be25d607f361f2796f1ccf9d06eb90a89c8b3))
* restore terminal after maximize by using CSS instead of duplicate TerminalPane ([#12](https://github.com/tomo-chan/panemux/issues/12)) ([2511bb0](https://github.com/tomo-chan/panemux/commit/2511bb00357e50d3763432503d7085cca6ec2064))
* validate shell against /etc/shells to resolve go/command-injection alert ([#8](https://github.com/tomo-chan/panemux/issues/8)) ([ffe967d](https://github.com/tomo-chan/panemux/commit/ffe967d78ee2650b347c047259897d3be64a57e3))
