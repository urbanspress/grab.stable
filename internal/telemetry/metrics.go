package telemetry

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"time"
)

type routeMetrics struct {
	count   [6]uint64
	buckets [12]uint64
	sum     float64
}

var latencyBuckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}

func (r *Registry) ConfigureRoutes(routes []string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.routes == nil {
		r.routes = map[string]*routeMetrics{"unmatched": {}}
	}
	for _, route := range routes {
		if len(r.routes) < 1001 && len(route) <= 256 && r.routes[route] == nil {
			r.routes[route] = &routeMetrics{}
		}
	}
}
func (r *Registry) Observe(route string, status int, duration time.Duration) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.routes[route]
	if v == nil {
		v = r.routes["unmatched"]
	}
	if v == nil {
		return
	}
	class := status / 100
	if class < 1 || class > 5 {
		class = 5
	}
	v.count[class]++
	seconds := duration.Seconds()
	v.sum += seconds
	for i, b := range latencyBuckets {
		if seconds <= b {
			v.buckets[i]++
		}
	}
	v.buckets[11]++
}
func (r *Registry) writeRoutes(w io.Writer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.routes))
	for name := range r.routes {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Fprintln(w, "# TYPE grab_route_requests counter\n# TYPE grab_route_duration_seconds histogram")
	for _, name := range names {
		v := r.routes[name]
		label := strconv.Quote(name)
		for class := 1; class <= 5; class++ {
			fmt.Fprintf(w, "grab_route_requests_total{route=%s,status_class=\"%dxx\"} %d\n", label, class, v.count[class])
		}
		for i, b := range latencyBuckets {
			fmt.Fprintf(w, "grab_route_duration_seconds_bucket{route=%s,le=%q} %d\n", label, strconv.FormatFloat(b, 'g', -1, 64), v.buckets[i])
		}
		fmt.Fprintf(w, "grab_route_duration_seconds_bucket{route=%s,le=\"+Inf\"} %d\ngrab_route_duration_seconds_count{route=%s} %d\ngrab_route_duration_seconds_sum{route=%s} %g\n", label, v.buckets[11], label, v.buckets[11], label, v.sum)
	}
}
func (r *Registry) Set(name string, n int64) {
	if r == nil {
		return
	}
	for i, k := range metricNames {
		if k == name {
			r.values[i].Store(n)
			return
		}
	}
}
