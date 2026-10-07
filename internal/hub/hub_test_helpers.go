//go:build testing

package hub

// TESTING ONLY: GetPubkey returns the public key
func (h *Hub) GetPubkey() string {
	return h.publicKey()
}

// TESTING ONLY: SetPubkey sets the public key and forgets the cached signer, so the next
// GetSSHKey loads the key again.
func (h *Hub) SetPubkey(pubkey string) {
	h.keyMu.Lock()
	defer h.keyMu.Unlock()
	h.pubKey = pubkey
	h.signer = nil
}

func (h *Hub) SetCollectionAuthSettings() error {
	return setCollectionAuthSettings(h)
}
