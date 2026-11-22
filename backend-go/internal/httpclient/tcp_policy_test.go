package httpclient

import (
	"context"
	"net"
	"testing"
)

func TestOutboundTCPPolicyPreservesPrivateDefaultAndChecksRestrictions(t *testing.T) {
	for _, key := range []string{"YUNQI_OUTBOUND_ALLOWED_HOSTS", "YUNQI_OUTBOUND_ALLOWED_PORTS", "YUNQI_OUTBOUND_BLOCK_PRIVATE", "YUNQI_OUTBOUND_PRIVATE_CIDRS"} {
		t.Setenv(key, "")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	addr := listener.Addr().String()
	conn, err := DialOutboundTCPContext(context.Background(), "tcp", addr)
	if err != nil {
		t.Fatal("private Redis compatibility changed", err)
	}
	_ = conn.Close()
	t.Setenv("YUNQI_OUTBOUND_BLOCK_PRIVATE", "true")
	if err := ValidateOutboundTCPAddress(context.Background(), "tcp", addr); err == nil {
		t.Fatal("private restriction bypassed")
	}
	t.Setenv("YUNQI_OUTBOUND_PRIVATE_CIDRS", "127.0.0.1/32")
	conn, err = DialOutboundTCPContext(context.Background(), "tcp", addr)
	if err != nil {
		t.Fatal("explicit private CIDR not honored", err)
	}
	_ = conn.Close()
	t.Setenv("YUNQI_OUTBOUND_ALLOWED_HOSTS", "different.invalid")
	if err := ValidateOutboundTCPAddress(context.Background(), "tcp", addr); err == nil {
		t.Fatal("host allowlist bypassed")
	}
	t.Setenv("YUNQI_OUTBOUND_ALLOWED_HOSTS", "")
	t.Setenv("YUNQI_OUTBOUND_ALLOWED_PORTS", "443")
	if err := ValidateOutboundTCPAddress(context.Background(), "tcp", addr); err == nil {
		t.Fatal("port allowlist bypassed")
	}
}
func TestOutboundTCPPolicyBlocksMetadataAndServiceNamePorts(t *testing.T) {
	for _, addr := range []string{"169.254.169.254:6379", "[fd00:ec2::254]:6379", "metadata.google.internal:6379", "localhost:http", "localhost:0", "localhost:65536"} {
		if err := ValidateOutboundTCPAddress(context.Background(), "tcp", addr); err == nil {
			t.Fatalf("unsafe TCP address permitted: %s", addr)
		}
	}
	if err := ValidateOutboundTCPAddress(context.Background(), "unix", "/private/tmp/redis.sock"); err == nil {
		t.Fatal("TCP guard accepted a filesystem socket")
	}
}
