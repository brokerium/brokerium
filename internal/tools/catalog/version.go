// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package main

import (
	"fmt"
	"strconv"
	"strings"
)

// version is a Kafka release such as 4.4.0.
type version struct{ major, minor, patch int }

// parseVersion reads a release of the form major.minor.patch.
func parseVersion(s string) (version, bool) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return version{}, false
	}
	var n [3]int
	for i, p := range parts {
		if p == "" || strings.Trim(p, "0123456789") != "" {
			return version{}, false
		}
		var err error
		if n[i], err = strconv.Atoi(p); err != nil {
			return version{}, false
		}
	}
	return version{n[0], n[1], n[2]}, true
}

func (v version) String() string { return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch) }

func (v version) line() line { return line{v.major, v.minor} }

// hasImage reports whether Apache publishes an apache/kafka image of the release, which it
// does from 3.7.0.
func (v version) hasImage() bool { return v.line().compare(line{3, 7}) >= 0 }

// line is a minor release of Kafka such as 4.4.
type line struct{ major, minor int }

func (l line) String() string { return fmt.Sprintf("%d.%d", l.major, l.minor) }

func (l line) compare(o line) int {
	if l.major != o.major {
		return l.major - o.major
	}
	return l.minor - o.minor
}

// docsPath is the directory of the documentation of the line on kafka.apache.org, such as 44.
func (l line) docsPath() string { return fmt.Sprintf("%d%d", l.major, l.minor) }
