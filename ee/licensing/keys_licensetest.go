// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise && licensetest

package licensing

// The private seed of the test key is public (testdata/test-signing.key), so only test builds
// may trust it.
func init() {
	publicKeys[testKeyID] = "kxsbIoY1qD2q173KsVKdiY6snWrgBozZdyNNWxSeNa0="
}
