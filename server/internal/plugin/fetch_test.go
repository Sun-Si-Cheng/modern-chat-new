package plugin

import (
	"context"
	"net"
	"testing"
)

func TestCheckIPRejectsPrivateAndReserved(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "10.1.2.3", "192.168.1.1", "172.16.0.9",
		"169.254.1.1", "0.0.0.0", "224.0.0.1", "::1", "fe80::1", "fc00::1",
		// 新增覆盖：CGNAT、基准测试/文档/保留网段、NAT64 与 IPv4-mapped
		"100.64.0.1", "198.18.0.1", "240.0.0.1", "255.255.255.255",
		"64:ff9b::a00:1", "::ffff:127.0.0.1", "::ffff:10.0.0.1",
	}
	for _, s := range blocked {
		if err := checkIP(net.ParseIP(s)); err == nil {
			t.Errorf("expect %q blocked", s)
		}
	}
}

// IP 字面量在拨号前就要被拒绝，不能等到连接阶段。
func TestResolvePublicIPRejectsPrivateLiteral(t *testing.T) {
	if _, err := resolvePublicIP(context.Background(), "127.0.0.1"); err == nil {
		t.Fatal("private IP literal should be rejected")
	}
	if _, err := resolvePublicIP(context.Background(), "100.64.0.1"); err == nil {
		t.Fatal("CGNAT literal should be rejected")
	}
	ip, err := resolvePublicIP(context.Background(), "8.8.8.8")
	if err != nil || ip.String() != "8.8.8.8" {
		t.Fatalf("public literal: ip=%v err=%v", ip, err)
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
