// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package agentgw

import (
	"fmt"
	"strings"
)

// maxNodeName is the length limit of node names.
const maxNodeName = 63

func nameChar(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-'
}

// ValidNodeName reports whether name is a node name: 1 to 63 letters, digits, '.', '_' and '-'.
func ValidNodeName(name string) bool {
	return name != "" && len(name) <= maxNodeName && strings.IndexFunc(name, func(r rune) bool { return !nameChar(r) }) < 0
}

// NodeNameFromHost turns a host name an agent reports into a node name: other characters become
// '-', and the name is cut to 63 characters. An empty host name gives "node".
func NodeNameFromHost(host string) string {
	name := strings.Map(func(r rune) rune {
		if nameChar(r) {
			return r
		}
		return '-'
	}, host)
	if len(name) > maxNodeName {
		name = name[:maxNodeName]
	}
	if name == "" {
		return "node"
	}
	return name
}

// numberedName is the n-th candidate name for a node named after base: base itself, then
// base-2, base-3 and so on, with base shortened to keep the name within 63 characters.
func numberedName(base string, n int) string {
	if n <= 1 {
		return base
	}
	suffix := fmt.Sprintf("-%d", n)
	if len(base)+len(suffix) > maxNodeName {
		base = base[:maxNodeName-len(suffix)]
	}
	return base + suffix
}
