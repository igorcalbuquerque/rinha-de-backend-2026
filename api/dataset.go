package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
)

func loadMCCRisk(path string) (map[string]float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var m map[string]float64
	if err := json.NewDecoder(f).Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

func loadNormalization(path string) (Normalization, error) {
	f, err := os.Open(path)
	if err != nil {
		return Normalization{}, err
	}
	defer f.Close()
	var n Normalization
	if err := json.NewDecoder(f).Decode(&n); err != nil {
		return Normalization{}, err
	}
	return n, nil
}

func loadReferences(path string) ([]Point, error) {
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

	points := make([]Point, 0, 3_000_000)

	var rec struct {
		Vector [dims]float64 `json:"vector"`
		Label  string        `json:"label"`
	}

	for dec.More() {
		if err := dec.Decode(&rec); err != nil {
			return nil, err
		}
		var p Point
		for i, v := range rec.Vector {
			p.Vec[i] = quantize(v)
		}
		p.Fraud = rec.Label == "fraud"
		points = append(points, p)
	}

	return points, nil
}

func loadAll(resourcesPath string) (*VPTree, map[string]float64, Normalization, error) {
	mccRisk, norms, err := loadMetadata(resourcesPath)
	if err != nil {
		return nil, nil, Normalization{}, err
	}

	points, err := loadReferences(filepath.Join(resourcesPath, "references.json.gz"))
	if err != nil {
		return nil, nil, Normalization{}, fmt.Errorf("references: %w", err)
	}

	tree := BuildVPTree(points)
	debug.FreeOSMemory()
	return tree, mccRisk, norms, nil
}

func loadMetadata(resourcesPath string) (map[string]float64, Normalization, error) {
	mccRisk, err := loadMCCRisk(filepath.Join(resourcesPath, "mcc_risk.json"))
	if err != nil {
		return nil, Normalization{}, fmt.Errorf("mcc_risk: %w", err)
	}

	norms, err := loadNormalization(filepath.Join(resourcesPath, "normalization.json"))
	if err != nil {
		return nil, Normalization{}, fmt.Errorf("normalization: %w", err)
	}

	return mccRisk, norms, nil
}
