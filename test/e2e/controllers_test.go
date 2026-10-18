// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

//go:build e2e

package e2e

import (
	"context"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/brokerium/brokerium/internal/kafka"
)

// TestControllersDirect checks that Brokerium can read the quorum from the controller listeners
// alone (no brokers involved). Controller listeners are usually reachable only inside the
// cluster network, so run it there:
//
//	BROKERIUM_E2E_CONTROLLERS=delta-c1:9093,delta-c2:9093,delta-c3:9093
func TestControllersDirect(t *testing.T) {
	addrs := os.Getenv("BROKERIUM_E2E_CONTROLLERS")
	if addrs == "" {
		t.Skip("BROKERIUM_E2E_CONTROLLERS is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	profile := kafka.Profile{BootstrapServers: strings.Split(addrs, ",")}

	cc, err := kafka.NewControllerClient(profile)
	if err != nil {
		t.Fatal(err)
	}
	defer cc.Close()

	clusterID, active, controllers, err := cc.DescribeControllers(ctx)
	if err != nil {
		t.Fatalf("DescribeCluster with the controller endpoint type: %v", err)
	}
	t.Logf("cluster %s, active controller %d, controllers %+v", clusterID, active, controllers)
	if len(controllers) == 0 {
		t.Fatal("no controllers returned")
	}

	q, err := cc.DescribeQuorum(ctx)
	if err != nil {
		t.Fatalf("DescribeQuorum: %v", err)
	}
	t.Logf("quorum leader %d epoch %d, %d voters, %d observers", q.LeaderID, q.LeaderEpoch, len(q.Voters), len(q.Observers))
	if q.LeaderID != active {
		t.Errorf("quorum leader %d differs from the active controller %d", q.LeaderID, active)
	}

	// Record which admin APIs a controller listener serves.
	plain, err := kafka.NewClient(profile)
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	av := kmsg.NewPtrApiVersionsRequest()
	av.ClientSoftwareName, av.ClientSoftwareVersion = "brokerium-e2e", "1"
	resp, err := plain.KGO.SeedBrokers()[0].Request(ctx, av)
	if err != nil {
		t.Fatalf("ApiVersions: %v", err)
	}
	if code := resp.(*kmsg.ApiVersionsResponse).ErrorCode; code != 0 {
		t.Fatalf("ApiVersions error code %d", code)
	}
	var names []string
	served := map[int16]bool{}
	for _, k := range resp.(*kmsg.ApiVersionsResponse).ApiKeys {
		served[k.ApiKey] = true
		names = append(names, kmsg.NameForKey(k.ApiKey))
	}
	sort.Strings(names)
	t.Logf("controller listener serves %d APIs: %s", len(names), strings.Join(names, ", "))
	for _, key := range []int16{55, 60, 57, 80, 81} { // DescribeQuorum, DescribeCluster, UpdateFeatures, AddRaftVoter, RemoveRaftVoter
		if !served[key] {
			t.Errorf("controller does not serve %s", kmsg.NameForKey(key))
		}
	}
	t.Logf("Metadata served by controllers: %v", served[3])

	// The regular client routes requests through cluster metadata, which controllers do not
	// provide; record what happens.
	short, cancelShort := context.WithTimeout(ctx, 10*time.Second)
	defer cancelShort()
	_, err = plain.DescribeQuorum(short)
	t.Logf("metadata-routed DescribeQuorum against controllers: err=%v", err)
}
