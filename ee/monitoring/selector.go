// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package monitoring

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/brokerium/brokerium/internal/cluster"
)

// DefaultExporterPort is the port of the JMX exporter on the brokers unless a cluster sets
// another one.
const DefaultExporterPort = 9141

// maxSelector bounds the length of a selector.
const maxSelector = 4096

// ErrBadSelector is returned for selectors that are not a list of PromQL label matchers.
var ErrBadSelector = errors.New("invalid selector")

// matcher is a PromQL label matcher such as job="kafka".
type matcher struct {
	name, op, value string
}

func (m matcher) String() string {
	return m.name + m.op + strconv.Quote(m.value)
}

var labelNameRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*`)

// normalizeSelector checks a selector of label matchers, such as job="kafka", env=~"prod|stage",
// optionally in braces, and returns it in a canonical form. Every matcher of a panel query is
// added to it, so it must not name a metric.
func normalizeSelector(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) > maxSelector {
		return "", fmt.Errorf("%w: longer than %d characters", ErrBadSelector, maxSelector)
	}
	if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	var out []string
	for s != "" {
		name := labelNameRe.FindString(s)
		if name == "" {
			return "", fmt.Errorf("%w: expected a label name at %q", ErrBadSelector, cut(s))
		}
		if name == "__name__" {
			return "", fmt.Errorf("%w: the panels choose the metrics, so the selector cannot match __name__", ErrBadSelector)
		}
		s = strings.TrimSpace(s[len(name):])
		var op string
		for _, candidate := range []string{"=~", "!~", "!=", "="} {
			if strings.HasPrefix(s, candidate) {
				op = candidate
				break
			}
		}
		if op == "" {
			return "", fmt.Errorf("%w: expected =, !=, =~ or !~ after %s", ErrBadSelector, name)
		}
		s = strings.TrimSpace(s[len(op):])
		value, rest, err := readString(s)
		if err != nil {
			return "", fmt.Errorf("%w: the value of %s: %v", ErrBadSelector, name, err)
		}
		if op == "=~" || op == "!~" {
			if _, err := regexp.Compile("^(?:" + value + ")$"); err != nil {
				return "", fmt.Errorf("%w: the regular expression of %s: %v", ErrBadSelector, name, err)
			}
		}
		out = append(out, matcher{name: name, op: op, value: value}.String())
		s = strings.TrimSpace(rest)
		if s == "" {
			break
		}
		if s[0] != ',' {
			return "", fmt.Errorf("%w: expected a comma at %q", ErrBadSelector, cut(s))
		}
		s = strings.TrimSpace(s[1:])
	}
	return strings.Join(out, ","), nil
}

// readString reads a PromQL string literal in double quotes, single quotes or backticks.
func readString(s string) (value, rest string, err error) {
	if s == "" {
		return "", "", errors.New("missing")
	}
	quote := s[0]
	switch quote {
	case '`':
		end := strings.IndexByte(s[1:], '`')
		if end < 0 {
			return "", "", errors.New("unterminated string")
		}
		return s[1 : end+1], s[end+2:], nil
	case '"', '\'':
		// The literal is rewritten in double quotes for strconv.Unquote.
		var b strings.Builder
		b.WriteByte('"')
		for i := 1; i < len(s); i++ {
			switch c := s[i]; {
			case c == '\\' && i+1 < len(s):
				if s[i+1] == '\'' {
					b.WriteByte('\'')
				} else {
					b.WriteString(s[i : i+2])
				}
				i++
			case c == quote:
				b.WriteByte('"')
				value, err := strconv.Unquote(b.String())
				if err != nil {
					return "", "", errors.New("invalid escape sequence")
				}
				return value, s[i+1:], nil
			case c == '"':
				b.WriteString(`\"`)
			default:
				b.WriteByte(c)
			}
		}
		return "", "", errors.New("unterminated string")
	}
	return "", "", errors.New("expected a quoted string")
}

func cut(s string) string {
	if len(s) > 20 {
		return s[:20] + "…"
	}
	return s
}

// nodes are the hosts of the nodes of a cluster, as the default selector matches them, and the
// names under which the panels show them.
type nodes struct {
	hosts []string
	names map[string]string
}

// clusterNodes returns the hosts of the brokers and of the controllers whose endpoints the
// quorum reports, or the hosts of the bootstrap servers before the first snapshot.
func clusterNodes(v *cluster.View) nodes {
	ids := map[string]map[int32]string{}
	add := func(host, role string, id int32) {
		if host == "" {
			return
		}
		if ids[host] == nil {
			ids[host] = map[int32]string{}
		}
		if prev, ok := ids[host][id]; ok && prev != role {
			role = "node"
		}
		ids[host][id] = role
	}
	if v.Snapshot != nil {
		for _, b := range v.Snapshot.Brokers {
			add(b.Host, "broker", b.ID)
		}
		if q := v.Snapshot.Quorum; q != nil {
			for _, voter := range q.Voters {
				for _, e := range voter.Endpoints {
					add(e.Host, "controller", voter.NodeID)
				}
			}
		}
	}
	if len(ids) == 0 && v.Cluster != nil {
		for _, server := range v.Cluster.BootstrapServers {
			if host, _, err := net.SplitHostPort(server); err == nil {
				add(host, "", -1)
			}
		}
	}
	n := nodes{names: map[string]string{}}
	for host, byID := range ids {
		n.hosts = append(n.hosts, host)
		if len(byID) != 1 {
			continue
		}
		for id, role := range byID {
			if role != "" {
				n.names[host] = role + " " + strconv.Itoa(int(id))
			}
		}
	}
	slices.Sort(n.hosts)
	return n
}

// defaultSelector matches the exporters on the hosts by their instance label, host:port.
func defaultSelector(hosts []string, port int) string {
	if len(hosts) == 0 {
		return ""
	}
	parts := make([]string, len(hosts))
	for i, h := range hosts {
		parts[i] = regexp.QuoteMeta(net.JoinHostPort(h, strconv.Itoa(port)))
	}
	return matcher{name: "instance", op: "=~", value: strings.Join(parts, "|")}.String()
}

// instanceName names a series by the node behind its instance label.
func (n nodes) instanceName(instance string) string {
	host, _, err := net.SplitHostPort(instance)
	if err != nil {
		host = instance
	}
	if name, ok := n.names[host]; ok {
		return name
	}
	return instance
}
