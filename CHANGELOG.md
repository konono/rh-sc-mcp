# Changelog

## [0.5.0](https://github.com/konono/rh-sc-mcp/compare/v0.4.0...v0.5.0) (2026-10-26)


### Features

* publish multi-platform container images to GHCR ([#10](https://github.com/konono/rh-sc-mcp/issues/10)) ([ad2dfb2](https://github.com/konono/rh-sc-mcp/commit/ad2dfb29f65a7d97c1e250bbc82f379c04093763))

## [0.4.0](https://github.com/konono/rh-sc-mcp/compare/v0.3.1...v0.4.0) (2026-10-26)


### Features

* automate OpenShift deployment and scoped uninstall ([3023ecf](https://github.com/konono/rh-sc-mcp/commit/3023ecf6401e7ad041803688cfafa1f81bbc2992))

## [0.3.1](https://github.com/konono/rh-sc-mcp/compare/v0.3.0...v0.3.1) (2026-10-26)

This release updates the documentation and build exclusions. Server behavior is unchanged apart from the reported version.

### Documentation

* record the observed `apiVersion: v1` read results and Podman container limits ([7dfc2a5](https://github.com/konono/rh-sc-mcp/commit/7dfc2a5a34d8469d281742b9bef764a69c7d63d3)).
* clarify that a passing `--check-auth` does not establish that a known case can be fetched ([4f58490](https://github.com/konono/rh-sc-mcp/commit/4f5849080b7897b4e7ef1c0c5655b30dce0909d9)).
* record idle container shutdown on `SIGTERM`; handling of in-flight requests remains unmeasured ([7d803ef](https://github.com/konono/rh-sc-mcp/commit/7d803efc7d1dc0fa5e32dacbba83a632897b5fe6)).
* record an OpenShift test with two Ready pods under `restricted-v2`; Route certificate trust and cluster shutdown remain unverified ([c829eee](https://github.com/konono/rh-sc-mcp/commit/c829eee)).

### Bug Fixes

* exclude a repository-root `.kube` directory from git and the image build context ([beba77d](https://github.com/konono/rh-sc-mcp/commit/beba77d514fbcb493bfaa6c1b7d84fb284bd0018)).

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
