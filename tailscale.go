package tailscale

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/pkg/fall"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	"tailscale.com/client/tailscale"
	"tailscale.com/ipn"
	"tailscale.com/tailcfg"
	"tailscale.com/types/netmap"
)

const (
	// Default socket path
	DefaultSocketPath = "/tmp/tailscale/tailscaled.sock"
)

type Tailscale struct {
	next plugin.Handler
	zone string
	fall fall.F

	socketPath string
	lc         *tailscale.LocalClient

	mu      sync.RWMutex
	entries map[string]map[string][]string
}

// Name implements the Handler interface.
func (t *Tailscale) Name() string { return "tailscale" }

// ServeDNS implements the CoreDNS plugin.Handler interface
func (t *Tailscale) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	return t.ServeCoreDNS(ctx, w, r)
}

// start connects the Tailscale plugin to a tailscale daemon and populates DNS entries for nodes in the tailnet.
// DNS entries are automatically kept up to date with any node changes.
//
// If t.authkey is non-empty, this function uses that key to connect to the Tailnet using a tsnet server
// instead of connecting to the local tailscaled instance.
func (t *Tailscale) start() error {
	// Use socket path to connect to external tailscaled
	if t.socketPath == "" {
		t.socketPath = DefaultSocketPath
	}
	
	// Check if socket exists
	if _, err := os.Stat(t.socketPath); os.IsNotExist(err) {
		clog.Warningf("Tailscale socket not found at %s, will retry", t.socketPath)
	}
	
	// Create LocalClient that connects to the socket
	t.lc = &tailscale.LocalClient{
		Socket: t.socketPath,
	}

	go t.watchIPNBus()
	
	clog.Infof("Tailscale plugin connected to socket: %s", t.socketPath)
	
	return nil
}

// watchIPNBus watches the Tailscale IPN Bus and updates DNS entries for any netmap update.
// This function does not return. If it is unable to read from the IPN Bus, it will continue to retry.
func (t *Tailscale) watchIPNBus() {
	for {
		watcher, err := t.lc.WatchIPNBus(context.Background(), ipn.NotifyInitialNetMap|ipn.NotifyNoPrivateKeys)
		if err != nil {
			clog.Info("unable to read from Tailscale event bus, retrying in 1 minute")
			time.Sleep(1 * time.Minute)
			continue
		}
		defer watcher.Close()

		for {
			n, err := watcher.Next()
			if err != nil {
				// If we're unable to read, then close watcher and reconnect
				watcher.Close()
				break
			}
			t.processNetMap(n.NetMap)
		}
	}
}

func (t *Tailscale) processNetMap(nm *netmap.NetworkMap) {
	if nm == nil {
		return
	}

	clog.Debugf("Self tags: %+v", nm.SelfNode.Tags().AsSlice())
	nodes := []tailcfg.NodeView{nm.SelfNode}
	nodes = append(nodes, nm.Peers...)

	entries := map[string]map[string][]string{}
	for _, node := range nodes {
		if node.IsWireGuardOnly() {
			// IsWireGuardOnly identifies a node as a Mullvad exit node.
			continue
		}
		if !node.Sharer().IsZero() {
			// Skip shared nodes, since they don't necessarily have unique hostnames within this tailnet.
			// TODO: possibly make it configurable to include shared nodes and figure out what hostname to use.
			continue
		}

		hostname := node.ComputedName()
		
		// Ensure we have a map for this hostname
		if _, ok := entries[hostname]; !ok {
			entries[hostname] = make(map[string][]string)
		}

		// Currently entry["A"/"AAAA"] will have max one element
		for _, pfx := range node.Addresses().AsSlice() {
			addr := pfx.Addr()
			if addr.Is4() {
				entries[hostname]["A"] = append(entries[hostname]["A"], addr.String())
			} else if addr.Is6() {
				entries[hostname]["AAAA"] = append(entries[hostname]["AAAA"], addr.String())
			}
		}

		// Convert hostname-1, hostname-2, etc. to hostname
		// which allows us to define round robin DNS A records.
		// By default, Tailscale automatically adds a numerical suffix to
		// the hostname if it already exists.
		hostnameWithoutSuffix := hostname
		re := regexp.MustCompile(`(?P<hostnameWithoutSuffix>.*)-\d+`)
		if matches := re.FindStringSubmatch(hostname); matches != nil {
			hostnameWithoutSuffix = matches[1]
		}
		
		// Only create grouped entry if it's different from the original hostname
		if hostnameWithoutSuffix != hostname {
			// Ensure we have a map for this grouped hostname
			if _, ok := entries[hostnameWithoutSuffix]; !ok {
				entries[hostnameWithoutSuffix] = make(map[string][]string)
			}
			// Add A/AAAA records for grouped hostname
			for _, pfx := range node.Addresses().AsSlice() {
				addr := pfx.Addr()
				if addr.Is4() {
					entries[hostnameWithoutSuffix]["A"] = append(entries[hostnameWithoutSuffix]["A"], addr.String())
				} else if addr.Is6() {
					entries[hostnameWithoutSuffix]["AAAA"] = append(entries[hostnameWithoutSuffix]["AAAA"], addr.String())
				}
			}
		}

		// Process Tags looking for cname- prefixed ones
		if node.Tags().Len() > 0 {
			for _, raw := range node.Tags().AsSlice() {
				if tag, ok := strings.CutPrefix(raw, "tag:cname-"); ok {
					if _, ok := entries[tag]; !ok {
						entries[tag] = map[string][]string{}
					}
					entries[tag]["CNAME"] = append(entries[tag]["CNAME"], fmt.Sprintf("%s.%s.", hostname, t.zone))
				}
			}
		}
	}

	t.mu.Lock()
	t.entries = entries
	t.mu.Unlock()
	clog.Debugf("updated %d Tailscale entries", len(entries))
}





