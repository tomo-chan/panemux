# Changelog

## [0.18.8](https://github.com/tomo-chan/panemux/compare/v0.18.7...v0.18.8) (2026-06-20)


### Bug Fixes

* preserve maximize state across workspace switches ([#138](https://github.com/tomo-chan/panemux/issues/138)) ([004aaf5](https://github.com/tomo-chan/panemux/commit/004aaf5efd15c77cce040e3599d7cb26a54dae14))

## [0.18.7](https://github.com/tomo-chan/panemux/compare/v0.18.6...v0.18.7) (2026-06-17)


### Bug Fixes

* handle large interactive agent session logs ([#130](https://github.com/tomo-chan/panemux/issues/130)) ([5cb4fd6](https://github.com/tomo-chan/panemux/commit/5cb4fd6e91c0c4dd9bf2cf6746c47ef59ec904d6))
* reduce git-info polling and session log reads ([#133](https://github.com/tomo-chan/panemux/issues/133)) ([eacde38](https://github.com/tomo-chan/panemux/commit/eacde38cbde0a282788b2240753a42ad0539c55d))

## [0.18.6](https://github.com/tomo-chan/panemux/compare/v0.18.5...v0.18.6) (2026-06-15)


### Bug Fixes

* add cross-platform tmux selection modifier ([#126](https://github.com/tomo-chan/panemux/issues/126)) ([a3a5858](https://github.com/tomo-chan/panemux/commit/a3a5858938f33df309b8459e51476333cb6fb400))

## [0.18.5](https://github.com/tomo-chan/panemux/compare/v0.18.4...v0.18.5) (2026-06-14)


### Bug Fixes

* prefer claude transcript cwd for workdir detection ([#123](https://github.com/tomo-chan/panemux/issues/123)) ([e4b54ca](https://github.com/tomo-chan/panemux/commit/e4b54ca5a4085735c9f4d82142fbed1acf6a240f))

## [0.18.4](https://github.com/tomo-chan/panemux/compare/v0.18.3...v0.18.4) (2026-06-14)


### Bug Fixes

* improve git context error diagnostics ([#121](https://github.com/tomo-chan/panemux/issues/121)) ([96875f6](https://github.com/tomo-chan/panemux/commit/96875f613ebeeef3cfa7b375c17a4004d4e1df5a))

## [0.18.3](https://github.com/tomo-chan/panemux/compare/v0.18.2...v0.18.3) (2026-06-10)


### Bug Fixes

* normalize claude transcript project dir names ([#118](https://github.com/tomo-chan/panemux/issues/118)) ([d834d42](https://github.com/tomo-chan/panemux/commit/d834d421ea02f484f3c1fa757e800bf1bbd80df3))

## [0.18.2](https://github.com/tomo-chan/panemux/compare/v0.18.1...v0.18.2) (2026-06-10)


### Bug Fixes

* resolve remote Claude worktree headers ([#116](https://github.com/tomo-chan/panemux/issues/116)) ([04fa53e](https://github.com/tomo-chan/panemux/commit/04fa53ef8aa552de90009aa6a76d6b28b4ec6a85))

## [0.18.1](https://github.com/tomo-chan/panemux/compare/v0.18.0...v0.18.1) (2026-06-07)


### Bug Fixes

* keep agent worktree git context after exit ([#110](https://github.com/tomo-chan/panemux/issues/110)) ([327a393](https://github.com/tomo-chan/panemux/commit/327a3934228be4cd7ba45eed528d1e1e545c11b1))
* reconnect disconnected SSH sessions ([#113](https://github.com/tomo-chan/panemux/issues/113)) ([fd7313b](https://github.com/tomo-chan/panemux/commit/fd7313be8c655f071f7167fa47731ebb5e3fee63))

## [0.18.0](https://github.com/tomo-chan/panemux/compare/v0.17.4...v0.18.0) (2026-05-30)


### Features

* link pane header repo name to repository page ([#108](https://github.com/tomo-chan/panemux/issues/108)) ([d60f129](https://github.com/tomo-chan/panemux/commit/d60f129e849b69721cb0d0d2e12c208712e098da))

## [0.17.4](https://github.com/tomo-chan/panemux/compare/v0.17.3...v0.17.4) (2026-05-20)


### Bug Fixes

* detect ssh tmux git-info worktree correctly ([#106](https://github.com/tomo-chan/panemux/issues/106)) ([3621b00](https://github.com/tomo-chan/panemux/commit/3621b00662a0458cb54e3c48d721e30bd4df2eb7))

## [0.17.3](https://github.com/tomo-chan/panemux/compare/v0.17.2...v0.17.3) (2026-05-20)


### Bug Fixes

* detect codex root process in tmux worktrees ([#104](https://github.com/tomo-chan/panemux/issues/104)) ([da7c303](https://github.com/tomo-chan/panemux/commit/da7c3037ffc2d3958d2bbaf50e14220c6c4b8d67))

## [0.17.2](https://github.com/tomo-chan/panemux/compare/v0.17.1...v0.17.2) (2026-05-19)


### Bug Fixes

* restore git header for ssh and ssh tmux panes ([#102](https://github.com/tomo-chan/panemux/issues/102)) ([3f76f7e](https://github.com/tomo-chan/panemux/commit/3f76f7ec1c478b5f4bb75dc22180aa6f48a79261))

## [0.17.1](https://github.com/tomo-chan/panemux/compare/v0.17.0...v0.17.1) (2026-05-18)


### Bug Fixes

* restore known_hosts SHA-1 compatibility ([#99](https://github.com/tomo-chan/panemux/issues/99)) ([6b53e6e](https://github.com/tomo-chan/panemux/commit/6b53e6e7e6d6abf433f62fa4bcd120e25080e978))

## [0.17.0](https://github.com/tomo-chan/panemux/compare/v0.16.0...v0.17.0) (2026-05-17)


### Features

* allow pane moves across workspaces ([#97](https://github.com/tomo-chan/panemux/issues/97)) ([fe55609](https://github.com/tomo-chan/panemux/commit/fe55609d4db21b0eb2f04da84efc7e9c47cbb685))

## [0.16.0](https://github.com/tomo-chan/panemux/compare/v0.15.0...v0.16.0) (2026-05-17)


### Features

* show linked pull requests in pane headers ([#93](https://github.com/tomo-chan/panemux/issues/93)) ([a0cda2c](https://github.com/tomo-chan/panemux/commit/a0cda2c703c8705568658ae5f177a99406f0622b))


### Bug Fixes

* open VSCode in active agent worktree ([#94](https://github.com/tomo-chan/panemux/issues/94)) ([890e4a0](https://github.com/tomo-chan/panemux/commit/890e4a0555404e987a03c4fb793665dd67418b91))
* resolve codex worktree git context across session types ([#95](https://github.com/tomo-chan/panemux/issues/95)) ([8b118cd](https://github.com/tomo-chan/panemux/commit/8b118cdcc5d9f647b56257ffdfa7e2a8845d7898))

## [0.15.0](https://github.com/tomo-chan/panemux/compare/v0.14.0...v0.15.0) (2026-05-16)


### Features

* add directory browser for pane working directories ([#91](https://github.com/tomo-chan/panemux/issues/91)) ([399a909](https://github.com/tomo-chan/panemux/commit/399a9092666ee0035316c28743af16700aa6e67b))

## [0.14.0](https://github.com/tomo-chan/panemux/compare/v0.13.2...v0.14.0) (2026-05-16)


### Features

* add persistent pane layout editing ([#89](https://github.com/tomo-chan/panemux/issues/89)) ([201ccd5](https://github.com/tomo-chan/panemux/commit/201ccd59fac1b087aeb64d46ad090dee2d32b906))

## [0.13.2](https://github.com/tomo-chan/panemux/compare/v0.13.1...v0.13.2) (2026-05-14)


### Bug Fixes

* suppress replay-generated terminal input ([#87](https://github.com/tomo-chan/panemux/issues/87)) ([edfec67](https://github.com/tomo-chan/panemux/commit/edfec6788adb3837e0e9086e3397e177c54ccc71))

## [0.13.1](https://github.com/tomo-chan/panemux/compare/v0.13.0...v0.13.1) (2026-05-10)


### Bug Fixes

* reduce duplicate attention notifications ([#85](https://github.com/tomo-chan/panemux/issues/85)) ([c763c63](https://github.com/tomo-chan/panemux/commit/c763c635187059bc6cfd35271ad868df4437c0ff))
* tone down terminal scrollbar chrome ([#84](https://github.com/tomo-chan/panemux/issues/84)) ([68b989d](https://github.com/tomo-chan/panemux/commit/68b989da16e69081afd7f4f633a08ecb6cb8be79))

## [0.13.0](https://github.com/tomo-chan/panemux/compare/v0.12.0...v0.13.0) (2026-05-09)


### Features

* expand approval prompt notifications ([#82](https://github.com/tomo-chan/panemux/issues/82)) ([2139616](https://github.com/tomo-chan/panemux/commit/2139616040636b2d26ce3c7a29f54c66fe537eca))


### Bug Fixes

* harden WebSocket, CORS, security headers, and input validation ([#81](https://github.com/tomo-chan/panemux/issues/81)) ([3e56f0c](https://github.com/tomo-chan/panemux/commit/3e56f0cfec1a6c05aeccc498f797c726d41bec44))

## [0.12.0](https://github.com/tomo-chan/panemux/compare/v0.11.0...v0.12.0) (2026-05-09)


### Features

* add agent attention notifications ([#74](https://github.com/tomo-chan/panemux/issues/74)) ([8b51f8f](https://github.com/tomo-chan/panemux/commit/8b51f8f92562ad60fd95384f80056214ea983a53))
* add workspace rename ([#72](https://github.com/tomo-chan/panemux/issues/72)) ([d9d3e77](https://github.com/tomo-chan/panemux/commit/d9d3e77e3b41e2d4806f1275518c4bef74c5d649))
* allow workspace tab position changes ([#73](https://github.com/tomo-chan/panemux/issues/73)) ([a7826ae](https://github.com/tomo-chan/panemux/commit/a7826ae133c2af7ade751b89cb81ac7b9424a7e3))


### Bug Fixes

* repaint terminal after workspace switches ([#75](https://github.com/tomo-chan/panemux/issues/75)) ([22b76be](https://github.com/tomo-chan/panemux/commit/22b76be96be2ab422a216a1e033ab20405203469))
* replay terminal output after workspace switches ([#76](https://github.com/tomo-chan/panemux/issues/76)) ([ad3a9f8](https://github.com/tomo-chan/panemux/commit/ad3a9f838d2ab7d4bee7b91e7c9a6b8a35a4a9ca))
* restore hidden workspace attention notifications ([#77](https://github.com/tomo-chan/panemux/issues/77)) ([9586746](https://github.com/tomo-chan/panemux/commit/95867464d1fa7379ccd0d0b07a61575e713bc328))

## [0.11.0](https://github.com/tomo-chan/panemux/compare/v0.10.0...v0.11.0) (2026-04-29)


### Features

* add workspace tabs ([#69](https://github.com/tomo-chan/panemux/issues/69)) ([d720a17](https://github.com/tomo-chan/panemux/commit/d720a170d893061e7f377dfb924e1adf1faf5378))

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
