// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package agentcfg

import (
	"strings"
	"testing"
)

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

func TestValidateHost(t *testing.T) {
	t.Parallel()
	tests := []struct {
		host string
		ok   bool
	}{
		{"kafka-17.example.com", true},
		{"Kafka-17", true},
		{"k", true},
		{"1kafka.example.com", true},
		{"broker.1", true},
		{"192.0.2.10", true},
		{"2001:db8::10", true},
		{"::1", true},
		{"", false},
		{"kafka_17", false},
		{"-kafka", false},
		{"kafka-", false},
		{"kafka..example.com", false},
		{"kafka.example.com.", false},
		{"kafka.example.com:9092", false},
		{"[2001:db8::10]", false},
		{"fe80::1%eth0", false},
		{"10.0.0.256", false},
		{"12345", false},
		{"kafka 17", false},
		{"kafka\n17", false},
		{strings.Repeat("a", 64) + ".example.com", false},
		{strings.Repeat(strings.Repeat("a", 60)+".", 4) + "com", true},
		{strings.Repeat(strings.Repeat("a", 62)+".", 4) + "com", false},
	}
	for _, tt := range tests {
		if err := ValidateHost(tt.host); (err == nil) != tt.ok {
			t.Errorf("ValidateHost(%q) = %v, want ok=%v", tt.host, err, tt.ok)
		}
	}
}

func TestValidateRack(t *testing.T) {
	t.Parallel()
	for rack, ok := range map[string]bool{"": true, "rack-a": true, "eu-west-1a": true, "Rack_1.2": true,
		strings.Repeat("r", 64): true, strings.Repeat("r", 65): false, "rack a": false, "rack/a": false, "rack\na": false} {
		if err := ValidateRack(rack); (err == nil) != ok {
			t.Errorf("ValidateRack(%q) = %v, want ok=%v", rack, err, ok)
		}
	}
}
