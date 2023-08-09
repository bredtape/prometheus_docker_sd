package docker

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/network"
	"github.com/prometheus/common/model"
	. "github.com/smartystreets/goconvey/convey"
)

func TestExtractSingleContainer(t *testing.T) {
	instancePrefix := "host1"
	targetNetwork := "metrics-net"

	log := slog.Default()

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

		xs := extract(log, instancePrefix, targetNetwork, []types.Container{c}, nil)

		Convey("should have 1 entry", func() {
			So(xs, ShouldHaveLength, 1)

			x := xs[0]
			Convey("with single target ip1:2000", func() {
				So(x.Address, ShouldResemble, "ip1:2000")
			})

			Convey("should have label", func() {
				Convey("job", func() {
					So(x.Labels, ShouldContainKey, model.JobLabel)
					So(x.Labels[model.JobLabel], ShouldEqual, "job1")
				})

				Convey("instance", func() {
					So(x.Labels, ShouldContainKey, model.InstanceLabel)
					So(x.Labels[model.InstanceLabel], ShouldEqual, "host1/containerName:2000")
				})

				Convey("should not have label prometheus_job", func() {
					So(x.Labels, ShouldNotContainKey, "prometheus_job")
				})
			})

			Convey("should be in target network", func() {
				So(x.IsInTargetNetwork, ShouldBeTrue)
			})

			Convey("should have ports", func() {
				So(x.HasTCPPorts, ShouldBeTrue)
			})

			Convey("should have explicit port", func() {
				So(x.HasExplicitPort, ShouldBeTrue)
			})
		})

		Convey("with label "+scrapePort, func() {
			Convey("2001", func() {
				c.Labels[scrapePort] = "2001"

				xs := extract(log, instancePrefix, targetNetwork, []types.Container{c}, nil)
				x := xs[0]

				Convey("should have target with port 2001", func() {
					So(x.Address, ShouldResemble, "ip1:2001")
				})

				Convey("instance", func() {
					So(x.Labels, ShouldContainKey, model.InstanceLabel)
					So(x.Labels[model.InstanceLabel], ShouldEqual, "host1/containerName:2001")
				})

				Convey("should not have any labels with prefix "+dockerLabelContainerLabelPrefix+extractScrapePrefix, func() {
					So(x.Labels, ShouldNotHaveKeyWithPrefix, dockerLabelContainerLabelPrefix+extractScrapePrefix)
				})
			})
		})

		Convey("with label "+scrapeInterval, func() {
			Convey("5s", func() {
				c.Labels[scrapeInterval] = "5s"

				xs := extract(log, instancePrefix, targetNetwork, []types.Container{c}, nil)
				x := xs[0]

				Convey("should have label "+model.ScrapeIntervalLabel, func() {
					So(x.Labels, ShouldContainKey, model.ScrapeIntervalLabel)
					So(x.Labels[model.ScrapeIntervalLabel], ShouldEqual, "5s")
				})

				Convey("should not have any labels with prefix "+dockerLabelContainerLabelPrefix+extractScrapePrefix, func() {
					So(x.Labels, ShouldNotHaveKeyWithPrefix, dockerLabelContainerLabelPrefix+extractScrapePrefix)
				})
			})
		})

		Convey("with label "+scrapeTimeout, func() {
			Convey("10s", func() {
				c.Labels[scrapeTimeout] = "10s"

				xs := extract(log, instancePrefix, targetNetwork, []types.Container{c}, nil)
				x := xs[0]

				Convey("should have label "+model.ScrapeTimeoutLabel, func() {
					So(x.Labels, ShouldContainKey, model.ScrapeTimeoutLabel)
					So(x.Labels[model.ScrapeTimeoutLabel], ShouldEqual, "10s")
				})

				Convey("should not have any labels with prefix "+dockerLabelContainerLabelPrefix+extractScrapePrefix, func() {
					So(x.Labels, ShouldNotHaveKeyWithPrefix, dockerLabelContainerLabelPrefix+extractScrapePrefix)
				})
			})
		})

		Convey("with label "+scrapePath, func() {
			Convey("10s", func() {
				c.Labels[scrapePath] = "/stuff/metrics"

				xs := extract(log, instancePrefix, targetNetwork, []types.Container{c}, nil)
				x := xs[0]

				Convey("should have label "+model.MetricsPathLabel, func() {
					So(x.Labels, ShouldContainKey, model.MetricsPathLabel)
					So(x.Labels[model.MetricsPathLabel], ShouldEqual, "/stuff/metrics")
				})

				Convey("should not have any labels with prefix "+dockerLabelContainerLabelPrefix+extractScrapePrefix, func() {
					So(x.Labels, ShouldNotHaveKeyWithPrefix, dockerLabelContainerLabelPrefix+extractScrapePrefix)
				})
			})
		})

		key := "prometheus_key1"
		Convey("with label "+key+"and value 'val1'", func() {
			c.Labels[key] = "val1"

			xs := extract(log, instancePrefix, targetNetwork, []types.Container{c}, nil)
			x := xs[0]

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

			xs := extract(log, instancePrefix, targetNetwork, []types.Container{c}, nil)
			x := xs[0]

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
				xs := extract(log, instancePrefix, targetNetwork, []types.Container{c}, nil)

				Convey("should have 1 entry", func() {
					So(xs, ShouldHaveLength, 1)
					x := xs[0]

					Convey("with single target ip1:2000, picking the lowest port", func() {
						So(x.Address, ShouldResemble, "ip1:2000")
					})

					Convey("should not have explicit port", func() {
						So(x.HasExplicitPort, ShouldBeFalse)
					})
				})
			})

			Convey("1000, should change target port", func() {
				c.Ports = append(c.Ports, types.Port{PrivatePort: 1000, Type: "tcp"})
				xs := extract(log, instancePrefix, targetNetwork, []types.Container{c}, nil)

				Convey("should have 1 entry", func() {
					So(xs, ShouldHaveLength, 1)

					x := xs[0]
					Convey("with single target ip1:1000, picking the lowest port", func() {
						So(x.Address, ShouldResemble, "ip1:1000")
					})

					Convey("should _not_ have explicit port", func() {
						So(x.HasExplicitPort, ShouldBeFalse)
					})
				})

				Convey("with label "+scrapePort, func() {
					c.Labels[scrapePort] = "1998"

					xs := extract(log, instancePrefix, targetNetwork, []types.Container{c}, nil)

					Convey("should have 1 entry", func() {
						So(xs, ShouldHaveLength, 1)

						x := xs[0]
						Convey("with single target ip1:1998, overriding", func() {
							So(x.Address, ShouldResemble, "ip1:1998")
						})

						Convey("should have explicit port", func() {
							So(x.HasExplicitPort, ShouldBeTrue)
						})
					})
				})
			})
		})

		Convey("with duplicate port", func() {
			c.Ports = append(c.Ports, types.Port{PrivatePort: 2000, Type: "tcp"})

			xs := extract(log, instancePrefix, targetNetwork, []types.Container{c}, nil)

			Convey("should have 1 entry", func() {
				So(xs, ShouldHaveLength, 1)

				x := xs[0]
				Convey("with single target ip1:1000, picking the lowest port", func() {
					So(x.Address, ShouldResemble, "ip1:2000")
				})

				Convey("should have explicit port", func() {
					So(x.HasExplicitPort, ShouldBeTrue)
				})
			})
		})

		Convey("not in target network: "+targetNetwork, func() {
			c.NetworkSettings = &types.SummaryNetworkSettings{
				Networks: map[string]*network.EndpointSettings{
					"other": {IPAddress: "ip1"}}}

			xs := extract(log, instancePrefix, targetNetwork, []types.Container{c}, nil)

			Convey("should have 1 entry", func() {
				So(xs, ShouldHaveLength, 1)

				x := xs[0]

				Convey("should not be in target network", func() {
					So(x.IsInTargetNetwork, ShouldBeFalse)
				})
			})
		})

		Convey("no ports", func() {
			c.Ports = nil

			xs := extract(log, instancePrefix, targetNetwork, []types.Container{c}, nil)

			Convey("should have 1 entry", func() {
				So(xs, ShouldHaveLength, 1)

				x := xs[0]

				Convey("should _not_ have any ports", func() {
					So(x.HasTCPPorts, ShouldBeFalse)
				})
			})
		})

		Convey("not a tcp port", func() {
			c.Ports[0].Type = "udp"

			xs := extract(log, instancePrefix, targetNetwork, []types.Container{c}, nil)

			Convey("should have 1 entry", func() {
				So(xs, ShouldHaveLength, 1)

				x := xs[0]

				Convey("should _not_ have any ports", func() {
					So(x.HasTCPPorts, ShouldBeFalse)
				})
			})
		})

		Convey("no "+jobLabelPrefix, func() {
			delete(c.Labels, jobLabelPrefix)

			xs := extract(log, instancePrefix, targetNetwork, []types.Container{c}, nil)

			Convey("should have 1 entry", func() {
				So(xs, ShouldHaveLength, 1)

				x := xs[0]

				Convey("should _not_ have job", func() {
					So(x.HasJob, ShouldBeFalse)
				})
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
