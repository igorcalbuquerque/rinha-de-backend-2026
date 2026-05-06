package main

import "time"

type Normalization struct {
	MaxAmount            float64 `json:"max_amount"`
	MaxInstallments      float64 `json:"max_installments"`
	AmountVsAvgRatio     float64 `json:"amount_vs_avg_ratio"`
	MaxMinutes           float64 `json:"max_minutes"`
	MaxKm                float64 `json:"max_km"`
	MaxTxCount24h        float64 `json:"max_tx_count_24h"`
	MaxMerchantAvgAmount float64 `json:"max_merchant_avg_amount"`
}

func clamp(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

func normDiv(value, max float64) float64 {
	if max <= 0 {
		return 0
	}
	return clamp(value / max)
}

func normalize(req *FraudRequest, mccRisk map[string]float64, norms Normalization) [dims]float32 {
	tx := req.Transaction
	cust := req.Customer
	merch := req.Merchant
	term := req.Terminal
	last := req.LastTransaction

	t, txTimeErr := time.Parse(time.RFC3339, tx.RequestedAt)
	var hour, dow float64
	if txTimeErr == nil {
		hour = float64(t.UTC().Hour())
		dow = float64((int(t.UTC().Weekday()) + 6) % 7)
	}

	var minutesSinceLast float64 = -1
	var kmFromLast float64 = -1
	if last != nil {
		lastT, err := time.Parse(time.RFC3339, last.Timestamp)
		if txTimeErr == nil && err == nil {
			diff := t.Sub(lastT).Minutes()
			minutesSinceLast = normDiv(diff, norms.MaxMinutes)
		}
		kmFromLast = normDiv(last.KmFromCurrent, norms.MaxKm)
	}

	knownSet := make(map[string]struct{}, len(cust.KnownMerchants))
	for _, m := range cust.KnownMerchants {
		knownSet[m] = struct{}{}
	}
	var unknownMerchant float64
	if _, ok := knownSet[merch.ID]; !ok {
		unknownMerchant = 1
	}

	mcc, ok := mccRisk[merch.MCC]
	if !ok {
		mcc = 0.5
	}

	var isOnline, cardPresent float64
	if term.IsOnline {
		isOnline = 1
	}
	if term.CardPresent {
		cardPresent = 1
	}

	var amountVsAvg float64
	if cust.AvgAmount > 0 && norms.AmountVsAvgRatio > 0 {
		amountVsAvg = clamp((tx.Amount / cust.AvgAmount) / norms.AmountVsAvgRatio)
	}

	var vec [dims]float32
	vec[0] = float32(normDiv(tx.Amount, norms.MaxAmount))
	vec[1] = float32(normDiv(float64(tx.Installments), norms.MaxInstallments))
	vec[2] = float32(amountVsAvg)
	vec[3] = float32(hour / 23)
	vec[4] = float32(dow / 6)
	vec[5] = float32(minutesSinceLast)
	vec[6] = float32(kmFromLast)
	vec[7] = float32(normDiv(term.KmFromHome, norms.MaxKm))
	vec[8] = float32(normDiv(float64(cust.TxCount24h), norms.MaxTxCount24h))
	vec[9] = float32(isOnline)
	vec[10] = float32(cardPresent)
	vec[11] = float32(unknownMerchant)
	vec[12] = float32(mcc)
	vec[13] = float32(normDiv(merch.AvgAmount, norms.MaxMerchantAvgAmount))
	return vec
}
