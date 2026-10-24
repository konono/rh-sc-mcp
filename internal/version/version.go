// Package version holds the single constant the release tooling rewrites.
package version

// Version is reported to MCP clients during initialize and by --version.
//
// It is not the only copy of the version: .release-please-manifest.json
// holds it too, and that file is the one release-please reads. This
// constant is a derived copy, kept in step because
// release-please-config.json lists it under extra-files, so a release
// updates both together and neither should be edited by hand.
//
// The comments around the constant are release-please's markers for
// finding the line to replace. Detaching or removing them would leave
// it without a target here; what happens then — a failed run, or a
// release that updates the manifest and leaves this behind — has not
// been tested, so the safe course is to leave them where they are.

// x-release-please-start-version
const Version = "0.1.0"

// x-release-please-end
