package plugin

import (
	"net"
	"testing"
)

func TestCheckIPRejectsPrivateAndReserved(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "10.1.2.3", "192.168.1.1", "172.16.0.9",
		"169.254.1.1", "0.0.0.0", "224.0.0.1", "::1", "fe80::1", "fc00::1",
	}
	for _, s := range blocked {
		if err := checkIP(net.ParseIP(s)); err == nil {
			t.Errorf("expect %q blocked", s)
		}
	}
}

func TestCheckIPAllowsPublic(t *testing.T) {
	allowed := []string{"8.8.8.8", "1.1.1.1", "2606:4700::1111"}
	for _, s := range allowed {
		if err := checkIP(net.ParseIP(s)); err != nil {
			t.Errorf("expect %q allowed, got %v", s, err)
		}
	}
}
