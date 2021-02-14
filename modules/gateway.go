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
	// These peers have been verified to be v1.3.7 or higher
	BootstrapPeers = build.Select(build.Var{
		Standard: []NetAddress{
			"95.78.166.67:8481",
			"68.199.121.249:8481",
			"24.194.148.158:8481",
			"82.231.193.206:8481",
			"185.216.208.214:8481",
			"165.73.59.75:8481",
			"81.5.154.29:8481",
			"68.133.15.97:8481",
			"223.19.102.54:8481",
			"136.52.23.122:8481",
			"45.56.21.129:8481",
			"109.172.42.157:8481",
			"188.244.40.69:9985",
			"176.37.126.147:8481",
			"68.96.80.134:8481",
			"92.255.195.111:8481",
			"88.202.201.30:8481",
			"76.103.83.241:8481",
			"77.132.24.85:8481",
			"81.167.50.168:8481",
			"91.206.15.126:8481",
			"91.231.94.22:8481",
			"212.105.168.207:8481",
			"94.113.86.207:8481",
			"188.242.52.10:8481",
			"94.137.140.40:8481",
			"137.74.1.200:8481",
			"85.27.163.135:8481",
			"46.246.68.66:8481",
			"92.70.88.30:8481",
			"188.68.37.232:8481",
			"153.210.37.241:8481",
			"24.20.240.181:8481",
			"92.154.126.211:8481",
			"45.50.26.222:8481",
			"41.160.218.190:8481",
			"23.175.0.151:8481",
			"109.248.206.13:8481",
			"222.161.26.222:8481",
			"68.97.208.223:8481",
			"71.190.208.128:8481",
			"69.120.2.164:8481",
			"37.204.141.163:8481",
			"188.243.111.129:8481",
			"78.46.64.86:8481",
			"188.244.40.69:8481",
			"87.237.42.180:8481",
			"212.42.213.179:8481",
			"62.216.59.236:8481",
			"80.56.227.209:8481",
			"202.181.196.157:8481",
			"188.242.52.10:9986",
			"188.242.52.10:9988",
			"81.24.30.12:8481",
			"109.233.59.68:8481",
			"77.162.159.137:8481",
			"176.240.111.223:8481",
			"126.28.73.206:8481",
			"178.63.11.62:8481",
			"174.84.49.170:8481",
			"185.6.124.16:8481",
			"81.24.30.13:8481",
			"31.208.123.118:8481",
			"85.69.198.249:8481",
			"5.9.147.103:8481",
			"77.168.231.70:8481",
			"81.24.30.14:8481",
			"82.253.237.216:8481",
			"161.53.40.130:8481",
			"34.209.55.245:8481",
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
