package network

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/brendlij/labbeacon/internal/config"
	"github.com/brendlij/labbeacon/internal/metric"
)

type Collector struct {
	Config  config.Network
	Client  *http.Client
	next    time.Time
	ip      string
	checked time.Time
	valid   bool
}

func New(c config.Network) *Collector {
	return &Collector{Config: c, Client: &http.Client{Timeout: c.PublicIP.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *Collector) Collect(ctx context.Context) ([]metric.Sample, error) {
	var out []metric.Sample
	if c.Config.LocalIPs {
		interfaces, err := net.Interfaces()
		if err != nil {
			return nil, err
		}
		for _, iface := range interfaces {
			addresses, err := iface.Addrs()
			if err != nil {
				return out, err
			}
			ips := []string{}
			for _, address := range addresses {
				ips = append(ips, address.String())
			}
			s := metric.Sensor("net_"+metric.Key(iface.Name)+"_ips", iface.Name+" local IPs", "", len(ips))
			s.Attributes = map[string]any{"interface": iface.Name, "addresses": ips}
			out = append(out, s)
		}
	}
	if !c.Config.PublicIP.Enabled {
		return out, nil
	}
	if !time.Now().Before(c.next) {
		c.next = time.Now().Add(c.Config.PublicIP.Interval)
		c.valid = false
		query, cancel := context.WithTimeout(ctx, c.Config.PublicIP.Timeout)
		defer cancel()
		req, err := http.NewRequestWithContext(query, http.MethodGet, c.Config.PublicIP.Endpoint, nil)
		if err != nil {
			return out, err
		}
		resp, err := c.Client.Do(req)
		if err != nil {
			return out, fmt.Errorf("public IP endpoint unavailable")
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return out, fmt.Errorf("public IP endpoint: HTTP %d", resp.StatusCode)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 128))
		if err != nil {
			return out, err
		}
		ip := net.ParseIP(strings.TrimSpace(string(data)))
		if ip == nil {
			return out, fmt.Errorf("public IP endpoint did not return an IP address")
		}
		c.ip = ip.String()
		c.checked = time.Now().UTC()
		c.valid = true
	}
	if c.valid {
		s := metric.Sensor("public_ip", "Public IP", "", c.ip)
		s.Attributes = map[string]any{"checked_at": c.checked.Format(time.RFC3339)}
		out = append(out, s)
	}
	return out, nil
}
