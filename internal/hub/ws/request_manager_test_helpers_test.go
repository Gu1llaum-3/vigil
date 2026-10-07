//go:build testing

package ws

func (rm *RequestManager) hasPendingRequests() bool {
	rm.RLock()
	defer rm.RUnlock()
	return len(rm.pendingReqs) > 0
}
