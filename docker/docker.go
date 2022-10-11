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
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/go-kit/log"
	"github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/util/strutil"
)

const (
	dockerLabel                     = model.MetaLabelPrefix + "docker_"
	dockerLabelContainerPrefix      = dockerLabel + "container_"
	dockerLabelContainerID          = dockerLabelContainerPrefix + "id"
	dockerLabelContainerName        = dockerLabelContainerPrefix + "name"
	dockerLabelContainerNetworkMode = dockerLabelContainerPrefix + "network_mode"
	dockerLabelContainerLabelPrefix = dockerLabelContainerPrefix + "label_"
	dockerLabelNetworkPrefix        = dockerLabel + "network_"
	dockerLabelNetworkIP            = dockerLabelNetworkPrefix + "ip"
	dockerLabelPortPrefix           = dockerLabel + "port_"
	dockerLabelPortPrivate          = dockerLabelPortPrefix + "private"
	dockerLabelPortPublic           = dockerLabelPortPrefix + "public"
	dockerLabelPortPublicIP         = dockerLabelPortPrefix + "public_ip"
	userAgent                       = "github.com/bredtape/prometheus_docker_sd"

	ExtractLabelPrefix  = "prometheus_"
	JobLabelPrefix      = ExtractLabelPrefix + "job"
	ExtractScrapePrefix = "prometheus_scrape_"
	ScrapePort          = ExtractScrapePrefix + "port"
	ScrapeInterval      = ExtractScrapePrefix + "interval"
	ScrapeTimeout       = ExtractScrapePrefix + "timeout"
	ScrapePath          = ExtractScrapePrefix + "path"
	ScrapeScheme        = ExtractScrapePrefix + "scheme"
)

type Export struct {
	Targets []string          `yaml:"targets"`
	Labels  map[string]string `yaml:"labels,omitempty"`
}

// Config is the configuration for Docker (non-swarm) based service discovery.
type Config struct {
	HTTPClientConfig config.HTTPClientConfig `yaml:",inline"`
	Host             string                  `yaml:"host"`
	RefreshInterval  time.Duration           `yaml:"refresh_interval"`

	// prefix for instance. The Container name is appended
	InstancePrefix string
	// network that the Container must be a member of
	TargetNetwork string
}

type Discovery struct {
	client         *client.Client
	logger         log.Logger
	instancePrefix string
	targetNetwork  string
}

// New returns a new DockerDiscovery which periodically refreshes its targets.
func New(conf *Config, logger log.Logger) (*Discovery, error) {
	var err error

	d := &Discovery{
		logger:         logger,
		instancePrefix: conf.InstancePrefix,
		targetNetwork:  conf.TargetNetwork}

	hostURL, err := url.Parse(conf.Host)
	if err != nil {
		return nil, err
	}

	opts := []client.Opt{
		client.WithHost(conf.Host),
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

func (d *Discovery) Refresh(ctx context.Context) ([]Export, error) {
	containers, err := d.client.ContainerList(ctx, types.ContainerListOptions{})
	if err != nil {
		return nil, fmt.Errorf("error while listing containers: %w", err)
	}

	networkLabels, err := getNetworksLabels(ctx, d.client, dockerLabel)
	if err != nil {
		return nil, fmt.Errorf("error while computing network labels: %w", err)
	}

	return extract(d.logger, d.instancePrefix, d.targetNetwork, containers, networkLabels), nil
}

func extract(logger log.Logger, instancePrefix string, targetNetworkName string, containers []types.Container, networkLabels map[string]map[string]string) []Export {

	exports := make([]Export, 0)

	notInNetwork := 0
	noPorts := 0
	multiplePortsNotExplicit := 0

	for _, c := range containers {
		if len(c.Names) == 0 {
			continue
		}

		_, exists := c.Labels[JobLabelPrefix]
		if !exists {
			continue
		}

		labels := map[string]string{
			dockerLabelContainerID:          c.ID,
			dockerLabelContainerName:        c.Names[0],
			dockerLabelContainerNetworkMode: c.HostConfig.NetworkMode}

		var scrapePort string
		for k, v := range c.Labels {
			ln := strutil.SanitizeLabelName(k)

			if strings.HasPrefix(ln, ExtractScrapePrefix) {
				switch k {
				case ScrapePort:
					scrapePort = v
				case ScrapeInterval:
					labels[model.ScrapeIntervalLabel] = v
				case ScrapeTimeout:
					labels[model.ScrapeTimeoutLabel] = v
				case ScrapePath:
					labels[model.MetricsPathLabel] = v
				case ScrapeScheme:
					labels[model.SchemeLabel] = v
				}
			} else if strings.HasPrefix(ln, ExtractLabelPrefix) {
				labels[ln[len(ExtractLabelPrefix):]] = v
			} else {
				labels[dockerLabelContainerLabelPrefix+ln] = v
			}
		}

		n, found := c.NetworkSettings.Networks[targetNetworkName]
		if !found {
			notInNetwork++
			continue
		}

		// match scrape port, fallback to lowest if not defined/found
		p, found := matchScrapePort(c.Ports, scrapePort)
		if !found {
			pp, candidates, found := findLowestTCPPrivatePort(c.Ports)
			if !found {
				noPorts++
				continue
			}
			p = pp

			if scrapePort == "" && candidates > 1 {
				multiplePortsNotExplicit++
			}
		}

		labels[dockerLabelNetworkIP] = n.IPAddress
		labels[dockerLabelPortPrivate] = strconv.FormatUint(uint64(p.PrivatePort), 10)

		if p.PublicPort > 0 {
			labels[dockerLabelPortPublic] = strconv.FormatUint(uint64(p.PublicPort), 10)
			labels[dockerLabelPortPublicIP] = p.IP
		}

		for k, v := range networkLabels[n.NetworkID] {
			labels[k] = v
		}

		if scrapePort == "" {
			scrapePort = strconv.FormatUint(uint64(p.PrivatePort), 10)
		}

		addr := net.JoinHostPort(n.IPAddress, scrapePort)
		labels[model.AddressLabel] = addr
		labels[model.InstanceLabel] = instancePrefix + c.Names[0] + ":" + scrapePort

		exports = append(exports, Export{
			Targets: []string{addr},
			Labels:  labels})
	}

	metric_count.WithLabelValues().Set(float64(len(containers)))
	metric_ignored_containers_not_in_network.WithLabelValues(targetNetworkName).Set(float64(notInNetwork))
	metric_ignored_no_ports.WithLabelValues(targetNetworkName).Set(float64(noPorts))
	metric_multiple_ports.WithLabelValues(targetNetworkName).Set(float64(multiplePortsNotExplicit))
	return exports
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
	for _, x := range xs {
		if x.Type != "tcp" {
			continue
		}
		candidates++
		if x.PrivatePort < min {
			min = x.PrivatePort
			entry = x
		}
	}

	return entry, candidates, min < math.MaxUint16
}
