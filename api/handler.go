package main

import (
	"encoding/json"
	"net/http"
	"strconv"
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

func writeScoreResponse(w http.ResponseWriter, score float64) {
	buf := make([]byte, 0, 48)
	if score < 0.6 {
		buf = append(buf, `{"approved":true,"fraud_score":`...)
	} else {
		buf = append(buf, `{"approved":false,"fraud_score":`...)
	}
	buf = strconv.AppendFloat(buf, score, 'f', 4, 64)
	buf = append(buf, '}')
	w.Write(buf)
}

type server struct {
	mu         sync.RWMutex
	ready      bool
	classifier *Classifier
	mccRisk    map[string]float64
	norms      Normalization
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
			writeScoreResponse(w, 0)
		}
	}()

	var req FraudRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeScoreResponse(w, 0)
		return
	}

	s.mu.RLock()
	classifier := s.classifier
	mccRisk := s.mccRisk
	norms := s.norms
	ready := s.ready
	s.mu.RUnlock()

	if !ready || classifier == nil {
		writeScoreResponse(w, 0)
		return
	}

	vec := normalize(&req, mccRisk, norms)
	writeScoreResponse(w, classifier.Score(vec))
}
