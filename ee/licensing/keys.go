// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package licensing

// publicKeys maps the key ids of signing keys to their Ed25519 public keys, standard base64 of
// 32 bytes.
var publicKeys = map[string]string{}
