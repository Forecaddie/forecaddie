// Package transport defines the interface between the coordinator and the
// overlay network that carries traffic between Caddy nodes and app hosts.
//
// NetBird is the only driver shipped. The policy operation is declarative so
// that a driver for a system with a single policy document (Tailscale,
// Headscale) can diff rather than create and delete individual objects.
// Nothing driver-specific leaks into the registry: the coordinator stores an
// opaque Ref per peer and addresses everything else by group name.
package transport

import (
	"context"
	"encoding/json"
	"net/netip"
	"time"
)

// Peer is a host enrolled in the overlay.
type Peer struct {
	Ref       json.RawMessage // driver-specific identifiers
	Hostname  string
	OverlayIP netip.Addr
	Groups    []string
	Connected bool
	LastSeen  time.Time
	Approved  bool
}

// Rule allows SrcGroup to reach DstGroup on Proto/Port. Groups managed by
// the coordinator use the prefixes "caddy-", "host-" and the name
// "coordinator".
type Rule struct {
	SrcGroup string
	DstGroup string
	Proto    string // "tcp" | "udp"
	Port     uint16
}

// Transport is implemented by each overlay driver.
type Transport interface {
	ListPeers(ctx context.Context) ([]Peer, error)
	ApprovePeer(ctx context.Context, ref json.RawMessage) error
	RemovePeer(ctx context.Context, ref json.RawMessage) error

	EnsureGroup(ctx context.Context, name string) error
	DeleteGroup(ctx context.Context, name string) error

	// MintEnrollmentKey returns a single-use key that enrolls a new peer
	// into exactly the given groups. The secret is shown once and never
	// stored by the coordinator.
	MintEnrollmentKey(ctx context.Context, groups []string, ttl time.Duration) (secret string, ref json.RawMessage, err error)

	// SetDesiredPolicy makes the overlay allow exactly rules among
	// coordinator-managed groups and nothing else. The driver computes
	// the diff against current state.
	SetDesiredPolicy(ctx context.Context, rules []Rule) error

	// DefaultOpen reports whether the overlay currently allows traffic
	// beyond the desired set, such as NetBird's default All->All policy.
	// The coordinator refuses to approve anything while this is true.
	DefaultOpen(ctx context.Context) (bool, error)

	// InterfaceName is the overlay interface on peers (wt0, tailscale0).
	InterfaceName() string
	// JoinCommand is the argv a new host runs to enroll with key.
	JoinCommand(key string) []string
}
