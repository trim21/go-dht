package dht

import (
	"container/ring"

	lru "github.com/hashicorp/golang-lru/v2"
)

// For the inner map, the key address in binary form. value=ignored.
type peerContactsSet struct {
	set map[string]bool
	// Needed to ensure different peers are returned each time.
	ring *ring.Ring
}

// next returns up to 8 peer contacts, if available. Further calls will return a
// different set of contacts, if possible.
func (p *peerContactsSet) next() []string {
	count := kNodes
	if count > len(p.set) {
		count = len(p.set)
	}
	x := make([]string, 0, count)
	xx := make(map[string]bool) //maps are easier to dedupe
	for range p.set {
		nid := p.ring.Move(1).Value.(string)
		if _, ok := xx[nid]; p.set[nid] && !ok {
			xx[nid] = true
		}
		if len(xx) >= count {
			break
		}
	}

	if len(xx) < count {
		for range p.set {
			nid := p.ring.Move(1).Value.(string)
			if _, ok := xx[nid]; ok {
				continue
			}
			xx[nid] = true
			if len(xx) >= count {
				break
			}
		}
	}
	for id := range xx {
		x = append(x, id)
	}
	return x
}

// put adds a peerContact to an infohash contacts set. peerContact must be a binary encoded contact
// address where the first four bytes form the IP and the last byte is the port. IPv6 addresses are
// not currently supported. peerContact with less than 6 bytes will not be stored.
func (p *peerContactsSet) put(peerContact string) bool {
	if len(peerContact) < 6 {
		return false
	}
	if ok := p.set[peerContact]; ok {
		return false
	}
	p.set[peerContact] = true
	r := &ring.Ring{Value: peerContact}
	if p.ring == nil {
		p.ring = r
	} else {
		p.ring.Link(r)
	}
	return true
}

// drop cycles throught the peerContactSet and deletes the contact if it finds it
// if the argument is empty, it first tries to drop a dead peer
func (p *peerContactsSet) drop(peerContact string) string {
	if peerContact == "" {
		if c := p.dropDead(); c != "" {
			return c
		} else {
			return p.drop(p.ring.Next().Value.(string))
		}
	}
	for i := 0; i < p.ring.Len()+1; i++ {
		if p.ring.Move(1).Value.(string) == peerContact {
			dn := p.ring.Unlink(1).Value.(string)
			delete(p.set, dn)
			return dn
		}
	}
	return ""
}

// dropDead drops the first dead contact, returns the id if a contact was dropped
func (p *peerContactsSet) dropDead() string {
	for i := 0; i < p.ring.Len()+1; i++ {
		if !p.set[p.ring.Move(1).Value.(string)] {
			dn := p.ring.Unlink(1).Value.(string)
			delete(p.set, dn)
			return dn
		}
	}
	return ""
}

func (p *peerContactsSet) kill(peerContact string) {
	if ok := p.set[peerContact]; ok {
		p.set[peerContact] = false
	}
}

// Size is the number of contacts known for an infohash.
func (p *peerContactsSet) Size() int {
	return len(p.set)
}

func (p *peerContactsSet) Alive() int {
	var ret int = 0
	for ih := range p.set {
		if p.set[ih] {
			ret++
		}
	}
	return ret
}

func newPeerStore(maxInfoHashes, maxInfoHashPeers int) *peerStore {
	return &peerStore{
		infoHashPeers:        must(lru.New[InfoHash, *peerContactsSet](maxInfoHashes)),
		localActiveDownloads: make(map[InfoHash]int),
		maxInfoHashes:        maxInfoHashes,
		maxInfoHashPeers:     maxInfoHashPeers,
	}
}

type peerStore struct {
	// cache of peers for infohashes. Each key is an infohash and the
	// values are peerContactsSet.
	infoHashPeers *lru.Cache[InfoHash, *peerContactsSet]
	// infoHashes for which we are peers.
	localActiveDownloads map[InfoHash]int // value is port number
	maxInfoHashes        int
	maxInfoHashPeers     int
}

func (h *peerStore) Get(ih InfoHash) *peerContactsSet {
	c, ok := h.infoHashPeers.Get(ih)
	if !ok {
		return nil
	}
	return c
}

// count shows the number of known peers for the given infohash.
func (h *peerStore) Count(ih InfoHash) int {
	peers := h.Get(ih)
	if peers == nil {
		return 0
	}
	return peers.Size()
}

func (h *peerStore) Alive(ih InfoHash) int {
	peers := h.Get(ih)
	if peers == nil {
		return 0
	}
	return peers.Alive()
}

// peerContacts returns a random set of 8 peers for the ih InfoHash.
func (h *peerStore) PeerContacts(ih InfoHash) []string {
	peers := h.Get(ih)
	if peers == nil {
		return nil
	}
	return peers.next()
}

// addContact as a peer for the provided ih. Returns true if the contact was
// added, false otherwise (e.g: already present, or invalid).
func (h *peerStore) AddContact(ih InfoHash, peerContact string) bool {
	var peers *peerContactsSet
	peers, ok := h.infoHashPeers.Get(ih)
	if ok {
		if peers != nil {
			if peers.Size() >= h.maxInfoHashPeers {
				if _, ok := peers.set[peerContact]; ok {
					return false
				}
				if peers.drop("") == "" {
					return false
				}
			}
			h.infoHashPeers.Add(ih, peers)
			return peers.put(peerContact)
		}
		// Bogus peer contacts, reset them.
	}
	peers = &peerContactsSet{set: make(map[string]bool)}
	h.infoHashPeers.Add(ih, peers)
	return peers.put(peerContact)
}

func (h *peerStore) KillContact(peerContact string) {
	if h == nil {
		return
	}
	for ih := range h.localActiveDownloads {
		if p := h.Get(ih); p != nil {
			p.kill(peerContact)
		}
	}
}

func (h *peerStore) AddLocalDownload(ih InfoHash, port int) {
	h.localActiveDownloads[ih] = port
}

func (h *peerStore) HasLocalDownload(ih InfoHash) (port int) {
	port, _ = h.localActiveDownloads[ih]
	return
}

func (h *peerStore) RemoveLocalDownload(ih InfoHash) {
	delete(h.localActiveDownloads, ih)
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}

	return v
}

type PeerStore interface {
	Get(ih InfoHash) *peerContactsSet
	Count(ih InfoHash) int
	Alive(ih InfoHash) int
	PeerContacts(ih InfoHash) []string
	AddContact(ih InfoHash, peerContact string) bool
	KillContact(peerContact string)
	AddLocalDownload(ih InfoHash, port int)
	HasLocalDownload(ih InfoHash) (port int)
	RemoveLocalDownload(ih InfoHash)
}
