//go:build testing

package hub

// TESTING ONLY: GetPubkey returns the public key
func (h *Hub) GetPubkey() string {
	return h.pubKey
}

// TESTING ONLY: SetPubkey sets the public key
func (h *Hub) SetPubkey(pubkey string) {
	h.pubKey = pubkey
}

func (h *Hub) SetCollectionAuthSettings() error {
	return setCollectionAuthSettings(h)
}
