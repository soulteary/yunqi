// routing-state-init provisions an empty Redis routing namespace while all
// gateways in that realm are stopped. It never resets an existing epoch.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/soulteary/yunqi/internal/redisconn"
)

func main() {
	if os.Getenv("YUNQI_CONFIRM_EMPTY_ROUTING_REALM") != "yes" {
		fmt.Fprintln(os.Stderr, "requires YUNQI_CONFIRM_EMPTY_ROUTING_REALM=yes; stop every gateway in the realm first")
		os.Exit(2)
	}
	namespace := os.Getenv("PROXY_REDIS_NAMESPACE")
	if namespace == "" {
		namespace = "yunqi"
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9:_-]{1,64}$`).MatchString(namespace) {
		fmt.Fprintln(os.Stderr, "invalid routing namespace")
		os.Exit(2)
	}
	client, err := redisconn.New(os.Getenv("PROXY_REDIS_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid Redis configuration")
		os.Exit(1)
	}
	defer client.Close()
	var nonce [32]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		os.Exit(1)
	}
	epoch := hex.EncodeToString(nonce[:])
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// Refuse any pre-existing key in this namespace, not only an epoch marker.
	ok, err := client.Eval(ctx, `local matches=redis.call('KEYS',ARGV[1]);if #matches>0 then return 0 end;redis.call('SET',KEYS[1],ARGV[2],'NX');return 1`, []string{namespace + ":{routing}:epoch"}, namespace+":{routing}:*", epoch).Int()
	if err != nil || ok != 1 {
		fmt.Fprintln(os.Stderr, "routing namespace unavailable or nonempty; refusing initialization")
		os.Exit(1)
	}
	fmt.Println("PROXY_REDIS_ROUTING_EPOCH=" + epoch)
}
