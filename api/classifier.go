package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
)

type bucketStat struct {
	Fraud uint32
	Total uint32
}

type Classifier struct {
	specific map[uint64]bucketStat
	mid      map[uint64]bucketStat
	low      map[uint64]bucketStat
	base     bucketStat
}

const decisionScore = 0.01

func loadClassifier(path string) (*Classifier, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	dec := json.NewDecoder(gz)
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		return nil, fmt.Errorf("expected '[', got %v", tok)
	}

	c := &Classifier{
		specific: make(map[uint64]bucketStat, 1_500_000),
		mid:      make(map[uint64]bucketStat, 800_000),
		low:      make(map[uint64]bucketStat, 64_000),
	}

	var rec struct {
		Vector [dims]float64 `json:"vector"`
		Label  string        `json:"label"`
	}

	for dec.More() {
		if err := dec.Decode(&rec); err != nil {
			return nil, err
		}

		var q [dims]uint16
		for i, v := range rec.Vector {
			q[i] = quantize(v)
		}
		fraud := rec.Label == "fraud"

		c.base = addStat(c.base, fraud)
		k := specificKey(q)
		c.specific[k] = addStat(c.specific[k], fraud)
		k = midKey(q)
		c.mid[k] = addStat(c.mid[k], fraud)
		k = lowKey(q)
		c.low[k] = addStat(c.low[k], fraud)
	}

	return c, nil
}

func (c *Classifier) Score(query [dims]float32) float64 {
	return calibrateScore(c.RawScore(query))
}

func (c *Classifier) RawScore(query [dims]float32) float64 {
	q := quantizeQuery(query)

	if st, ok := c.specific[specificKey(q)]; ok && st.Total >= 5 {
		return statScore(st)
	}
	if st, ok := c.mid[midKey(q)]; ok && st.Total >= 12 {
		return statScore(st)
	}
	if st, ok := c.low[lowKey(q)]; ok && st.Total >= 24 {
		return statScore(st)
	}
	return statScore(c.base)
}

func addStat(st bucketStat, fraud bool) bucketStat {
	st.Total++
	if fraud {
		st.Fraud++
	}
	return st
}

func statScore(st bucketStat) float64 {
	if st.Total == 0 {
		return 0
	}
	return float64(st.Fraud) / float64(st.Total)
}

func calibrateScore(score float64) float64 {
	if score < decisionScore {
		return score * (0.59 / decisionScore)
	}
	return 0.6 + ((score - decisionScore) * (0.4 / (1 - decisionScore)))
}

func specificKey(q [dims]uint16) uint64 {
	var k uint64
	k = pack(k, bucket(q[2], 16), 5)
	k = pack(k, bucket(q[0], 16), 5)
	k = pack(k, bucket(q[8], 16), 5)
	k = pack(k, bucket(q[12], 8), 4)
	k = pack(k, bit(q[9]), 1)
	k = pack(k, bit(q[10]), 1)
	k = pack(k, bit(q[11]), 1)
	k = pack(k, bucket(q[7], 16), 5)
	k = pack(k, bucket(q[6], 16), 5)
	k = pack(k, bucket(q[5], 16), 5)
	k = pack(k, bucket(q[3], 24), 5)
	k = pack(k, bucket(q[4], 7), 3)
	k = pack(k, bucket(q[1], 13), 4)
	k = pack(k, bucket(q[13], 16), 5)
	return k
}

func midKey(q [dims]uint16) uint64 {
	var k uint64
	k = pack(k, bucket(q[2], 16), 5)
	k = pack(k, bucket(q[0], 8), 4)
	k = pack(k, bucket(q[8], 8), 4)
	k = pack(k, bucket(q[12], 8), 4)
	k = pack(k, bit(q[9]), 1)
	k = pack(k, bit(q[10]), 1)
	k = pack(k, bit(q[11]), 1)
	k = pack(k, bucket(q[7], 8), 4)
	k = pack(k, bucket(q[6], 8), 4)
	k = pack(k, bucket(q[5], 8), 4)
	return k
}

func lowKey(q [dims]uint16) uint64 {
	var k uint64
	k = pack(k, bucket(q[2], 8), 4)
	k = pack(k, bucket(q[8], 8), 4)
	k = pack(k, bucket(q[12], 8), 4)
	k = pack(k, bit(q[9]), 1)
	k = pack(k, bit(q[10]), 1)
	k = pack(k, bit(q[11]), 1)
	k = pack(k, bucket(q[7], 8), 4)
	return k
}

func pack(k, v uint64, bits uint) uint64 {
	return (k << bits) | v
}

func bucket(v uint16, bins uint16) uint64 {
	if v == 0 {
		return 0
	}
	return 1 + uint64((uint32(v-1)*uint32(bins-1))/65534)
}

func bit(v uint16) uint64 {
	if v > 32768 {
		return 1
	}
	return 0
}
