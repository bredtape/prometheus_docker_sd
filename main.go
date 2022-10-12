package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bredtape/prometheus_docker_sd/docker"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/namsral/flag"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/yaml.v3"
)

var (
	outputFile  string
	httpAddress string

	logger log.Logger
)

const (
	envPrefix = "prometheus_docker_sd"
)

func parseArgs() *docker.Config {
	fs := flag.NewFlagSetWithEnvPrefix(os.Args[0], strings.ToUpper(envPrefix), flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s <options>\n", os.Args[0])
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "Options may also be set from the environment. Prefix with %s_, use all caps and replace any - with _\n", strings.ToUpper(envPrefix))
	}

	var dockerHost, instancePrefix, targetNetworkName string
	var refreshInterval time.Duration
	fs.StringVar(&outputFile, "output-file", "docker_sd.yml", "Output .json, .yml or .yaml file with format as specified in https://prometheus.io/docs/prometheus/latest/configuration/configuration/#file_sd_config")
	fs.StringVar(&dockerHost, "docker-host", "unix:///var/run/docker.sock", "Docker host url")
	fs.StringVar(&targetNetworkName, "target-network-name", "metrics-net", "Network that the containers must be a member of to be considered. Consider making it 'external' in the docker-compose...")
	fs.StringVar(&instancePrefix, "instance-prefix", "", "Prefix added to Container name to form the 'instance' label. Required")
	fs.DurationVar(&refreshInterval, "refresh-interval", 60*time.Second, "Refresh interval to query the Docker host for containers")
	fs.StringVar(&httpAddress, "http-address", ":9200", "http address to serve metrics on")
	var logLevel string
	fs.StringVar(&logLevel, "log-level", "INFO", "Specify log level (DEBUG, INFO, WARN, ERROR)")

	var help bool
	fs.BoolVar(&help, "help", false, "Display help")

	err := fs.Parse(os.Args[1:])
	if err != nil {
		bail(fs, "failed to parse args: %v", err)
	}

	if help {
		fs.Usage()
		os.Exit(2)
	}

	logger = log.NewLogfmtLogger(os.Stderr)
	logger = level.NewFilter(logger, level.Allow(level.ParseDefault(logLevel, level.InfoValue())))
	logger = log.With(logger, "ts", log.DefaultTimestampUTC)

	if len(targetNetworkName) == 0 {
		bail(fs, "'target-network-name' required")
	}

	if len(instancePrefix) == 0 {
		bail(fs, "'instance-prefix' required")
	}

	return &docker.Config{
		Host:            dockerHost,
		InstancePrefix:  instancePrefix,
		TargetNetwork:   targetNetworkName,
		RefreshInterval: refreshInterval}
}

func main() {
	ctx := context.Background()
	config := parseArgs()

	logger := log.NewJSONLogger(os.Stdout)
	logger = log.With(logger, "ts", log.DefaultTimestampUTC)

	_ = level.Info(logger).Log("msg", "starting http handler", "address", httpAddress)
	go http.ListenAndServe(httpAddress, promhttp.Handler())

	d, err := docker.New(config, logger)
	if err != nil {
		_ = level.Error(logger).Log("failed to configure discovery", err)
		os.Exit(4)
	}

	t := time.After(0)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t:
			// refresh timer
			t = time.After(config.RefreshInterval)

			_ = level.Debug(logger).Log("msg", "refresh")
			xs, err := d.Refresh(ctx)
			if err != nil {
				_ = level.Error(logger).Log("msg", "failed to refresh containers", "error", err)
			} else {
				err := writeResultsToFile(outputFile, xs)
				if err != nil {
					_ = level.Error(logger).Log("msg", "failed to write results", "error", err)
				}
			}
		}
	}
}

func writeResultsToFile(outputFile string, xs []docker.Export) error {
	switch filepath.Ext(strings.ToLower(outputFile)) {
	case ".yml", ".yaml":
		data, err := yaml.Marshal(xs)
		if err != nil {
			return errors.Wrap(err, "failed to marshal")
		}
		return os.WriteFile(outputFile, data, 0644)
	case ".json":
		data, err := json.Marshal(xs)
		if err != nil {
			return errors.Wrap(err, "failed to marshal")
		}
		return os.WriteFile(outputFile, data, 0644)
	default:
		return fmt.Errorf("unsupported file extension in output-file: %s", outputFile)
	}
}

func bail(fs *flag.FlagSet, format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format, args...)
	fs.Usage()
	os.Exit(3)
}
