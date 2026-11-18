// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Package agentcfg holds the rules for the configuration the server sends to agents. Agents
// write it to server.properties and into the environment of the Kafka service, so the server
// checks it before sending and the agent again before writing.
package agentcfg

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"unicode"
)

var (
	propertyRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	heapRe     = regexp.MustCompile(`^-Xm[sx][1-9][0-9]*[kKmMgG]?$`)
	labelRe    = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
	numericRe  = regexp.MustCompile(`^[0-9]+$`)
	rackRe     = regexp.MustCompile(`^[A-Za-z0-9._-]{0,64}$`)
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

// ValidateHost accepts the hosts that listeners advertise: a DNS name made of RFC 1123 labels
// separated by dots, or an IPv4 or IPv6 address. Names of numbers only, such as 10.0.0.256, are
// mistyped addresses.
func ValidateHost(host string) error {
	if net.ParseIP(host) != nil {
		return nil
	}
	if host == "" {
		return errors.New("the host is empty")
	}
	labels := strings.Split(host, ".")
	numeric := true
	for _, l := range labels {
		if !labelRe.MatchString(l) {
			return fmt.Errorf("host %q is neither a DNS name nor an IP address", host)
		}
		numeric = numeric && numericRe.MatchString(l)
	}
	if numeric || len(host) > 253 {
		return fmt.Errorf("host %q is neither a DNS name nor an IP address", host)
	}
	return nil
}

// ValidateRack allows broker.rack values of at most 64 letters, digits, '.', '_' and '-'; empty
// means no rack.
func ValidateRack(rack string) error {
	if !rackRe.MatchString(rack) {
		return fmt.Errorf("rack %q: use at most 64 letters, digits, '.', '_' and '-'", rack)
	}
	return nil
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
