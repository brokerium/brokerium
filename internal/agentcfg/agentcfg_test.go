// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package agentcfg

import "testing"

func TestValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		props map[string]string
		heap  string
		ok    bool
	}{
		{"defaults", map[string]string{"node.id": "101", "listener.security.protocol.map": "INTERNAL:PLAINTEXT,EXTERNAL:PLAINTEXT"}, "-Xmx512m -Xms512m", true},
		{"jaas config", map[string]string{"listener.name.sasl.plain.sasl.jaas.config": `org.apache.kafka.common.security.plain.PlainLoginModule required username="admin" password="a b;c";`}, "", true},
		{"heap in gigabytes", nil, "-Xms2G -Xmx2G", true},
		{"newline in a value", map[string]string{"log.dirs": "/var/lib/kafka\nauthorizer.class.name=x"}, "", false},
		{"newline in a key", map[string]string{"a\nb": "c"}, "", false},
		{"key with spaces", map[string]string{"log dirs": "/x"}, "", false},
		{"unit directive in the heap", nil, "-Xmx512m\nExecStartPre=/bin/sh -c id", false},
		{"other JVM option", nil, "-Xmx512m -XX:OnOutOfMemoryError=id", false},
		{"java agent", nil, "-javaagent:/tmp/a.jar", false},
		{"double space", nil, "-Xmx512m  -Xms512m", false},
		{"zero size", nil, "-Xmx0", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := Validate(tt.props, tt.heap); (err == nil) != tt.ok {
				t.Errorf("Validate() = %v, want ok=%v", err, tt.ok)
			}
		})
	}
}
