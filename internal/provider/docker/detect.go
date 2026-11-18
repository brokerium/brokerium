// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package docker

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"sync"
	"time"
)

// Values of BROKERIUM_DOCKER_PROVIDER.
const (
	// ModeAuto offers Docker clusters only in the Docker installation of Brokerium.
	ModeAuto = "auto"
	// ModeOn offers them regardless, for a server that runs on the Docker host in development.
	ModeOn = "on"
	// ModeOff never offers them.
	ModeOff = "off"
)

const (
	pingTimeout = 2 * time.Second
	// recheckAfter is how long the result of a ping is reused, so that a Docker daemon that
	// starts or stops later is noticed without pinging on every request.
	recheckAfter     = 30 * time.Second
	needInstallation = "Docker clusters need the Docker installation of Brokerium: "
)

// ErrUnavailable is returned for changes to Docker clusters while the server does not offer them.
var ErrUnavailable = errors.New("docker clusters are unavailable")

// Availability tells whether the server offers Docker clusters, and if not, why.
type Availability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
}

// Err is nil when Docker clusters are available. Otherwise it matches ErrUnavailable and its
// message is the reason.
func (a Availability) Err() error {
	if a.Available {
		return nil
	}
	return unavailableError(a.Reason)
}

type unavailableError string

func (e unavailableError) Error() string        { return string(e) }
func (e unavailableError) Is(target error) bool { return target == ErrUnavailable }

// environment is what the detection reads; tests replace it.
type environment struct {
	fileExists func(name string) bool
	getenv     func(key string) string
	ping       func(ctx context.Context) error
	now        func() time.Time
}

// Detector decides whether the server offers Docker clusters: always with ModeOn, never with
// ModeOff, and with ModeAuto only in the Docker installation, where the server runs in a Docker
// container outside Kubernetes and reaches the Docker API.
type Detector struct {
	mode         string
	host         string
	client       *Client
	inContainer  bool
	inKubernetes bool
	env          environment

	mu      sync.Mutex
	last    Availability
	checked time.Time
}

// NewDetector creates the Docker client for host and looks at the environment of the server.
func NewDetector(mode, host string) *Detector {
	client, err := New(host)
	ping := func(context.Context) error { return err }
	if err == nil {
		host, ping = client.Host(), client.Ping
	}
	d := newDetector(mode, host, environment{fileExists: fileExists, getenv: os.Getenv, ping: ping, now: time.Now})
	d.client = client
	return d
}

func newDetector(mode, host string, env environment) *Detector {
	return &Detector{mode: mode, host: host, env: env, inContainer: env.fileExists("/.dockerenv"),
		inKubernetes: env.getenv("KUBERNETES_SERVICE_HOST") != ""}
}

func fileExists(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}

// Client is the Docker client, nil when the Docker host is not supported.
func (d *Detector) Client() *Client {
	if d == nil {
		return nil
	}
	return d.client
}

// InContainer reports whether the server runs in a Docker container. Such a server reaches the
// nodes of Docker clusters by container name on the lab network.
func (d *Detector) InContainer() bool {
	return d != nil && d.inContainer
}

// Check tells whether the server offers Docker clusters now.
func (d *Detector) Check(ctx context.Context) Availability {
	switch {
	case d == nil:
		return Availability{Reason: "Docker clusters are not set up on this server"}
	case d.mode == ModeOn:
		return Availability{Available: true}
	case d.mode == ModeOff:
		return Availability{Reason: "Docker clusters are turned off on this server (BROKERIUM_DOCKER_PROVIDER=off)"}
	case d.inKubernetes:
		return Availability{Reason: needInstallation + "this server runs in Kubernetes"}
	case !d.inContainer:
		return Availability{Reason: needInstallation + "this server does not run in a Docker container " +
			"(BROKERIUM_DOCKER_PROVIDER=on offers them for development on the Docker host)"}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.env.now()
	if !d.checked.IsZero() && now.Sub(d.checked) < recheckAfter {
		return d.last
	}
	// A request that ends during the ping must not leave its cancellation as the result.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), pingTimeout)
	defer cancel()
	d.last, d.checked = Availability{Available: true}, now
	if err := d.env.ping(ctx); err != nil {
		d.last = Availability{Reason: needInstallation + pingReason(d.host, err)}
	}
	return d.last
}

func pingReason(host string, err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Sprintf("the Docker API at %s does not answer within %s", host, pingTimeout)
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Sprintf("the server cannot reach the Docker API at %s: %v (mount the Docker socket into the server container)", host, err)
	case errors.Is(err, fs.ErrPermission):
		return fmt.Sprintf("the server cannot reach the Docker API at %s: %v (add the group of the Docker socket to the server container with --group-add)", host, err)
	}
	return fmt.Sprintf("the server cannot reach the Docker API at %s: %v", host, err)
}
