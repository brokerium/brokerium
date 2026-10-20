// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Package agentcfg holds the rules for the configuration the server sends to agents. Agents
// write it to server.properties and into the environment of the Kafka service, so the server
// checks it before sending and the agent again before writing.
package agentcfg

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var (
	propertyRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	heapRe     = regexp.MustCompile(`^-Xm[sx][1-9][0-9]*[kKmMgG]?$`)
)

// Validate checks the properties and heap options of a WriteConfig command.
func Validate(props map[string]string, heap string) error {
	for k, v := range props {
		if !propertyRe.MatchString(k) {
			return fmt.Errorf("property name %q: only letters, digits, '.', '_' and '-' are allowed", k)
		}
		if strings.IndexFunc(v, unicode.IsControl) >= 0 {
			return fmt.Errorf("property %s: control characters are not allowed in values", k)
		}
	}
	return ValidateHeap(heap)
}

// ValidateHeap allows only -Xms and -Xmx options separated by single spaces, for example
// "-Xms512m -Xmx512m".
func ValidateHeap(heap string) error {
	if heap == "" {
		return nil
	}
	for _, opt := range strings.Split(heap, " ") {
		if !heapRe.MatchString(opt) {
			return fmt.Errorf("heap option %q: only -Xms<size> and -Xmx<size> are allowed", opt)
		}
	}
	return nil
}
