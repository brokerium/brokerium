// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Package docker is the lab node provider: it talks to the Docker Engine API over its unix
// socket to create and remove containers that run brokerium-agent, simulating virtual machines,
// and decides whether the server offers Docker clusters at all (ADR 0019).
package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ErrNotFound is returned for missing containers or volumes.
var ErrNotFound = errors.New("not found")

// Client is a minimal Docker Engine API client.
type Client struct {
	http *http.Client
	base string
	host string
}

// New connects to host ("unix:///path" or "tcp://host:port"). An empty host or a missing
// default socket falls back to DOCKER_HOST and the Docker Desktop socket in the home directory.
// When no socket exists yet, the client keeps the socket of host and uses it once it appears.
func New(host string) (*Client, error) {
	candidates := []string{host, os.Getenv("DOCKER_HOST"), "unix:///var/run/docker.sock"}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, "unix://"+filepath.Join(home, ".docker", "run", "docker.sock"))
	}
	for _, h := range candidates {
		if h == "" {
			continue
		}
		if path, ok := strings.CutPrefix(h, "unix://"); ok {
			if _, err := os.Stat(path); err != nil { //nolint:gosec // socket path from BROKERIUM_DOCKER_HOST
				continue
			}
			return unixClient(h, path), nil
		}
		if addr, ok := strings.CutPrefix(h, "tcp://"); ok {
			return &Client{http: &http.Client{}, base: "http://" + addr, host: h}, nil
		}
	}
	if path, ok := strings.CutPrefix(host, "unix://"); ok && path != "" {
		return unixClient(host, path), nil
	}
	return nil, fmt.Errorf("unsupported Docker host %q: use unix:///path or tcp://host:port", host)
}

func unixClient(host, path string) *Client {
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}}
	return &Client{http: &http.Client{Transport: tr}, base: "http://docker", host: host}
}

// Host is the Docker host the client connects to.
func (c *Client) Host() string {
	return c.host
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrNotFound, strings.TrimSpace(string(data)))
	case resp.StatusCode == http.StatusNotModified:
		return nil
	case resp.StatusCode >= 300:
		return fmt.Errorf("docker %s %s: HTTP %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

// Ping checks the connection.
func (c *Client) Ping(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/_ping", nil, nil)
}

// ContainerSpec describes a node container.
type ContainerSpec struct {
	Name     string
	Image    string
	Hostname string
	Network  string
	Env      []string
	// Volumes maps volume names to mount paths.
	Volumes map[string]string
	// Ports maps container TCP ports to host ports.
	Ports  map[int]int
	Labels map[string]string
}

// Run creates and starts a container; an existing container with the name is started.
func (c *Client) Run(ctx context.Context, spec ContainerSpec) (string, error) {
	if info, err := c.Inspect(ctx, spec.Name); err == nil {
		if !info.Running {
			if err := c.Start(ctx, spec.Name); err != nil {
				return "", err
			}
		}
		return info.ID, nil
	}
	exposed := map[string]struct{}{}
	bindings := map[string][]map[string]string{}
	for cp, hp := range spec.Ports {
		key := strconv.Itoa(cp) + "/tcp"
		exposed[key] = struct{}{}
		bindings[key] = []map[string]string{{"HostPort": strconv.Itoa(hp)}}
	}
	var binds []string
	for vol, path := range spec.Volumes {
		if err := c.do(ctx, http.MethodPost, "/volumes/create", map[string]any{"Name": vol, "Labels": spec.Labels}, nil); err != nil {
			return "", err
		}
		binds = append(binds, vol+":"+path)
	}
	body := map[string]any{
		"Image":        spec.Image,
		"Hostname":     spec.Hostname,
		"Env":          spec.Env,
		"Labels":       spec.Labels,
		"ExposedPorts": exposed,
		"HostConfig": map[string]any{
			"Binds":         binds,
			"PortBindings":  bindings,
			"NetworkMode":   spec.Network,
			"ExtraHosts":    []string{"host.docker.internal:host-gateway"},
			"RestartPolicy": map[string]string{"Name": "unless-stopped"},
			"Memory":        1 << 30,
		},
	}
	var created struct{ ID string }
	create := "/containers/create?name=" + url.QueryEscape(spec.Name)
	err := c.do(ctx, http.MethodPost, create, body, &created)
	if errors.Is(err, ErrNotFound) && strings.Contains(strings.ToLower(err.Error()), "no such image") {
		if err := c.Pull(ctx, spec.Image); err != nil {
			return "", err
		}
		err = c.do(ctx, http.MethodPost, create, body, &created)
	}
	if err != nil {
		return "", err
	}
	if err := c.Start(ctx, spec.Name); err != nil {
		return "", err
	}
	return created.ID, nil
}

// Pull downloads an image from its registry.
func (c *Client) Pull(ctx context.Context, image string) error {
	repo, tag := splitImage(image)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.base+"/images/create?fromImage="+url.QueryEscape(repo)+"&tag="+url.QueryEscape(tag), nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("docker pull %s: HTTP %d: %s", image, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	// The pull reports progress and failures as a stream of JSON messages.
	dec := json.NewDecoder(resp.Body)
	for {
		var msg struct {
			Error string `json:"error"`
		}
		err := dec.Decode(&msg)
		switch {
		case errors.Is(err, io.EOF):
			return nil
		case err != nil:
			return fmt.Errorf("docker pull %s: %w", image, err)
		case msg.Error != "":
			return fmt.Errorf("docker pull %s: %s", image, msg.Error)
		}
	}
}

// splitImage splits an image reference into the repository and the tag or digest. The tag
// defaults to latest: an empty tag would pull every tag of the repository.
func splitImage(image string) (repo, tag string) {
	if i := strings.LastIndex(image, "@"); i >= 0 {
		return image[:i], image[i+1:]
	}
	if i := strings.LastIndex(image, ":"); i > strings.LastIndex(image, "/") {
		return image[:i], image[i+1:]
	}
	return image, "latest"
}

// Info is the state of a container.
type Info struct {
	ID      string
	Running bool
}

// Inspect returns the state of a container.
func (c *Client) Inspect(ctx context.Context, name string) (Info, error) {
	var out struct {
		ID    string
		State struct{ Running bool }
	}
	if err := c.do(ctx, http.MethodGet, "/containers/"+url.PathEscape(name)+"/json", nil, &out); err != nil {
		return Info{}, err
	}
	return Info{ID: out.ID, Running: out.State.Running}, nil
}

// Start starts a container.
func (c *Client) Start(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodPost, "/containers/"+url.PathEscape(name)+"/start", nil, nil)
}

// Stop stops a container, waiting up to timeout.
func (c *Client) Stop(ctx context.Context, name string, timeout time.Duration) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/containers/%s/stop?t=%d", url.PathEscape(name), int(timeout.Seconds())), nil, nil)
}

// Remove force-removes a container and the given volumes. Missing objects are ignored.
func (c *Client) Remove(ctx context.Context, name string, volumes ...string) error {
	if err := c.do(ctx, http.MethodDelete, "/containers/"+url.PathEscape(name)+"?force=1&v=1", nil, nil); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	for _, v := range volumes {
		if err := c.do(ctx, http.MethodDelete, "/volumes/"+url.PathEscape(v)+"?force=1", nil, nil); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
	}
	return nil
}
