# Changelog

## [0.3.1](https://github.com/konono/rh-sc-mcp/compare/v0.3.0...v0.3.1) (2026-10-26)


### Bug Fixes

* keep a root .kube out of git and the build context ([beba77d](https://github.com/konono/rh-sc-mcp/commit/beba77d514fbcb493bfaa6c1b7d84fb284bd0018))


### Miscellaneous Chores

* release 0.3.1 ([5756508](https://github.com/konono/rh-sc-mcp/commit/5756508bc967a3f0991e4818cc7ad03ae743832e))

## [0.3.0](https://github.com/konono/rh-sc-mcp/compare/v0.2.0...v0.3.0) (2026-10-25)


### Features

* create a default configuration on first run ([f870f1b](https://github.com/konono/rh-sc-mcp/commit/f870f1b2331e658c1e1d68266124632e70965b82))
* install with `go install`, and update by re-running it ([6a07d78](https://github.com/konono/rh-sc-mcp/commit/6a07d78f8d687bf6f74de43f5e9973f1008fedf7), [19f991b](https://github.com/konono/rh-sc-mcp/commit/19f991b5f9b848887d4a2cef4a566afcb02a51e1)). `rh-sc-mcp update` refuses to act unless the toolchain would install over the binary that is running.
* register the MCP server with Claude Code from `install.sh`, leaving any existing registration untouched ([646597f](https://github.com/konono/rh-sc-mcp/commit/646597fd91fa40d39a324aecfe0e7de32fe6c58f))

## [0.2.0](https://github.com/konono/rh-sc-mcp/compare/v0.1.0...v0.2.0) (2026-10-24)


### Features

* add --check-auth, and keep local token files out of git ([c39d831](https://github.com/konono/rh-sc-mcp/commit/c39d831f194732c5bbd7e7e600900041d30816e5))
* add the release pipeline and an MIT licence ([e61d3f3](https://github.com/konono/rh-sc-mcp/commit/e61d3f320eb957816c8fad660e7c7042e06d9e8f))
* project KCS search results instead of passing them through ([8963892](https://github.com/konono/rh-sc-mcp/commit/89638920123aff19d85d96529defa786cae344dc))
* Red Hat Support Case MCP server ([44fdc1c](https://github.com/konono/rh-sc-mcp/commit/44fdc1c88d0ae4536d9a4da6fbd98fab12d693c9))
* serve MCP over Streamable HTTP as well as stdio ([b6ef7a9](https://github.com/konono/rh-sc-mcp/commit/b6ef7a974e460054cad09e35564c16650de39d21))


### Bug Fixes

* send search criteria with the auth probe, and refuse empty searches ([9c5fc18](https://github.com/konono/rh-sc-mcp/commit/9c5fc1894937ee9ebc85e49cc3bf85096b8bfad8))
