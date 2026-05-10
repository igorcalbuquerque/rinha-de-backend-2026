package main

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"
)

type Transaction struct {
	Amount       float64 `json:"amount"`
	Installments int     `json:"installments"`
	RequestedAt  string  `json:"requested_at"`
}

type Customer struct {
	AvgAmount      float64  `json:"avg_amount"`
	TxCount24h     int      `json:"tx_count_24h"`
	KnownMerchants []string `json:"known_merchants"`
}

type Merchant struct {
	ID        string  `json:"id"`
	MCC       string  `json:"mcc"`
	AvgAmount float64 `json:"avg_amount"`
}

type Terminal struct {
	IsOnline    bool    `json:"is_online"`
	CardPresent bool    `json:"card_present"`
	KmFromHome  float64 `json:"km_from_home"`
}

type LastTransaction struct {
	Timestamp     string  `json:"timestamp"`
	KmFromCurrent float64 `json:"km_from_current"`
}

type FraudRequest struct {
	ID              string           `json:"id"`
	Transaction     Transaction      `json:"transaction"`
	Customer        Customer         `json:"customer"`
	Merchant        Merchant         `json:"merchant"`
	Terminal        Terminal         `json:"terminal"`
	LastTransaction *LastTransaction `json:"last_transaction"`
}

func writeFraudResponse(w http.ResponseWriter, fraudCount int) {
	switch fraudCount {
	case 0:
		io.WriteString(w, `{"approved":true,"fraud_score":0}`)
	case 1:
		io.WriteString(w, `{"approved":true,"fraud_score":0.2}`)
	case 2:
		io.WriteString(w, `{"approved":true,"fraud_score":0.4}`)
	case 3:
		io.WriteString(w, `{"approved":false,"fraud_score":0.6}`)
	case 4:
		io.WriteString(w, `{"approved":false,"fraud_score":0.8}`)
	default:
		io.WriteString(w, `{"approved":false,"fraud_score":1}`)
	}
}

type server struct {
	mu      sync.RWMutex
	ready   bool
	tree    *VPTree
	mccRisk map[string]float64
	norms   Normalization
}

func (s *server) readyHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	ready := s.ready
	s.mu.RUnlock()

	if !ready {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *server) fraudScoreHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	defer func() {
		if rec := recover(); rec != nil {
			writeFraudResponse(w, 0)
		}
	}()

	var req FraudRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeFraudResponse(w, 0)
		return
	}

	s.mu.RLock()
	tree := s.tree
	mccRisk := s.mccRisk
	norms := s.norms
	ready := s.ready
	s.mu.RUnlock()

	if !ready || tree == nil {
		writeFraudResponse(w, 0)
		return
	}

	vec := normalize(&req, mccRisk, norms)
	fraudCount := tree.KNNFraudCount(vec, 5)

	writeFraudResponse(w, fraudCount)
}
