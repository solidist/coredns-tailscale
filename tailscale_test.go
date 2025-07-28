package tailscale

import (
	"net/netip"
	"testing"

	"github.com/google/go-cmp/cmp"
	"tailscale.com/tailcfg"
	"tailscale.com/types/netmap"
)

func TestProcessNetMap(t *testing.T) {
	ts := &Tailscale{zone: "example.com"}

	self := (&tailcfg.Node{
		ComputedName: "self",
		Addresses: []netip.Prefix{
			netip.MustParsePrefix("100.64.0.1/32"),
			netip.MustParsePrefix("fd7a:115c:a1e0::1/128"),
		},
		Tags: []string{"tag:cname-app"},
	}).View()

	nm := &netmap.NetworkMap{
		SelfNode: self,
		Peers: []tailcfg.NodeView{
			(&tailcfg.Node{
				ComputedName: "peer",
				Addresses: []netip.Prefix{
					netip.MustParsePrefix("100.64.0.2/32"),
					netip.MustParsePrefix("fd7a:115c:a1e0::2/128"),
				},
				Tags: []string{"tag:cname-app"},
			}).View(),
			(&tailcfg.Node{
				// shared node should be excluded
				ComputedName: "shared",
				Sharer:       1,
				Addresses: []netip.Prefix{
					netip.MustParsePrefix("100.64.0.3/32"),
					netip.MustParsePrefix("fd7a:115c:a1e0::3/128"),
				},
				Tags: []string{"tag:cname-app"},
			}).View(),
			(&tailcfg.Node{
				// mullvad exit node should be excluded
				ComputedName:    "mullvad",
				IsWireGuardOnly: true,
				Addresses: []netip.Prefix{
					netip.MustParsePrefix("100.64.0.4/32"),
					netip.MustParsePrefix("fd7a:115c:a1e0::4/128"),
				},
				Tags: []string{"tag:cname-app"},
			}).View(),
		},
	}

	want := map[string]map[string][]string{
		"self": {
			"A":    {"100.64.0.1"},
			"AAAA": {"fd7a:115c:a1e0::1"},
		},
		"peer": {
			"A":    {"100.64.0.2"},
			"AAAA": {"fd7a:115c:a1e0::2"},
		},
		"app": {
			"CNAME": {"self.example.com.", "peer.example.com."},
		},
	}

	ts.processNetMap(nm)
	if !cmp.Equal(ts.entries, want) {
		t.Errorf("ts.entries = %v, want %v", ts.entries, want)
	}

	// now process another netmap with only self, and make sure peer is removed
	ts.processNetMap(&netmap.NetworkMap{SelfNode: self})
	want = map[string]map[string][]string{
		"self": {
			"A":    {"100.64.0.1"},
			"AAAA": {"fd7a:115c:a1e0::1"},
		},
		"app": {
			"CNAME": {"self.example.com."},
		},
	}
	if !cmp.Equal(ts.entries, want) {
		t.Errorf("ts.entries = %v, want %v", ts.entries, want)
	}
}

func TestProcessNetMapHostnameGrouping(t *testing.T) {
	ts := &Tailscale{zone: "example.com"}

	// Test hostname grouping feature with numbered hostnames
	web1 := (&tailcfg.Node{
		ComputedName: "web-1",
		Addresses: []netip.Prefix{
			netip.MustParsePrefix("100.64.0.10/32"),
			netip.MustParsePrefix("fd7a:115c:a1e0::10/128"),
		},
	}).View()

	web2 := (&tailcfg.Node{
		ComputedName: "web-2",
		Addresses: []netip.Prefix{
			netip.MustParsePrefix("100.64.0.11/32"),
			netip.MustParsePrefix("fd7a:115c:a1e0::11/128"),
		},
	}).View()

	web3 := (&tailcfg.Node{
		ComputedName: "web-3",
		Addresses: []netip.Prefix{
			netip.MustParsePrefix("100.64.0.12/32"),
			netip.MustParsePrefix("fd7a:115c:a1e0::12/128"),
		},
	}).View()

	nm := &netmap.NetworkMap{
		SelfNode: web1,
		Peers: []tailcfg.NodeView{web2, web3},
	}

	want := map[string]map[string][]string{
		"web-1": {
			"A":    {"100.64.0.10"},
			"AAAA": {"fd7a:115c:a1e0::10"},
		},
		"web-2": {
			"A":    {"100.64.0.11"},
			"AAAA": {"fd7a:115c:a1e0::11"},
		},
		"web-3": {
			"A":    {"100.64.0.12"},
			"AAAA": {"fd7a:115c:a1e0::12"},
		},
		// Grouped hostname should contain all IPs for round-robin DNS
		"web": {
			"A":    {"100.64.0.10", "100.64.0.11", "100.64.0.12"},
			"AAAA": {"fd7a:115c:a1e0::10", "fd7a:115c:a1e0::11", "fd7a:115c:a1e0::12"},
		},
	}

	ts.processNetMap(nm)
	if !cmp.Equal(ts.entries, want) {
		t.Errorf("ts.entries = %v, want %v", ts.entries, want)
	}
}
