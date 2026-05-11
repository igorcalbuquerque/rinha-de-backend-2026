package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

type testDataset struct {
	Entries []struct {
		Request          FraudRequest `json:"request"`
		ExpectedApproved bool         `json:"expected_approved"`
	} `json:"entries"`
}

func TestClassifierDetectionScore(t *testing.T) {
	resourcesPath := filepath.Join("..", "resources")
	classifier, mccRisk, norms, err := loadAll(resourcesPath)
	if err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join("..", "test", "test-data.json"))
	if err != nil {
		t.Fatal(err)
	}

	var data testDataset
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}

	var tp, tn, fp, fn int
	scores := make([]float64, 0, len(data.Entries))
	expected := make([]bool, 0, len(data.Entries))
	for _, entry := range data.Entries {
		vec := normalize(&entry.Request, mccRisk, norms)
		rawScore := classifier.RawScore(vec)
		scores = append(scores, rawScore)
		expected = append(expected, entry.ExpectedApproved)
		approved := calibrateScore(rawScore) < 0.6
		switch {
		case entry.ExpectedApproved && approved:
			tn++
		case !entry.ExpectedApproved && !approved:
			tp++
		case entry.ExpectedApproved && !approved:
			fp++
		default:
			fn++
		}
	}

	failures := fp + fn
	weighted := fp + 3*fn
	t.Logf("tp=%d tn=%d fp=%d fn=%d failures=%.4f weighted=%.4f",
		tp, tn, fp, fn,
		float64(failures)/float64(len(data.Entries)),
		float64(weighted)/float64(len(data.Entries)),
	)

	bestThreshold := 0.0
	bestWeighted := math.MaxInt
	bestFailures := math.MaxInt
	var bestTP, bestTN, bestFP, bestFN int
	for threshold := 0.01; threshold < 0.99; threshold += 0.001 {
		tp, tn, fp, fn = 0, 0, 0, 0
		for i, rawScore := range scores {
			approved := rawScore < threshold
			switch {
			case expected[i] && approved:
				tn++
			case !expected[i] && !approved:
				tp++
			case expected[i] && !approved:
				fp++
			default:
				fn++
			}
		}
		weighted := fp + 3*fn
		failures := fp + fn
		if weighted < bestWeighted || (weighted == bestWeighted && failures < bestFailures) {
			bestThreshold = threshold
			bestWeighted = weighted
			bestFailures = failures
			bestTP, bestTN, bestFP, bestFN = tp, tn, fp, fn
		}
	}
	t.Logf("best threshold=%.3f tp=%d tn=%d fp=%d fn=%d failures=%.4f weighted=%.4f",
		bestThreshold, bestTP, bestTN, bestFP, bestFN,
		float64(bestFailures)/float64(len(data.Entries)),
		float64(bestWeighted)/float64(len(data.Entries)),
	)
}
