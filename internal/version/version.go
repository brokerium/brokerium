// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Package version holds build information injected at link time.
package version

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String returns a human-readable build description.
func String() string {
	return Version + " (" + Commit + ", " + Date + ")"
}
