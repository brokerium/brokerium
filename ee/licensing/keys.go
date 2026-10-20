// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package licensing

// publicKeys maps the key ids of signing keys to their Ed25519 public keys, standard base64 of
// 32 bytes. A rotated key keeps its entry until the last key it signed has expired.
var publicKeys = map[string]string{
	"k1": "URbfTp44Q1QZJ7r01gpP1afAC0OlV8o9QN49Pxp+78E=",
}
