package torrent

import (
	"math/rand"
	"sort"
	"time"
)

const (
	// chokeReviewInterval is the duration between choking algorithm reviews.
	chokeReviewInterval = 10 * time.Second
	// optimisticUnchokeInterval is the duration between optimistic unchoke reviews.
	optimisticUnchokeInterval = 30 * time.Second
	// numRegularUnchokes is the number of peers to unchoke based on rate.
	numRegularUnchokes = 4
)

// choker manages the choking and unchoking logic for a torrent.
type choker struct {
	t                      *Torrent
	optimisticallyUnchoked *PeerConn
	stop                   chan struct{}
}

// newChoker creates a new choker for the given torrent.
func newChoker(t *Torrent) *choker {
	return &choker{
		t:    t,
		stop: make(chan struct{}),
	}
}

// start begins the choking review loop.
func (c *choker) start() {
	reviewTicker := time.NewTicker(chokeReviewInterval)
	optimisticTicker := time.NewTicker(optimisticUnchokeInterval)
	defer reviewTicker.Stop()
	defer optimisticTicker.Stop()

	for {
		select {
		case <-c.stop:
			return
		case <-reviewTicker.C:
			c.reviewUnchokes()
		case <-optimisticTicker.C:
			c.reviewOptimisticUnchoke()
		}
	}
}

// stopChoker gracefully stops the choker's loop.
func (c *choker) stopChoker() {
	close(c.stop)
}

// byRate implements sort.Interface for []*PeerConn to sort by rate.
type byRate struct {
	peers     []*PeerConn
	isSeeding bool
}

func (a byRate) Len() int      { return len(a.peers) }
func (a byRate) Swap(i, j int) { a.peers[i], a.peers[j] = a.peers[j], a.peers[i] }
func (a byRate) Less(i, j int) bool {
	// If seeding, we prefer peers that we upload to faster.
	if a.isSeeding {
		return a.peers[i].uploadRate > a.peers[j].uploadRate
	}
	// If downloading, we prefer peers that give us data faster.
	return a.peers[i].downloadRate > a.peers[j].downloadRate
}

// reviewUnchokes is the main choking algorithm logic, called every 10 seconds.
func (c *choker) reviewUnchokes() {
	// First, calculate rates for all peers for the last interval.
	peers := c.t.getPeers()
	for _, p := range peers {
		p.CalculateRates(chokeReviewInterval)
	}

	// Find candidates for unchoking. These are peers interested in our data.
	var candidates []*PeerConn
	for _, p := range peers {
		p.mu.RLock()
		interested := p.state.PeerInterested
		p.mu.RUnlock()

		if interested {
			candidates = append(candidates, p)
		}
	}

	if len(candidates) == 0 {
		return
	}

	// Sort candidates by rate.
	isSeeding := c.t.pieceManager.IsComplete()
	sort.Sort(byRate{peers: candidates, isSeeding: isSeeding})

	// Unchoke the top N peers and choke the rest.
	newlyUnchoked := make(map[*PeerConn]bool)
	for i := 0; i < len(candidates) && i < numRegularUnchokes; i++ {
		p := candidates[i]
		c.t.unchokePeer(p)
		newlyUnchoked[p] = true
	}

	// Choke any peer that was previously unchoked (but isn't the optimistic one)
	// and is not in our new top N list.
	for _, p := range peers {
		if _, isNewlyUnchoked := newlyUnchoked[p]; !isNewlyUnchoked && p != c.optimisticallyUnchoked {
			c.t.chokePeer(p)
		}
	}
}

// reviewOptimisticUnchoke is called every 30 seconds to unchoke a random peer.
func (c *choker) reviewOptimisticUnchoke() {
	peers := c.t.getPeers()

	// Choke the previously optimistically unchoked peer, if any.
	if c.optimisticallyUnchoked != nil {
		c.t.chokePeer(c.optimisticallyUnchoked)
		c.optimisticallyUnchoked = nil
	}

	// Find candidates: interested peers that we are currently choking.
	var candidates []*PeerConn
	for _, p := range peers {
		p.mu.RLock()
		interested := p.state.PeerInterested
		choking := p.state.AmChoking
		p.mu.RUnlock()

		if interested && choking {
			candidates = append(candidates, p)
		}
	}

	if len(candidates) == 0 {
		return
	}

	// Pick one at random and unchoke it.
	c.optimisticallyUnchoked = candidates[rand.Intn(len(candidates))]
	c.t.unchokePeer(c.optimisticallyUnchoked)
}
