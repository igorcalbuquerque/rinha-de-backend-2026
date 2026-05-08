package main

import (
	"container/heap"
	"math"
	"math/rand"
	"sort"
	"sync"
)

const (
	dims     = 14
	leafSize = 32
)

// quantize maps a float64 in [-1, 1] to uint16.
// -1.0 sentinel maps to 0; [0.0, 1.0] maps to [1, 65535].
func quantize(v float64) uint16 {
	if v < -0.5 {
		return 0
	}
	q := v*65534.0 + 1.5
	if q < 1 {
		return 1
	}
	if q > 65535 {
		return 65535
	}
	return uint16(q)
}

const dequantScale = float32(1.0 / 65534.0)

func dequantize(q uint16) float32 {
	if q == 0 {
		return -1.0
	}
	return float32(q-1) * dequantScale
}

var heapPool = sync.Pool{
	New: func() interface{} {
		h := make(maxHeap, 0, 6)
		return &h
	},
}

// sqDistQQ returns the squared Euclidean distance between two quantized vectors.
func sqDistQQ(a, b [dims]uint16) float32 {
	var sum float32
	for i := 0; i < dims; i++ {
		d := dequantize(a[i]) - dequantize(b[i])
		sum += d * d
	}
	return sum
}

// distQF returns the Euclidean distance between a quantized stored vector and a float32 query.
func distQF(stored [dims]uint16, query [dims]float32) float32 {
	var sum float32
	for i := 0; i < dims; i++ {
		d := dequantize(stored[i]) - query[i]
		sum += d * d
	}
	return float32(math.Sqrt(float64(sum)))
}

type Point struct {
	Vec   [dims]uint16
	Fraud bool
}

type vpNode struct {
	lo    int32
	hi    int32
	split int32
	mu    float32
	left  int32
	right int32
}

type VPTree struct {
	points []Point
	idx    []int32
	nodes  []vpNode
	root   int32
}

type distIndex struct {
	dist float32
	idx  int32
}

func BuildVPTree(pts []Point) *VPTree {
	n := len(pts)
	if n == 0 {
		return &VPTree{root: -1}
	}

	idx := make([]int32, n)
	for i := range idx {
		idx[i] = int32(i)
	}

	t := &VPTree{
		points: pts,
		idx:    idx,
		nodes:  make([]vpNode, 0, n/leafSize*2+2),
		root:   -1,
	}

	tmp := make([]distIndex, n)
	t.root = t.buildNode(0, n, tmp)
	return t
}

func (t *VPTree) buildNode(lo, hi int, tmp []distIndex) int32 {
	if lo >= hi {
		return -1
	}

	nodeIdx := int32(len(t.nodes))
	t.nodes = append(t.nodes, vpNode{lo: int32(lo), hi: int32(hi), left: -1, right: -1})

	if hi-lo <= leafSize {
		return nodeIdx
	}

	vpOff := lo + rand.Intn(hi-lo)
	t.idx[lo], t.idx[vpOff] = t.idx[vpOff], t.idx[lo]
	vp := t.points[t.idx[lo]].Vec

	inner := hi - lo - 1
	for i := 0; i < inner; i++ {
		pointIdx := t.idx[lo+1+i]
		tmp[i] = distIndex{
			dist: sqDistQQ(vp, t.points[pointIdx].Vec),
			idx:  pointIdx,
		}
	}

	sort.Slice(tmp[:inner], func(a, b int) bool {
		return tmp[a].dist < tmp[b].dist
	})

	mid := inner / 2
	t.nodes[nodeIdx].mu = float32(math.Sqrt(float64(tmp[mid].dist)))

	// Reorder t.idx[lo+1:hi] according to the sorted permutation.
	for i := 0; i < inner; i++ {
		t.idx[lo+1+i] = tmp[i].idx
	}

	split := lo + 1 + mid + 1
	t.nodes[nodeIdx].split = int32(split)

	t.nodes[nodeIdx].left = t.buildNode(lo+1, split, tmp)
	t.nodes[nodeIdx].right = t.buildNode(split, hi, tmp)

	return nodeIdx
}

type candidate struct {
	dist  float32
	fraud bool
}

type maxHeap []candidate

func (h maxHeap) Len() int            { return len(h) }
func (h maxHeap) Less(i, j int) bool  { return h[i].dist > h[j].dist }
func (h maxHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *maxHeap) Push(x interface{}) { *h = append(*h, x.(candidate)) }
func (h *maxHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

// KNNFraudCount returns the number of fraud labels among the k nearest neighbors.
func (t *VPTree) KNNFraudCount(query [dims]float32, k int) int {
	hp := heapPool.Get().(*maxHeap)
	*hp = (*hp)[:0]
	t.searchNode(t.root, query, k, hp)
	count := 0
	for _, c := range *hp {
		if c.fraud {
			count++
		}
	}
	heapPool.Put(hp)
	return count
}

func (t *VPTree) searchNode(nodeIdx int32, query [dims]float32, k int, h *maxHeap) {
	if nodeIdx < 0 {
		return
	}
	node := t.nodes[nodeIdx]

	if node.left < 0 { // leaf
		for i := node.lo; i < node.hi; i++ {
			p := t.points[t.idx[i]]
			d := distQF(p.Vec, query)
			t.tryInsert(h, k, d, p.Fraud)
		}
		return
	}

	vp := t.points[t.idx[node.lo]]
	d := distQF(vp.Vec, query)
	t.tryInsert(h, k, d, vp.Fraud)

	tau := float32(math.MaxFloat32)
	if h.Len() == k {
		tau = (*h)[0].dist
	}

	if d < node.mu {
		t.searchNode(node.left, query, k, h)
		if h.Len() == k {
			tau = (*h)[0].dist
		}
		if node.mu-d <= tau {
			t.searchNode(node.right, query, k, h)
		}
	} else {
		t.searchNode(node.right, query, k, h)
		if h.Len() == k {
			tau = (*h)[0].dist
		}
		if d-node.mu <= tau {
			t.searchNode(node.left, query, k, h)
		}
	}
}

func (t *VPTree) tryInsert(h *maxHeap, k int, d float32, fraud bool) {
	if h.Len() < k {
		heap.Push(h, candidate{d, fraud})
	} else if d < (*h)[0].dist {
		(*h)[0] = candidate{d, fraud}
		heap.Fix(h, 0)
	}
}
