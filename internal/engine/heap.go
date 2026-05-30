package engine

import "github.com/vmnavarro94/coding-challenge-mexico/internal/types"

// scoredOpportunity wraps an Opportunity with its heap index.
type scoredOpportunity struct {
	opp   types.Opportunity
	index int
}

// oppHeap implements container/heap as a max-heap ordered by Score descending.
type oppHeap []*scoredOpportunity

func (h oppHeap) Len() int { return len(h) }
func (h oppHeap) Less(i, j int) bool {
	// Max-heap: higher score = higher priority.
	return h[i].opp.Score.GreaterThan(h[j].opp.Score)
}
func (h oppHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}
func (h *oppHeap) Push(x any) {
	item := x.(*scoredOpportunity)
	item.index = len(*h)
	*h = append(*h, item)
}
func (h *oppHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*h = old[:n-1]
	return item
}
