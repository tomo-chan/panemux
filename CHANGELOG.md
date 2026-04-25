# Changelog

## [0.11.0](https://github.com/tomo-chan/panemux/compare/v0.10.0...v0.11.0) (2026-04-25)


### Features

* add 'Open in VSCode' button to terminal panes ([#36](https://github.com/tomo-chan/panemux/issues/36)) ([2b5e1f1](https://github.com/tomo-chan/panemux/commit/2b5e1f112c963320487f8335232e867a383d4549))
* add automated release pipeline ([#3](https://github.com/tomo-chan/panemux/issues/3)) ([a533d64](https://github.com/tomo-chan/panemux/commit/a533d649970644244c04914477ad5a6effb00468))
* add edit mode toggle to gate config file persistence ([#22](https://github.com/tomo-chan/panemux/issues/22)) ([d414aaf](https://github.com/tomo-chan/panemux/commit/d414aaff6cda4ad5e1bb134570da48f0d619bf40))
* add GitHub Actions CI workflow with test reporting ([#2](https://github.com/tomo-chan/panemux/issues/2)) ([ddbecbe](https://github.com/tomo-chan/panemux/commit/ddbecbe84598a0b22a20f761addf3a40c9162476))
* add golangci-lint v2 to lint pipeline and CI ([#58](https://github.com/tomo-chan/panemux/issues/58)) ([7082159](https://github.com/tomo-chan/panemux/commit/708215972cc8278476c880cad6af4e9d172e3886))
* add pane maximize/restore feature ([#1](https://github.com/tomo-chan/panemux/issues/1)) ([405aad7](https://github.com/tomo-chan/panemux/commit/405aad79c61b28fac2f8c653660462f69f0dd58f))
* add pane settings dialog for UI-based connection configuration ([#25](https://github.com/tomo-chan/panemux/issues/25)) ([45406a8](https://github.com/tomo-chan/panemux/commit/45406a8cef9587492e1df8c9608d55dc782b5041))
* auto-detect default shell per connection in pane settings ([#53](https://github.com/tomo-chan/panemux/issues/53)) ([9daf8d8](https://github.com/tomo-chan/panemux/commit/9daf8d88333b492c550abdbac91fc0686fbb18c0))
* default REPO to tomo-chan/panemux ([#24](https://github.com/tomo-chan/panemux/issues/24)) ([7b92ecb](https://github.com/tomo-chan/panemux/commit/7b92ecb60ad8cb4db775e91b7a9c569d0245c639))
* drag & drop pane reordering in edit mode ([#27](https://github.com/tomo-chan/panemux/issues/27)) ([4b4ddac](https://github.com/tomo-chan/panemux/commit/4b4ddacbab22995fddec3301e21f6ed969bef8a7))
* enlarge pane header buttons for easier clicking ([#14](https://github.com/tomo-chan/panemux/issues/14)) ([29b5bd3](https://github.com/tomo-chan/panemux/commit/29b5bd3d4379a87e47a18798ec39b7544c7d17c0))
* full-window drag in edit mode; lock terminal input during edit ([#31](https://github.com/tomo-chan/panemux/issues/31)) ([87ec740](https://github.com/tomo-chan/panemux/commit/87ec74058cba1895a4aa85dca19c938031f7c0ae))
* inherit source pane settings when splitting a panel ([#54](https://github.com/tomo-chan/panemux/issues/54)) ([a78edf6](https://github.com/tomo-chan/panemux/commit/a78edf66323e4f012bc2be5738849f58c91b8178))
* show git repository info in pane header ([#52](https://github.com/tomo-chan/panemux/issues/52)) ([64bb894](https://github.com/tomo-chan/panemux/commit/64bb8941dc2ce92f6f1df12e2faef78420c56d66))
* show status bar by default ([#46](https://github.com/tomo-chan/panemux/issues/46)) ([5a9bb64](https://github.com/tomo-chan/panemux/commit/5a9bb644d33b86c7de453a32b6b021e6b413af39))
* SSH connection management via ~/.ssh/config (VSCode-style) ([#29](https://github.com/tomo-chan/panemux/issues/29)) ([2056411](https://github.com/tomo-chan/panemux/commit/2056411b6ee5c9262618c4895fd59b80c54c811c))
* support ProxyJump for SSH sessions ([#45](https://github.com/tomo-chan/panemux/issues/45)) ([a3831c6](https://github.com/tomo-chan/panemux/commit/a3831c60529dcb2692f17b16f91f9b7d18135ba2))


### Bug Fixes

* break CodeQL taint chain for go/command-injection alert ([#15](https://github.com/tomo-chan/panemux/issues/15)) ([6b4563d](https://github.com/tomo-chan/panemux/commit/6b4563d4eb11a130b90aa22b2e91e6a9ecf75bca))
* detect interactive shell CWD for SSH sessions ([#49](https://github.com/tomo-chan/panemux/issues/49)) ([449477b](https://github.com/tomo-chan/panemux/commit/449477b5f24b0aafed65b5e6a4e6caa24951d9a3))
* hide split divider when a pane is maximized ([#13](https://github.com/tomo-chan/panemux/issues/13)) ([ff3b94e](https://github.com/tomo-chan/panemux/commit/ff3b94ea52f2b787bcbb2762aa90517f3205020d))
* make check build frontend first so it always tests latest code ([#16](https://github.com/tomo-chan/panemux/issues/16)) ([c6000fc](https://github.com/tomo-chan/panemux/commit/c6000fc5344a407677be6c87bc82f83fc6689a42))
* make install.sh POSIX sh compatible ([#23](https://github.com/tomo-chan/panemux/issues/23)) ([4cafd59](https://github.com/tomo-chan/panemux/commit/4cafd5939717d843fad475b740b636301ece8f58))
* resolve 4 GitHub code scanning security alerts ([#6](https://github.com/tomo-chan/panemux/issues/6)) ([e43be25](https://github.com/tomo-chan/panemux/commit/e43be25d607f361f2796f1ccf9d06eb90a89c8b3))
* resolve test failures and act() warnings ([#37](https://github.com/tomo-chan/panemux/issues/37)) ([ce69acc](https://github.com/tomo-chan/panemux/commit/ce69acc8a16a11ea3e0c2ce7c3ac216789370999))
* restore terminal after maximize by using CSS instead of duplicate TerminalPane ([#12](https://github.com/tomo-chan/panemux/issues/12)) ([2511bb0](https://github.com/tomo-chan/panemux/commit/2511bb00357e50d3763432503d7085cca6ec2064))
* show error message and suppress write spam when tmux session exits ([#20](https://github.com/tomo-chan/panemux/issues/20)) ([5bfff9a](https://github.com/tomo-chan/panemux/commit/5bfff9a2d9303d3d41e7fc1b6744c43276962523))
* tighten config permissions with gosec lint ([#67](https://github.com/tomo-chan/panemux/issues/67)) ([40c54b5](https://github.com/tomo-chan/panemux/commit/40c54b510f77c623bf5baabcfc87b8c9de76a92e))
* trigger release workflow on release published event ([#17](https://github.com/tomo-chan/panemux/issues/17)) ([ceef281](https://github.com/tomo-chan/panemux/commit/ceef2819e5a7a5d85b82f3a4c10f858bc526169c))
* validate shell against /etc/shells to resolve go/command-injection alert ([#8](https://github.com/tomo-chan/panemux/issues/8)) ([ffe967d](https://github.com/tomo-chan/panemux/commit/ffe967d78ee2650b347c047259897d3be64a57e3))

## [0.10.0](https://github.com/tomo-chan/panemux/compare/v0.9.0...v0.10.0) (2026-04-25)


### Features

* add golangci-lint v2 to lint pipeline and CI ([#58](https://github.com/tomo-chan/panemux/issues/58)) ([7082159](https://github.com/tomo-chan/panemux/commit/708215972cc8278476c880cad6af4e9d172e3886))


### Bug Fixes

* tighten config permissions with gosec lint ([#67](https://github.com/tomo-chan/panemux/issues/67)) ([40c54b5](https://github.com/tomo-chan/panemux/commit/40c54b510f77c623bf5baabcfc87b8c9de76a92e))

## [0.9.0](https://github.com/tomo-chan/panemux/compare/v0.8.0...v0.9.0) (2026-04-01)


### Features

* auto-detect default shell per connection in pane settings ([#53](https://github.com/tomo-chan/panemux/issues/53)) ([9daf8d8](https://github.com/tomo-chan/panemux/commit/9daf8d88333b492c550abdbac91fc0686fbb18c0))

## [0.8.0](https://github.com/tomo-chan/panemux/compare/v0.7.1...v0.8.0) (2026-03-31)


### Features

* inherit source pane settings when splitting a panel ([#54](https://github.com/tomo-chan/panemux/issues/54)) ([a78edf6](https://github.com/tomo-chan/panemux/commit/a78edf66323e4f012bc2be5738849f58c91b8178))
* show git repository info in pane header ([#52](https://github.com/tomo-chan/panemux/issues/52)) ([64bb894](https://github.com/tomo-chan/panemux/commit/64bb8941dc2ce92f6f1df12e2faef78420c56d66))

## [0.7.1](https://github.com/tomo-chan/panemux/compare/v0.7.0...v0.7.1) (2026-03-26)


### Bug Fixes

* detect interactive shell CWD for SSH sessions ([#49](https://github.com/tomo-chan/panemux/issues/49)) ([449477b](https://github.com/tomo-chan/panemux/commit/449477b5f24b0aafed65b5e6a4e6caa24951d9a3))

## [0.7.0](https://github.com/tomo-chan/panemux/compare/v0.6.0...v0.7.0) (2026-03-25)


### Features

* show status bar by default ([#46](https://github.com/tomo-chan/panemux/issues/46)) ([5a9bb64](https://github.com/tomo-chan/panemux/commit/5a9bb644d33b86c7de453a32b6b021e6b413af39))
* support ProxyJump for SSH sessions ([#45](https://github.com/tomo-chan/panemux/issues/45)) ([a3831c6](https://github.com/tomo-chan/panemux/commit/a3831c60529dcb2692f17b16f91f9b7d18135ba2))

## [0.6.0](https://github.com/tomo-chan/panemux/compare/v0.5.1...v0.6.0) (2026-03-24)


### Features

* add 'Open in VSCode' button to terminal panes ([#36](https://github.com/tomo-chan/panemux/issues/36)) ([2b5e1f1](https://github.com/tomo-chan/panemux/commit/2b5e1f112c963320487f8335232e867a383d4549))

## [0.5.1](https://github.com/tomo-chan/panemux/compare/v0.5.0...v0.5.1) (2026-03-23)


### Bug Fixes

* resolve test failures and act() warnings ([#37](https://github.com/tomo-chan/panemux/issues/37)) ([ce69acc](https://github.com/tomo-chan/panemux/commit/ce69acc8a16a11ea3e0c2ce7c3ac216789370999))

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
