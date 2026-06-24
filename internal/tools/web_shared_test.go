package tools

import "testing"

func TestIsPrivateIP(t *testing.T) {
	blocked := []struct {
		ip   string
		desc string
	}{
		{"10.0.0.1", "RFC 1918 class A"},
		{"172.16.5.1", "RFC 1918 class B"},
		{"192.168.1.1", "RFC 1918 class C"},
		{"127.0.0.1", "loopback"},
		{"169.254.169.254", "link-local / cloud metadata"},
		{"100.64.0.1", "carrier-grade NAT"},
		{"198.18.1.1", "benchmarking (RFC 2544)"},
		{"198.19.255.1", "benchmarking upper (RFC 2544)"},
		{"240.1.2.3", "reserved for future use"},
		{"255.255.255.255", "reserved broadcast"},
		{"::1", "IPv6 loopback"},
		{"fe80::1", "IPv6 link-local"},
		{"fc00::1", "IPv6 unique local"},
	}
	for _, tc := range blocked {
		t.Run(tc.desc, func(t *testing.T) {
			if !isPrivateIP(tc.ip) {
				t.Errorf("expected %s (%s) to be blocked", tc.ip, tc.desc)
			}
		})
	}

	allowed := []struct {
		ip   string
		desc string
	}{
		{"8.8.8.8", "Google DNS"},
		{"93.184.216.34", "example.com"},
		{"198.51.100.1", "public IP near benchmarking range"},
	}
	for _, tc := range allowed {
		t.Run(tc.desc, func(t *testing.T) {
			if isPrivateIP(tc.ip) {
				t.Errorf("expected %s (%s) to be allowed", tc.ip, tc.desc)
			}
		})
	}
}
