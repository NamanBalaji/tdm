package torrent

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math/big"
	"net"
	"sort"
	"sync"
	"time"
)

const (
	k          = 8   // K-bucket size
	nodeIDBits = 160 // SHA1-based node IDs
)

// Node represents a single node in the DHT network.
type Node struct {
	ID   [20]byte
	Addr *net.UDPAddr
	// lastSeen is used to check for node freshness.
	lastSeen time.Time
}

// newID generates a new random 20-byte Id using a cryptographically secure source.
func newID() ([20]byte, error) {
	var id [20]byte

	_, err := rand.Read(id[:])

	return id, err
}

// distance calculates the XOR metric between two node IDs.
func distance(id1, id2 [20]byte) *big.Int {
	n1 := new(big.Int).SetBytes(id1[:])
	n2 := new(big.Int).SetBytes(id2[:])

	return n1.Xor(n1, n2)
}

// routingTable manages the k-buckets of known DHT nodes.
type routingTable struct {
	nodeID  [20]byte
	buckets [nodeIDBits][]*Node
	mu      sync.RWMutex
	dht     *DHT // Reference to the parent DHT to send pings
}

// newRoutingTable creates and initializes a new routing table.
func newRoutingTable(id [20]byte, dht *DHT) *routingTable {
	rt := &routingTable{
		nodeID: id,
		dht:    dht,
	}
	for i := range nodeIDBits {
		rt.buckets[i] = make([]*Node, 0, k)
	}

	return rt
}

// Add inserts a new node into the appropriate k-bucket.
func (rt *routingTable) Add(node *Node) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	if node.ID == rt.nodeID {
		return
	}

	bucketIndex := rt.getBucketIndex(node.ID)
	bucket := rt.buckets[bucketIndex]

	// Check if the node is already in the bucket.
	for i, existingNode := range bucket {
		if existingNode.ID == node.ID {
			// Move the seen node to the front of the bucket (most recently seen).
			bucket = append(bucket[:i], bucket[i+1:]...)
			rt.buckets[bucketIndex] = append([]*Node{node}, bucket...)
			node.lastSeen = time.Now()

			return
		}
	}

	// If the bucket has space, add the new node to the front.
	if len(bucket) < k {
		rt.buckets[bucketIndex] = append([]*Node{node}, bucket...)
		node.lastSeen = time.Now()

		return
	}

	// If the bucket is full, ping the least-recently seen node (the one at the end).
	oldestNode := bucket[len(bucket)-1]
	// To prevent spamming, only ping if it hasn't been seen recently.
	if time.Since(oldestNode.lastSeen) > 5*time.Minute {
		rt.dht.ping(oldestNode.Addr, func(err error) {
			if err != nil {
				// Oldest node did not respond, replace it.
				rt.mu.Lock()
				// Re-check the bucket state in case it changed.
				b := rt.buckets[bucketIndex]
				if len(b) > 0 && b[len(b)-1].ID == oldestNode.ID {
					// Replace the oldest node with the new one
					b[len(b)-1] = node
				}

				rt.mu.Unlock()
			}
		})
	}
}

// findClosest returns a list of k nodes closest to the given target Id.
func (rt *routingTable) findClosest(targetID [20]byte, count int) []*Node {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	var candidates byDistance

	candidates.target = targetID

	for _, bucket := range rt.buckets {
		candidates.nodes = append(candidates.nodes, bucket...)
	}

	sort.Sort(candidates)

	if count > len(candidates.nodes) {
		count = len(candidates.nodes)
	}

	return candidates.nodes[:count]
}

// getBucketIndex calculates the appropriate k-bucket index for a node Id.
func (rt *routingTable) getBucketIndex(id [20]byte) int {
	dist := distance(rt.nodeID, id)

	l := dist.BitLen()
	if l == 0 {
		return 0
	}
	// The bit length of the distance gives us the bucket index.
	// (e.g., distance in range [2^i, 2^(i+1)-1] goes in bucket i)
	return l - 1
}

// byDistance is a helper type for sorting nodes by their XOR distance to a target.
type byDistance struct {
	target [20]byte
	nodes  []*Node
}

func (b byDistance) Len() int      { return len(b.nodes) }
func (b byDistance) Swap(i, j int) { b.nodes[i], b.nodes[j] = b.nodes[j], b.nodes[i] }
func (b byDistance) Less(i, j int) bool {
	dist1 := distance(b.target, b.nodes[i].ID)
	dist2 := distance(b.target, b.nodes[j].ID)

	return dist1.Cmp(dist2) == -1
}

// compactNodes encodes a list of nodes into the compact node info format.
func compactNodes(nodes []*Node) []byte {
	buf := make([]byte, 0, len(nodes)*26)
	for _, n := range nodes {
		if n.Addr.IP.To4() == nil {
			continue // Skip IPv6 for now
		}

		buf = append(buf, n.ID[:]...)
		buf = append(buf, n.Addr.IP.To4()...)
		buf = binary.BigEndian.AppendUint16(buf, uint16(n.Addr.Port))
	}

	return buf
}

// decodeCompactNodes decodes a compact node info string into a list of nodes.
func decodeCompactNodes(s []byte) ([]*Node, error) {
	if len(s)%26 != 0 {
		return nil, fmt.Errorf("compact node info has invalid length %d", len(s))
	}

	var nodes []*Node

	for i := 0; i < len(s); i += 26 {
		var id [20]byte
		copy(id[:], s[i:i+20])
		ip := net.IP(s[i+20 : i+24])
		port := binary.BigEndian.Uint16(s[i+24 : i+26])
		nodes = append(nodes, &Node{
			ID:   id,
			Addr: &net.UDPAddr{IP: ip, Port: int(port)},
		})
	}

	return nodes, nil
}
