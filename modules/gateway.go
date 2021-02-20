package modules

import (
	"net"
	"time"

	"github.com/uplo-tech/uplo/build"
)

const (
	// GatewayDir is the name of the directory used to store the gateway's
	// persistent data.
	GatewayDir = "gateway"
)

var (
	// BootstrapPeers is a list of peers that can be used to find other peers -
	// when a client first connects to the network, the only options for
	// finding peers are either manual entry of peers or to use a hardcoded
	// bootstrap point. While the bootstrap point could be a central service,
	// it can also be a list of peers that are known to be stable. We have
	// chosen to hardcode known-stable peers.
	//
	// These peers have been verified to be v1.0.1 or higher
	BootstrapPeers = build.Select(build.Var{
		Standard: []NetAddress{
			"112.213.39.219:8481", //node.sydney.uplo.tech
			"203.57.115.37:8481", //node.melbourne.uplo.tech
			"203.29.241.159:8481", //node.perth.uplo.tech
			"203.57.51.219:8481", //node.brisbane.uplo.tech
		},
		Dev:     []NetAddress(nil),
		Testing: []NetAddress(nil),
	}).([]NetAddress)
)

type (
	// Peer contains all the info necessary to Broadcast to a peer.
	Peer struct {
		Inbound    bool       `json:"inbound"`
		Local      bool       `json:"local"`
		NetAddress NetAddress `json:"netaddress"`
		Version    string     `json:"version"`
	}

	// A PeerConn is the connection type used when communicating with peers during
	// an RPC. It is identical to a net.Conn with the additional RPCAddr method.
	// This method acts as an identifier for peers and is the address that the
	// peer can be dialed on. It is also the address that should be used when
	// calling an RPC on the peer.
	PeerConn interface {
		net.Conn
		RPCAddr() NetAddress
	}

	// RPCFunc is the type signature of functions that handle RPCs. It is used for
	// both the caller and the callee. RPCFuncs may perform locking. RPCFuncs may
	// close the connection early, and it is recommended that they do so to avoid
	// keeping the connection open after all necessary I/O has been performed.
	RPCFunc func(PeerConn) error

	// A Gateway facilitates the interactions between the local node and remote
	// nodes (peers). It relays incoming blocks and transactions to local modules,
	// and broadcasts outgoing blocks and transactions to peers. In a broad sense,
	// it is responsible for ensuring that the local consensus set is consistent
	// with the "network" consensus set.
	Gateway interface {
		Alerter

		// BandwidthCounters returns the Gateway's upload and download bandwidth
		BandwidthCounters() (uint64, uint64, time.Time, error)

		// Connect establishes a persistent connection to a peer.
		Connect(NetAddress) error

		// ConnectManual is a Connect wrapper for a user-initiated Connect
		ConnectManual(NetAddress) error

		// Disconnect terminates a connection to a peer.
		Disconnect(NetAddress) error

		// DiscoverAddress discovers and returns the current public IP address
		// of the gateway. Contrary to Address, DiscoverAddress is blocking and
		// might take multiple minutes to return. A channel to cancel the
		// discovery can be supplied optionally.
		DiscoverAddress(cancel <-chan struct{}) (net.IP, error)

		// ForwardPort adds a port mapping to the router. It will block until
		// the mapping is established or until it is interrupted by a shutdown.
		ForwardPort(port string) error

		// DisconnectManual is a Disconnect wrapper for a user-initiated
		// disconnect
		DisconnectManual(NetAddress) error

		// AddToBlocklist adds addresses to the blocklist of the gateway
		AddToBlocklist(addresses []string) error

		// Blocklist returns the current blocklist of the Gateway
		Blocklist() ([]string, error)

		// RemoveFromBlocklist removes addresses from the blocklist of the
		// gateway
		RemoveFromBlocklist(addresses []string) error

		// SetBlocklist sets the blocklist of the gateway
		SetBlocklist(addresses []string) error

		// Address returns the Gateway's address.
		Address() NetAddress

		// Peers returns the addresses that the Gateway is currently connected
		// to.
		Peers() []Peer

		// RegisterRPC registers a function to handle incoming connections that
		// supply the given RPC ID.
		RegisterRPC(string, RPCFunc)

		// RateLimits returns the currently set bandwidth limits of the gateway.
		RateLimits() (int64, int64)

		// SetRateLimits changes the rate limits for the peer-connections of the
		// gateway.
		SetRateLimits(downloadSpeed, uploadSpeed int64) error

		// UnregisterRPC unregisters an RPC and removes all references to the
		// RPCFunc supplied in the corresponding RegisterRPC call. References to
		// RPCFuncs registered with RegisterConnectCall are not removed and
		// should be removed with UnregisterConnectCall. If the RPC does not
		// exist no action is taken.
		UnregisterRPC(string)

		// RegisterConnectCall registers an RPC name and function to be called
		// upon connecting to a peer.
		RegisterConnectCall(string, RPCFunc)

		// UnregisterConnectCall unregisters an RPC and removes all references to the
		// RPCFunc supplied in the corresponding RegisterConnectCall call. References
		// to RPCFuncs registered with RegisterRPC are not removed and should be
		// removed with UnregisterRPC. If the RPC does not exist no action is taken.
		UnregisterConnectCall(string)

		// RPC calls an RPC on the given address. RPC cannot be called on an
		// address that the Gateway is not connected to.
		RPC(NetAddress, string, RPCFunc) error

		// Broadcast transmits obj, prefaced by the RPC name, to all of the
		// given peers in parallel.
		Broadcast(name string, obj interface{}, peers []Peer)

		// Online returns true if the gateway is connected to remote hosts
		Online() bool

		// Close safely stops the Gateway's listener process.
		Close() error
	}
)
