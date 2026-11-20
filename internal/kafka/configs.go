// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package kafka

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kmsg"
)

// ConfigEntry is one configuration value with its source. Sensitive entries carry no value.
type ConfigEntry struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	Source    string `json:"source"`
	ReadOnly  bool   `json:"readOnly"`
	Sensitive bool   `json:"sensitive"`
}

// BrokerConfigs describes the configuration of each broker, asking every broker for its own.
// Brokers that do not answer are missing from the result and named in the error.
func (c *Client) BrokerConfigs(ctx context.Context, ids []int32) (map[int32][]ConfigEntry, error) {
	out := map[int32][]ConfigEntry{}
	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		errs []error
	)
	for _, id := range ids {
		wg.Go(func() {
			entries, err := c.brokerConfig(ctx, id)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, fmt.Errorf("broker %d: %w", id, err))
				return
			}
			out[id] = entries
		})
	}
	wg.Wait()
	return out, errors.Join(errs...)
}

func (c *Client) brokerConfig(ctx context.Context, id int32) ([]ConfigEntry, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req := kmsg.NewPtrDescribeConfigsRequest()
	res := kmsg.NewDescribeConfigsRequestResource()
	res.ResourceType = kmsg.ConfigResourceTypeBroker
	res.ResourceName = strconv.Itoa(int(id))
	req.Resources = append(req.Resources, res)
	resp, err := req.RequestWith(ctx, c.KGO.Broker(int(id)))
	if err != nil {
		return nil, fmt.Errorf("describe configs: %w", err)
	}
	if len(resp.Resources) != 1 {
		return nil, errors.New("describe configs: empty response")
	}
	r := resp.Resources[0]
	if err := errorFor(r.ErrorCode, r.ErrorMessage); err != nil {
		return nil, fmt.Errorf("describe configs: %w", err)
	}
	out := make([]ConfigEntry, 0, len(r.Configs))
	for _, cfg := range r.Configs {
		e := ConfigEntry{Name: cfg.Name, Source: cfg.Source.String(), ReadOnly: cfg.ReadOnly, Sensitive: cfg.IsSensitive}
		if cfg.Value != nil && !cfg.IsSensitive {
			e.Value = *cfg.Value
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
