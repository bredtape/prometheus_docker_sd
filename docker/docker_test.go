package docker

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/network"
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/model"
	. "github.com/smartystreets/goconvey/convey"
)

func TestExtractSingleContainer(t *testing.T) {
	instancePrefix := "host1"
	targetNetwork := "metrics-net"
	logger := log.NewJSONLogger(os.Stdout)

	Convey("given container with prometheus_job label, in target network and with 1 exposed port", t, func() {
		c := types.Container{
			ID:    "containerID",
			Names: []string{"/containerName"},
			Labels: map[string]string{
				"prometheus_job": "job1"},
			Ports: []types.Port{{Type: "tcp", PrivatePort: 2000}},
			NetworkSettings: &types.SummaryNetworkSettings{
				Networks: map[string]*network.EndpointSettings{
					targetNetwork: {IPAddress: "ip1"}}}}

		xs := extract(logger, instancePrefix, targetNetwork, []types.Container{c}, nil)

		Convey("should have 1 entry", func() {
			So(xs, ShouldHaveLength, 1)

			x := xs[0]
			Convey("with single target ip1:2000", func() {
				So(x.Targets, ShouldResemble, []string{"ip1:2000"})
			})

			Convey("should have label", func() {
				Convey("job", func() {
					So(x.Labels, ShouldContainKey, model.JobLabel)
					So(x.Labels[model.JobLabel], ShouldEqual, "job1")
				})

				Convey("instance", func() {
					So(x.Labels, ShouldContainKey, model.InstanceLabel)
					So(x.Labels[model.InstanceLabel], ShouldEqual, "host1/containerName")
				})

				Convey("should not have label prometheus_job", func() {
					So(x.Labels, ShouldNotContainKey, "prometheus_job")
				})
			})

			Convey("metric containers ignored, should be 0", func() {
				So(testutil.ToFloat64(metric_ignored_containers_not_in_network.WithLabelValues(targetNetwork)), ShouldEqual, 0)
			})

			Convey("metric containers ignored no ports, should be 0", func() {
				So(testutil.ToFloat64(metric_ignored_no_ports.WithLabelValues(targetNetwork)), ShouldEqual, 0)
			})

			Convey("metric containers multiple ports, scrape port explicit, should be 0", func() {
				So(testutil.ToFloat64(metric_multiple_ports.WithLabelValues(targetNetwork)), ShouldEqual, 0)
			})
		})

		Convey("with label "+ScrapePort, func() {
			Convey("2001", func() {
				c.Labels[ScrapePort] = "2001"

				x := extract(logger, instancePrefix, targetNetwork, []types.Container{c}, nil)[0]

				Convey("should have target with port 2001", func() {
					So(x.Targets, ShouldResemble, []string{"ip1:2001"})
				})

				Convey("should not have any labels with prefix "+dockerLabelContainerLabelPrefix+ExtractScrapePrefix, func() {
					So(x.Labels, ShouldNotHaveKeyWithPrefix, dockerLabelContainerLabelPrefix+ExtractScrapePrefix)
				})
			})
		})

		Convey("with label "+ScrapeInterval, func() {
			Convey("5s", func() {
				c.Labels[ScrapeInterval] = "5s"

				x := extract(logger, instancePrefix, targetNetwork, []types.Container{c}, nil)[0]

				Convey("should have label "+model.ScrapeIntervalLabel, func() {
					So(x.Labels, ShouldContainKey, model.ScrapeIntervalLabel)
					So(x.Labels[model.ScrapeIntervalLabel], ShouldEqual, "5s")
				})

				Convey("should not have any labels with prefix "+dockerLabelContainerLabelPrefix+ExtractScrapePrefix, func() {
					So(x.Labels, ShouldNotHaveKeyWithPrefix, dockerLabelContainerLabelPrefix+ExtractScrapePrefix)
				})
			})
		})

		Convey("with label "+ScrapeTimeout, func() {
			Convey("10s", func() {
				c.Labels[ScrapeTimeout] = "10s"

				x := extract(logger, instancePrefix, targetNetwork, []types.Container{c}, nil)[0]

				Convey("should have label "+model.ScrapeTimeoutLabel, func() {
					So(x.Labels, ShouldContainKey, model.ScrapeTimeoutLabel)
					So(x.Labels[model.ScrapeTimeoutLabel], ShouldEqual, "10s")
				})

				Convey("should not have any labels with prefix "+dockerLabelContainerLabelPrefix+ExtractScrapePrefix, func() {
					So(x.Labels, ShouldNotHaveKeyWithPrefix, dockerLabelContainerLabelPrefix+ExtractScrapePrefix)
				})
			})
		})

		Convey("with label "+ScrapePath, func() {
			Convey("10s", func() {
				c.Labels[ScrapePath] = "/stuff/metrics"

				x := extract(logger, instancePrefix, targetNetwork, []types.Container{c}, nil)[0]

				Convey("should have label "+model.MetricsPathLabel, func() {
					So(x.Labels, ShouldContainKey, model.MetricsPathLabel)
					So(x.Labels[model.MetricsPathLabel], ShouldEqual, "/stuff/metrics")
				})

				Convey("should not have any labels with prefix "+dockerLabelContainerLabelPrefix+ExtractScrapePrefix, func() {
					So(x.Labels, ShouldNotHaveKeyWithPrefix, dockerLabelContainerLabelPrefix+ExtractScrapePrefix)
				})
			})
		})

		key := "prometheus_key1"
		Convey("with label "+key+"and value 'val1'", func() {
			c.Labels[key] = "val1"

			x := extract(logger, instancePrefix, targetNetwork, []types.Container{c}, nil)[0]

			Convey("should have label key1", func() {
				So(x.Labels, ShouldContainKey, "key1")
				So(x.Labels["key1"], ShouldEqual, "val1")
			})

			Convey("should not have label "+dockerLabelContainerLabelPrefix+key, func() {
				So(x.Labels, ShouldNotContainKey, dockerLabelContainerLabelPrefix+key)
			})
		})

		key = "prometheus&=5b"
		Convey("with label "+key+"and value 'val1'", func() {
			c.Labels[key] = "val1"

			x := extract(logger, instancePrefix, targetNetwork, []types.Container{c}, nil)[0]

			Convey("should have sanitized label key _5b", func() {
				So(x.Labels, ShouldContainKey, "_5b")
				So(x.Labels["_5b"], ShouldEqual, "val1")
			})

			Convey("should not have label "+dockerLabelContainerLabelPrefix+key, func() {
				So(x.Labels, ShouldNotContainKey, dockerLabelContainerLabelPrefix+key)
			})
		})

		Convey("with extra port", func() {
			Convey("2002, should still have target on 2000", func() {
				c.Ports = append(c.Ports, types.Port{PrivatePort: 2002, Type: "tcp"})
				xs := extract(logger, instancePrefix, targetNetwork, []types.Container{c}, nil)

				Convey("should have 1 entry", func() {
					So(xs, ShouldHaveLength, 1)

					x := xs[0]
					Convey("with single target ip1:2000, picking the lowest port", func() {
						So(x.Targets, ShouldResemble, []string{"ip1:2000"})
					})
				})

				Convey("metric containers multiple ports, scrape port not explicit, should be 1", func() {
					So(testutil.ToFloat64(metric_multiple_ports.WithLabelValues(targetNetwork)), ShouldEqual, 1)
				})
			})

			Convey("1000, should change target port", func() {
				c.Ports = append(c.Ports, types.Port{PrivatePort: 1000, Type: "tcp"})
				xs := extract(logger, instancePrefix, targetNetwork, []types.Container{c}, nil)

				Convey("should have 1 entry", func() {
					So(xs, ShouldHaveLength, 1)

					x := xs[0]
					Convey("with single target ip1:1000, picking the lowest port", func() {
						So(x.Targets, ShouldResemble, []string{"ip1:1000"})
					})
				})

				Convey("metric containers multiple ports, scrape port not explicit, should be 1", func() {
					So(testutil.ToFloat64(metric_multiple_ports.WithLabelValues(targetNetwork)), ShouldEqual, 1)
				})

				Convey("with label "+ScrapePort, func() {
					c.Labels[ScrapePort] = "1998"

					xs := extract(logger, instancePrefix, targetNetwork, []types.Container{c}, nil)

					Convey("should have 1 entry", func() {
						So(xs, ShouldHaveLength, 1)

						x := xs[0]
						Convey("with single target ip1:1998, overriding", func() {
							So(x.Targets, ShouldResemble, []string{"ip1:1998"})
						})
					})

					Convey("metric containers multiple ports, scrape port explicit, should be 0", func() {
						So(testutil.ToFloat64(metric_multiple_ports.WithLabelValues(targetNetwork)), ShouldEqual, 0)
					})
				})
			})
		})

		Convey("not in target network: "+targetNetwork, func() {
			c.NetworkSettings = &types.SummaryNetworkSettings{
				Networks: map[string]*network.EndpointSettings{
					"other": {IPAddress: "ip1"}}}

			xs := extract(logger, instancePrefix, targetNetwork, []types.Container{c}, nil)

			Convey("should have no entries", func() {
				So(xs, ShouldBeEmpty)
			})

			Convey("metric containers ignored, should be 1", func() {
				So(testutil.ToFloat64(metric_ignored_containers_not_in_network.WithLabelValues(targetNetwork)), ShouldEqual, 1)
			})
		})

		Convey("no ports", func() {
			c.Ports = nil

			xs := extract(logger, instancePrefix, targetNetwork, []types.Container{c}, nil)

			Convey("should have no entries", func() {
				So(xs, ShouldBeEmpty)
			})

			Convey("metric containers ignored no ports, should be 1", func() {
				So(testutil.ToFloat64(metric_ignored_no_ports.WithLabelValues(targetNetwork)), ShouldEqual, 1)
			})
		})

		Convey("not a tcp port", func() {
			c.Ports[0].Type = "udp"

			xs := extract(logger, instancePrefix, targetNetwork, []types.Container{c}, nil)

			Convey("should have no entries", func() {
				So(xs, ShouldBeEmpty)
			})

			Convey("metric containers ignored no ports, should be 1", func() {
				So(testutil.ToFloat64(metric_ignored_no_ports.WithLabelValues(targetNetwork)), ShouldEqual, 1)
			})
		})
	})
}

// actual map[string]string
// expected string
func ShouldNotHaveKeyWithPrefix(actual interface{}, expected ...interface{}) string {
	xs, ok := actual.(map[string]string)
	if !ok {
		return "actual should be of type map[string]string"
	}

	if len(expected) != 1 {
		return "expected 1 key prefix"
	}

	y, ok := expected[0].(string)
	if !ok {
		return "expected string"
	}
	if y == "" {
		return "empty prefix"
	}

	found := make([]string, 0)
	for k := range xs {
		if strings.HasPrefix(string(k), y) {
			found = append(found, string(k))
		}
	}

	if len(found) > 0 {
		return fmt.Sprintf("keys with prefix %s: %v", y, found)
	}
	return ""
}
