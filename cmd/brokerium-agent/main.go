// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Command brokerium-agent runs on Kafka hosts (or as the main process of lab node containers),
// connects out to brokerium-server and executes typed lifecycle commands (ADR 0009).
package main

import (
	"fmt"
	"os"

	"github.com/bvaliev/brokerium/internal/version"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println(version.String())
		return
	}
	fmt.Fprintln(os.Stderr, "brokerium-agent: not implemented yet")
	os.Exit(1)
}
