// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestInstallationSettings(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	key := "test." + NewID()
	t.Cleanup(func() { _ = s.DeleteSetting(context.Background(), key) })
	if _, err := s.GetSetting(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSetting of a missing key = %v, want ErrNotFound", err)
	}
	st := &Setting{Key: key, Value: json.RawMessage(`{"url":"http://a"}`), SecretsSealed: []byte{1, 2}, UpdatedBy: "alice"}
	if err := s.SetSetting(ctx, st); err != nil || st.UpdatedAt.IsZero() {
		t.Fatalf("SetSetting = %v, updated at %v", err, st.UpdatedAt)
	}
	if err := s.SetSetting(ctx, &Setting{Key: key, Value: json.RawMessage(`{"url":"http://b"}`), UpdatedBy: "bob"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSetting(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	var value struct{ URL string }
	if err := json.Unmarshal(got.Value, &value); err != nil || value.URL != "http://b" || got.SecretsSealed != nil ||
		got.UpdatedBy != "bob" {
		t.Errorf("after the second SetSetting: value %s, secrets %v, by %q", got.Value, got.SecretsSealed, got.UpdatedBy)
	}
	if err := s.DeleteSetting(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSetting(ctx, key); err != nil {
		t.Errorf("deleting a missing setting: %v", err)
	}
	if _, err := s.GetSetting(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetSetting after DeleteSetting = %v, want ErrNotFound", err)
	}
}

func TestClusterMonitoring(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c := &Cluster{Name: "test-" + NewID(), Provider: ProviderExternal, BootstrapServers: []string{"localhost:9092"}}
	if err := s.CreateCluster(ctx, c); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.DeleteCluster(context.Background(), c.ID) })
	if _, err := s.GetClusterMonitoring(ctx, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetClusterMonitoring without settings = %v, want ErrNotFound", err)
	}
	m := &ClusterMonitoring{ClusterID: c.ID, Selector: `job="kafka"`, ExporterPort: 7071, UpdatedBy: "alice"}
	if err := s.SetClusterMonitoring(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := s.SetClusterMonitoring(ctx, &ClusterMonitoring{ClusterID: c.ID, UpdatedBy: "bob"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetClusterMonitoring(ctx, c.ID)
	if err != nil || got.Selector != "" || got.ExporterPort != 0 || got.UpdatedBy != "bob" {
		t.Errorf("GetClusterMonitoring = %+v, %v", got, err)
	}
	if err := s.SetClusterMonitoring(ctx, &ClusterMonitoring{ClusterID: "missing-" + NewID()}); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetClusterMonitoring of a missing cluster = %v, want ErrNotFound", err)
	}
	if err := s.DeleteCluster(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetClusterMonitoring(ctx, c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("the settings outlived their cluster: %v", err)
	}
}
