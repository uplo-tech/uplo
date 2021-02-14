package hosttree

import (
	"sort"
	"sync"

	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/fastrand"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
)

var (
	// ErrHostExists is returned if an Insert is called with a public key that
	// already exists in the tree.
	ErrHostExists = errors.New("host already exists in the tree")

	// ErrNoSuchHost is returned if Remove is called with a public key that does
	// not exist in the tree.
	ErrNoSuchHost = errors.New("no host with specified public key")
)

type (
	// WeightFunc is a function used to weight a given HostDBEntry in the tree.
	WeightFunc func(modules.HostDBEntry) ScoreBreakdown

	// HostTree is used to store and select host database entries. Each HostTree
	// is initialized with a weighting func that is able to assign a weight to
	// each entry. The entries can then be selected at random, weighted by the
	// weight func.
	HostTree struct {
		root *node

		// hosts is a map of public keys to nodes.
		hosts map[string]*node

		// resolver is the Resolver that is used by the hosttree to resolve
		// hostnames to IP addresses.
		resolver modules.Resolver

		// weightFn calculates the weight of a hostEntry
		weightFn WeightFunc

		mu sync.Mutex
	}

	// hostEntry is an entry in the host tree.
	hostEntry struct {
		modules.HostDBEntry
		weight types.Currency
	}

	// node is a node in the tree.
	node struct {
		parent *node
		left   *node
		right  *node

		count int  // cumulative count of this node and all children
		taken bool // `taken` indicates whether there is an active host at this node or not.

		weight types.Currency
		entry  *hostEntry
	}
)

// createNode creates a new node using the provided `parent` and `entry`.
func createNode(parent *node, entry *hostEntry) *node {
	return &node{
		parent: parent,
		weight: entry.weight,
		count:  1,

		taken: true,
		entry: entry,
	}
}

// New creates a new HostTree given a weight function and a resolver
// for hostnames.
func New(wf WeightFunc, resolver modules.Resolver) *HostTree {
	return &HostTree{
		hosts: make(map[string]*node),
		root: &node{
			count: 1,
		},
		resolver: resolver,
		weightFn: wf,
	}
}

// recursiveInsert inserts an entry into the appropriate place in the tree. The
// running time of recursiveInsert is log(n) in the maximum number of elements
// that have ever been in the tree.
func (n *node) recursiveInsert(entry *hostEntry) (nodesAdded int, newnode *node) {
	// If there is no parent and no children, and the node is not taken, assign
	// this entry to this node.
	if n.parent == nil && n.left == nil && n.right == nil && !n.taken {
		n.entry = entry
		n.taken = true
		n.weight = entry.weight
		newnode = n
		return
	}

	n.weight = n.weight.Add(entry.weight)

	// If the current node is empty, add the entry but don't increase the
	// count.
	if !n.taken {
		n.taken = true
		n.entry = entry
		newnode = n
		return
	}

	// Insert the element into the lest populated side.
	if n.left == nil {
		n.left = createNode(n, entry)
		nodesAdded = 1
		newnode = n.left
	} else if n.right == nil {
		n.right = createNode(n, entry)
		nodesAdded = 1
		newnode = n.right
	} else if n.left.count <= n.right.count {
		nodesAdded, newnode = n.left.recursiveInsert(entry)
	} else {
		nodesAdded, newnode = n.right.recursiveInsert(entry)
	}

	n.count += nodesAdded
	return
}

// nodeAtWeight grabs an element in the tree that appears at the given weight.
// Though the tree has an arbitrary sorting, a sufficiently random weight will
// pull a random element. The tree is searched through in a post-ordered way.
func (n *node) nodeAtWeight(weight types.Currency) *node {
	// Sanity check - weight must be less than the total weight of the tree.
	if weight.Cmp(n.weight) > 0 {
		build.Critical("Node weight corruption")
		return nil
	}

	// Check if the left or right child should be returned.
	if n.left != nil {
		if weight.Cmp(n.left.weight) < 0 {
			return n.left.nodeAtWeight(weight)
		}
		weight = weight.Sub(n.left.weight) // Search from the 0th index of the right side.
	}
	if n.right != nil && weight.Cmp(n.right.weight) < 0 {
		return n.right.nodeAtWeight(weight)
	}

	// Should we panic here instead?
	if !n.taken {
		build.Critical("Node tree structure corruption")
		return nil
	}

	// Return the root entry.
	return n
}

// remove takes a node and removes it from the tree by climbing through the
// list of parents. remove does not delete nodes.
func (n *node) remove() {
	n.weight = n.weight.Sub(n.entry.weight)
	n.taken = false
	current := n.parent
	for current != nil {
		current.weight = current.weight.Sub(n.entry.weight)
		current = current.parent
	}
}

// Host returns the address of the HostEntry.
func (he *hostEntry) Host() string {
	return he.NetAddress.Host()
}

// All returns all of the hosts in the host tree, sorted by weight.
func (ht *HostTree) All() []modules.HostDBEntry {
	ht.mu.Lock()
	defer ht.mu.Unlock()
	return ht.all()
}

// Insert inserts the entry provided to `entry` into the host tree. Insert will
// return an error if the input host already exists.
func (ht *HostTree) Insert(hdbe modules.HostDBEntry) error {
	ht.mu.Lock()
	defer ht.mu.Unlock()
	return ht.insert(hdbe)
}

// Remove removes the host with the public key provided by `pk`.
func (ht *HostTree) Remove(pk types.UploPublicKey) error {
	ht.mu.Lock()
	defer ht.mu.Unlock()

	node, exists := ht.hosts[pk.String()]
	if !exists {
		return ErrNoSuchHost
	}
	node.remove()
	delete(ht.hosts, pk.String())

	return nil
}

// Modify updates a host entry at the given public key, replacing the old entry
// with the entry provided by `newEntry`.
func (ht *HostTree) Modify(hdbe modules.HostDBEntry) error {
	ht.mu.Lock()
	defer ht.mu.Unlock()

	node, exists := ht.hosts[hdbe.PublicKey.String()]
	if !exists {
		return ErrNoSuchHost
	}

	node.remove()

	entry := &hostEntry{
		HostDBEntry: hdbe,
		weight:      ht.weightFn(hdbe).Score(),
	}

	_, node = ht.root.recursiveInsert(entry)

	ht.hosts[entry.PublicKey.String()] = node
	return nil
}

// SetFiltered updates a host entry filtered field.
func (ht *HostTree) SetFiltered(pubKey types.UploPublicKey, filtered bool) error {
	entry, ok := ht.Select(pubKey)
	if !ok {
		return ErrNoSuchHost
	}
	entry.Filtered = filtered
	return ht.Modify(entry)
}

// SetWeightFunction resets the HostTree and assigns it a new weight
// function. This resets the tree and reinserts all the hosts.
func (ht *HostTree) SetWeightFunction(wf WeightFunc) error {
	ht.mu.Lock()
	defer ht.mu.Unlock()

	// Get all the hosts.
	allHosts := ht.all()

	// Reset the tree
	ht.hosts = make(map[string]*node)
	ht.root = &node{
		count: 1,
	}

	// Assign the new weight function.
	ht.weightFn = wf

	// Reinsert all the hosts. To prevent the host tree from having a
	// catastrophic failure in the event of an error early on, we tally up all
	// of the insertion errors and return them all at the end.
	var insertErrs error
	for _, hdbe := range allHosts {
		if err := ht.insert(hdbe); err != nil {
			insertErrs = errors.Compose(err, insertErrs)
		}
	}
	return insertErrs
}

// Select returns the host with the provided public key, should the host exist.
func (ht *HostTree) Select(spk types.UploPublicKey) (modules.HostDBEntry, bool) {
	ht.mu.Lock()
	defer ht.mu.Unlock()

	node, exists := ht.hosts[spk.String()]
	if !exists {
		return modules.HostDBEntry{}, false
	}
	return node.entry.HostDBEntry, true
}

// SelectRandom grabs a random n hosts from the tree. There will be no repeats,
// but the length of the slice returned may be less than n, and may even be
// zero.  The hosts that are returned first have the higher priority.
//
// Hosts passed to 'blacklist' will not be considered; pass `nil` if no
// blacklist is desired. 'addressBlacklist' is similar to 'blacklist' but
// instead of not considering the hosts in the list, hosts that use the same IP
// subnet as those hosts will be ignored. In most cases those blacklists contain
// the same elements but sometimes it is useful to block a host without blocking
// its IP range.
//
// Hosts with a score of 1 will be ignored. 1 is the lowest score possible, at
// which point it's impossible to distinguish between hosts. Any sane scoring
// system should always have scores greater than 1 unless the host is
// intentionally being given a low score to indicate that the host should not be
// used.
func (ht *HostTree) SelectRandom(n int, blacklist, addressBlacklist []types.UploPublicKey) []modules.HostDBEntry {
	ht.mu.Lock()
	defer ht.mu.Unlock()

	var removedEntries []*hostEntry

	// Create a filter.
	filter := NewFilter(ht.resolver)

	// Add the hosts from the addressBlacklist to the filter.
	for _, pubkey := range addressBlacklist {
		node, exists := ht.hosts[pubkey.String()]
		if !exists {
			continue
		}
		// Add the node to the addressFilter.
		filter.Add(node.entry.NetAddress)
	}
	// Remove hosts we want to blacklist from the tree but remember them to make
	// sure we can insert them later.
	for _, pubkey := range blacklist {
		node, exists := ht.hosts[pubkey.String()]
		if !exists {
			continue
		}
		// Remove the host from the tree.
		node.remove()
		delete(ht.hosts, pubkey.String())

		// Remember the host to insert it again later.
		removedEntries = append(removedEntries, node.entry)
	}

	var hosts []modules.HostDBEntry

	for len(hosts) < n && len(ht.hosts) > 0 {
		randWeight := fastrand.BigIntn(ht.root.weight.Big())
		node := ht.root.nodeAtWeight(types.NewCurrency(randWeight))
		weightOne := types.NewCurrency64(1)

		if node.entry.AcceptingContracts &&
			len(node.entry.ScanHistory) > 0 &&
			node.entry.ScanHistory[len(node.entry.ScanHistory)-1].Success &&
			!filter.Filtered(node.entry.NetAddress) &&
			node.entry.weight.Cmp(weightOne) > 0 {
			// The host must be online and accepting contracts to be returned
			// by the random function. It also has to pass the addressFilter
			// check.
			hosts = append(hosts, node.entry.HostDBEntry)

			// If the host passed the filter, we add it to the filter.
			filter.Add(node.entry.NetAddress)
		}

		removedEntries = append(removedEntries, node.entry)
		node.remove()
		delete(ht.hosts, node.entry.PublicKey.String())
	}

	for _, entry := range removedEntries {
		_, node := ht.root.recursiveInsert(entry)
		ht.hosts[entry.PublicKey.String()] = node
	}

	return hosts
}

// all returns all of the hosts in the host tree, sorted by weight.
func (ht *HostTree) all() []modules.HostDBEntry {
	he := make([]hostEntry, 0, len(ht.hosts))
	for _, node := range ht.hosts {
		he = append(he, *node.entry)
	}
	sort.Sort(byWeight(he))

	entries := make([]modules.HostDBEntry, 0, len(he))
	for _, entry := range he {
		entries = append(entries, entry.HostDBEntry)
	}
	return entries
}

// insert inserts the entry provided to `entry` into the host tree. Insert will
// return an error if the input host already exists.
func (ht *HostTree) insert(hdbe modules.HostDBEntry) error {
	entry := &hostEntry{
		HostDBEntry: hdbe,
		weight:      ht.weightFn(hdbe).Score(),
	}

	if _, exists := ht.hosts[entry.PublicKey.String()]; exists {
		return ErrHostExists
	}

	_, node := ht.root.recursiveInsert(entry)

	ht.hosts[entry.PublicKey.String()] = node
	return nil
}
