// Copyright 2021 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package docker

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/util/strutil"
)

const (
	dockerLabel                     = model.MetaLabelPrefix + "docker_"
	dockerLabelContainerPrefix      = dockerLabel + "container_"
	dockerLabelContainerID          = dockerLabelContainerPrefix + "id"
	dockerLabelContainerName        = dockerLabelContainerPrefix + "name"
	dockerLabelContainerState       = dockerLabelContainerPrefix + "state"
	dockerLabelContainerNetworkMode = dockerLabelContainerPrefix + "network_mode"
	dockerLabelContainerLabelPrefix = dockerLabelContainerPrefix + "label_"
	dockerLabelNetworkPrefix        = dockerLabel + "network_"
	dockerLabelNetworkIP            = dockerLabelNetworkPrefix + "ip"
	dockerLabelPortPrefix           = dockerLabel + "port_"
	dockerLabelPortPrivate          = dockerLabelPortPrefix + "private"
	dockerLabelPortPublic           = dockerLabelPortPrefix + "public"
	dockerLabelPortPublicIP         = dockerLabelPortPrefix + "public_ip"
	userAgent                       = "github.com/bredtape/prometheus_docker_sd"
	extractLabelPrefix              = "prometheus_"
	jobLabelPrefix                  = extractLabelPrefix + "job"
	extractScrapePrefix             = "prometheus_scrape_"
	scrapePort                      = extractScrapePrefix + "port"
	scrapeInterval                  = extractScrapePrefix + "interval"
	scrapeTimeout                   = extractScrapePrefix + "timeout"
	scrapePath                      = extractScrapePrefix + "path"
	scrapeScheme                    = extractScrapePrefix + "scheme"
	scrapeExternal                  = extractScrapePrefix + "external"
	fakeIP                          = "1.1.1.1"
)

type Meta struct {
	Name    string
	Address string
	Labels  map[string]string

	HasJob            bool
	IsInTargetNetwork bool
	HasTCPPorts       bool // at least 1 TCP port
	HasExplicitPort   bool // explicit or single port
	ScrapeExternal    bool
}

// whether the Container is exported
func (m Meta) IsExported() bool {
	return m.HasJob && m.IsInTargetNetwork && m.HasTCPPorts
}

// Config is the configuration for Docker (non-swarm) based service discovery.
type Config struct {
	HTTPClientConfig config.HTTPClientConfig `yaml:",inline"`
	// docker host url, e.g. unix:///var/run/docker.sock
	DockerHost      string        `yaml:"host"`
	RefreshInterval time.Duration `yaml:"refresh_interval"`

	// external host. To be used with targets that require external scraping
	ExternalHost string

	// prefix for instance. The Container name is appended
	InstancePrefix string
	// network that the Container must be a member of
	TargetNetwork string
}

type Discovery struct {
	client         *client.Client
	instancePrefix string
	externalHost   string
	targetNetwork  string
	log            *slog.Logger
}

func New(conf *Config) (*Discovery, error) {
	var err error

	d := &Discovery{
		instancePrefix: conf.InstancePrefix,
		targetNetwork:  conf.TargetNetwork,
		externalHost:   conf.ExternalHost,
		log: slog.Default().With(
			"targetNetwork", conf.TargetNetwork,
			"instancePrefix", conf.InstancePrefix)}

	hostURL, err := url.Parse(conf.DockerHost)
	if err != nil {
		return nil, err
	}

	opts := []client.Opt{
		client.WithHost(conf.DockerHost),
		client.WithAPIVersionNegotiation(),
	}

	// There are other protocols than HTTP supported by the Docker daemon, like
	// unix, which are not supported by the HTTP client. Passing HTTP client
	// options to the Docker client makes those non-HTTP requests fail.
	if hostURL.Scheme == "http" || hostURL.Scheme == "https" {
		rt, err := config.NewRoundTripperFromConfig(conf.HTTPClientConfig, "docker_sd")
		if err != nil {
			return nil, err
		}
		opts = append(opts,
			client.WithHTTPClient(&http.Client{
				Transport: rt,
				Timeout:   time.Duration(conf.RefreshInterval),
			}),
			client.WithScheme(hostURL.Scheme),
			client.WithHTTPHeaders(map[string]string{
				"User-Agent": userAgent,
			}),
		)
	}

	d.client, err = client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("error setting up docker client: %w", err)
	}

	return d, nil
}

func (d *Discovery) Refresh(ctx context.Context) ([]Meta, error) {
	containers, err := d.client.ContainerList(ctx, container.ListOptions{All: true, Latest: true})
	if err != nil {
		return nil, fmt.Errorf("error while listing containers: %w", err)
	}

	networkLabels, err := getNetworksLabels(ctx, d.client, dockerLabel)
	if err != nil {
		return nil, fmt.Errorf("error while computing network labels: %w", err)
	}

	return extract(d.log, d.instancePrefix, d.externalHost, d.targetNetwork, containers, networkLabels), nil
}

func extract(parentLog *slog.Logger, instancePrefix, externalHost, targetNetworkName string, containers []types.Container, networkLabels map[string]map[string]string) []Meta {

	result := make([]Meta, 0)

	for _, c := range containers {
		if len(c.Names) == 0 {
			continue
		}

		log := parentLog.With(
			"container", c.ID,
			"name", c.Names[0])

		meta := Meta{
			Name: c.Names[0],
			Labels: map[string]string{
				dockerLabelContainerID:          c.ID,
				dockerLabelContainerName:        c.Names[0],
				dockerLabelContainerState:       c.State,
				dockerLabelContainerNetworkMode: c.HostConfig.NetworkMode}}

		if _, exists := c.Labels[jobLabelPrefix]; exists {
			meta.HasJob = true
		}

		var port string
		for k, v := range c.Labels {
			ln := strutil.SanitizeLabelName(k)

			if strings.HasPrefix(ln, extractScrapePrefix) {
				switch k {
				case scrapePort:
					port = v
				case scrapeInterval:
					meta.Labels[model.ScrapeIntervalLabel] = v
				case scrapeTimeout:
					meta.Labels[model.ScrapeTimeoutLabel] = v
				case scrapePath:
					meta.Labels[model.MetricsPathLabel] = v
				case scrapeScheme:
					meta.Labels[model.SchemeLabel] = v
				case scrapeExternal:
					meta.ScrapeExternal = strings.ToLower(v) == "true"
				}
			} else if strings.HasPrefix(ln, extractLabelPrefix) {
				meta.Labels[ln[len(extractLabelPrefix):]] = v
			} else {
				meta.Labels[dockerLabelContainerLabelPrefix+ln] = v
			}
		}

		n, found := c.NetworkSettings.Networks[targetNetworkName]
		if !meta.ScrapeExternal && !found {
			log.Debug("network not found and no explicit scrape port",
				"targetNetwork", targetNetworkName,
				"networks", c.NetworkSettings.Networks)
			result = append(result, meta)
			continue
		}
		log = log.With("networkIP", n.IPAddress)

		meta.IsInTargetNetwork = true

		// no ports, but scrape port explicitly defined
		if len(c.Ports) == 0 && port != "" {
			p, _ := strconv.Atoi(port)
			c.Ports = append(c.Ports, types.Port{Type: "tcp", PrivatePort: uint16(p)})
			if n.IPAddress == "" {
				// insert fake IP to not have Prometheus validation fail, but the actual scrape
				// this happens when a container continous to restart
				n.IPAddress = fakeIP
				log.Info("no ports found or private IP address found, but explicit port specified. Will use explicit port and fake IP", "port", port, "ip", n.IPAddress)
			}
		}

		meta.Labels[dockerLabelNetworkIP] = n.IPAddress

		// match scrape port, fallback to lowest if not defined/found
		p, found := matchScrapePort(c.Ports, port)
		if found {
			meta.HasExplicitPort = true
		} else {
			pp, candidates, found := findLowestTCPPrivatePort(c.Ports)
			if !found {
				result = append(result, meta)
				log.Debug("no TCP ports found", "ports", c.Ports)
				continue
			}
			p = pp

			if candidates == 1 || port != "" {
				meta.HasExplicitPort = true
			}
		}
		meta.HasTCPPorts = true
		meta.Labels[dockerLabelPortPrivate] = strconv.FormatUint(uint64(p.PrivatePort), 10)

		if p.PublicPort > 0 {
			meta.Labels[dockerLabelPortPublic] = strconv.FormatUint(uint64(p.PublicPort), 10)
			meta.Labels[dockerLabelPortPublicIP] = p.IP
		}

		for k, v := range networkLabels[n.NetworkID] {
			meta.Labels[k] = v
		}

		if port == "" {
			port = strconv.FormatUint(uint64(p.PrivatePort), 10)
		}

		if meta.ScrapeExternal {
			meta.Address = net.JoinHostPort(externalHost, port)
		} else {
			meta.Address = net.JoinHostPort(n.IPAddress, port)
		}
		meta.Labels[model.AddressLabel] = meta.Address
		meta.Labels[model.InstanceLabel] = instancePrefix + meta.Name + ":" + port

		result = append(result, meta)
	}

	sort.Slice(result, func(i, j int) bool {
		x, y := result[i], result[j]
		if !x.IsExported() && y.IsExported() {
			return true
		}
		if x.IsExported() && !y.IsExported() {
			return false
		}
		return x.Name < y.Name
	})

	return result
}

func matchScrapePort(xs []types.Port, scrapePort string) (types.Port, bool) {
	for _, x := range xs {
		if x.Type != "tcp" {
			continue
		}
		if strconv.FormatUint(uint64(x.PrivatePort), 10) == scrapePort {
			return x, true
		}
	}
	return types.Port{}, false
}

func findLowestTCPPrivatePort(xs []types.Port) (types.Port, int, bool) {
	candidates := 0
	min := uint16(math.MaxUint16)
	var entry types.Port
	seen := map[uint16]struct{}{} // the same port may be mentioned multiple times (different host IP)
	for _, x := range xs {
		if x.Type != "tcp" {
			continue
		}

		if x.PrivatePort < min {
			min = x.PrivatePort
			entry = x
		}

		if _, exists := seen[x.PrivatePort]; !exists {
			candidates++
		}

		seen[x.PrivatePort] = struct{}{}
	}

	return entry, candidates, min < math.MaxUint16
}
