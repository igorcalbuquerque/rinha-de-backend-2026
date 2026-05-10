package main

import (
	"bytes"
	"strconv"
)

var (
	keyTransaction     = []byte(`"transaction"`)
	keyCustomer        = []byte(`"customer"`)
	keyMerchant        = []byte(`"merchant"`)
	keyTerminal        = []byte(`"terminal"`)
	keyLastTransaction = []byte(`"last_transaction"`)
	keyKnownMerchants  = []byte(`"known_merchants"`)
	keyAmount          = []byte(`"amount"`)
	keyInstallments    = []byte(`"installments"`)
	keyRequestedAt     = []byte(`"requested_at"`)
	keyAvgAmount       = []byte(`"avg_amount"`)
	keyTxCount24h      = []byte(`"tx_count_24h"`)
	keyID              = []byte(`"id"`)
	keyMCC             = []byte(`"mcc"`)
	keyIsOnline        = []byte(`"is_online"`)
	keyCardPresent     = []byte(`"card_present"`)
	keyKmFromHome      = []byte(`"km_from_home"`)
	keyTimestamp       = []byte(`"timestamp"`)
	keyKmFromCurrent   = []byte(`"km_from_current"`)
)

func normalizeBody(body []byte, mccRisk map[string]float64, norms Normalization) ([dims]float32, bool) {
	txObj, ok := objectValue(body, keyTransaction)
	if !ok {
		return [dims]float32{}, false
	}
	custObj, ok := objectValue(body, keyCustomer)
	if !ok {
		return [dims]float32{}, false
	}
	merchObj, ok := objectValue(body, keyMerchant)
	if !ok {
		return [dims]float32{}, false
	}
	termObj, ok := objectValue(body, keyTerminal)
	if !ok {
		return [dims]float32{}, false
	}

	amount := numberValue(txObj, keyAmount)
	installments := numberValue(txObj, keyInstallments)
	requestedAt := stringValue(txObj, keyRequestedAt)
	customerAvg := numberValue(custObj, keyAvgAmount)
	txCount24h := numberValue(custObj, keyTxCount24h)
	merchantID := stringValue(merchObj, keyID)
	mccBytes := stringValue(merchObj, keyMCC)
	merchantAvg := numberValue(merchObj, keyAvgAmount)
	isOnline := boolValue(termObj, keyIsOnline)
	cardPresent := boolValue(termObj, keyCardPresent)
	kmFromHome := numberValue(termObj, keyKmFromHome)

	hour, dow, txMinutes := parseTimestamp(requestedAt)

	minutesSinceLast := -1.0
	kmFromLast := -1.0
	if lastObj, ok := objectOrNullValue(body, keyLastTransaction); ok && len(lastObj) > 0 {
		lastAt := stringValue(lastObj, keyTimestamp)
		_, _, lastMinutes := parseTimestamp(lastAt)
		if txMinutes != 0 && lastMinutes != 0 {
			minutesSinceLast = normDiv(float64(txMinutes-lastMinutes), norms.MaxMinutes)
		}
		kmFromLast = normDiv(numberValue(lastObj, keyKmFromCurrent), norms.MaxKm)
	}

	unknownMerchant := 1.0
	if known, ok := arrayValue(custObj, keyKnownMerchants); ok && containsQuoted(known, merchantID) {
		unknownMerchant = 0
	}

	mcc := 0.5
	if v, ok := mccRisk[string(mccBytes)]; ok {
		mcc = v
	}

	amountVsAvg := 0.0
	if customerAvg > 0 && norms.AmountVsAvgRatio > 0 {
		amountVsAvg = clamp((amount / customerAvg) / norms.AmountVsAvgRatio)
	}

	var online, present float64
	if isOnline {
		online = 1
	}
	if cardPresent {
		present = 1
	}

	var vec [dims]float32
	vec[0] = float32(normDiv(amount, norms.MaxAmount))
	vec[1] = float32(normDiv(installments, norms.MaxInstallments))
	vec[2] = float32(amountVsAvg)
	vec[3] = float32(float64(hour) / 23)
	vec[4] = float32(float64(dow) / 6)
	vec[5] = float32(minutesSinceLast)
	vec[6] = float32(kmFromLast)
	vec[7] = float32(normDiv(kmFromHome, norms.MaxKm))
	vec[8] = float32(normDiv(txCount24h, norms.MaxTxCount24h))
	vec[9] = float32(online)
	vec[10] = float32(present)
	vec[11] = float32(unknownMerchant)
	vec[12] = float32(mcc)
	vec[13] = float32(normDiv(merchantAvg, norms.MaxMerchantAvgAmount))
	return vec, true
}

func objectValue(body, key []byte) ([]byte, bool) {
	i := bytes.Index(body, key)
	if i < 0 {
		return nil, false
	}
	i += len(key)
	i = skipColonSpace(body, i)
	if i >= len(body) || body[i] != '{' {
		return nil, false
	}
	return scanBalanced(body, i, '{', '}')
}

func objectOrNullValue(body, key []byte) ([]byte, bool) {
	i := bytes.Index(body, key)
	if i < 0 {
		return nil, false
	}
	i += len(key)
	i = skipColonSpace(body, i)
	if i >= len(body) {
		return nil, false
	}
	if body[i] == 'n' {
		return nil, true
	}
	if body[i] != '{' {
		return nil, false
	}
	return scanBalanced(body, i, '{', '}')
}

func arrayValue(body, key []byte) ([]byte, bool) {
	i := bytes.Index(body, key)
	if i < 0 {
		return nil, false
	}
	i += len(key)
	i = skipColonSpace(body, i)
	if i >= len(body) || body[i] != '[' {
		return nil, false
	}
	return scanBalanced(body, i, '[', ']')
}

func scanBalanced(body []byte, i int, open, close byte) ([]byte, bool) {
	start := i + 1
	depth := 0
	inString := false
	escape := false
	for ; i < len(body); i++ {
		c := body[i]
		if inString {
			if escape {
				escape = false
			} else if c == '\\' {
				escape = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			continue
		}
		if c == open {
			depth++
		} else if c == close {
			depth--
			if depth == 0 {
				return body[start:i], true
			}
		}
	}
	return nil, false
}

func numberValue(body, key []byte) float64 {
	i := bytes.Index(body, key)
	if i < 0 {
		return 0
	}
	i += len(key)
	i = skipColonSpace(body, i)
	j := i
	for j < len(body) {
		c := body[j]
		if (c >= '0' && c <= '9') || c == '.' || c == '-' || c == '+' || c == 'e' || c == 'E' {
			j++
			continue
		}
		break
	}
	v, _ := strconv.ParseFloat(string(body[i:j]), 64)
	return v
}

func boolValue(body, key []byte) bool {
	i := bytes.Index(body, key)
	if i < 0 {
		return false
	}
	i += len(key)
	i = skipColonSpace(body, i)
	return i+3 < len(body) && body[i] == 't'
}

func stringValue(body, key []byte) []byte {
	i := bytes.Index(body, key)
	if i < 0 {
		return nil
	}
	i += len(key)
	i = skipColonSpace(body, i)
	if i >= len(body) || body[i] != '"' {
		return nil
	}
	i++
	j := i
	for j < len(body) && body[j] != '"' {
		j++
	}
	return body[i:j]
}

func skipColonSpace(body []byte, i int) int {
	for i < len(body) && body[i] != ':' {
		i++
	}
	if i < len(body) {
		i++
	}
	for i < len(body) && (body[i] == ' ' || body[i] == '\n' || body[i] == '\r' || body[i] == '\t') {
		i++
	}
	return i
}

func containsQuoted(body, value []byte) bool {
	if len(value) == 0 {
		return false
	}
	for {
		i := bytes.IndexByte(body, '"')
		if i < 0 {
			return false
		}
		body = body[i+1:]
		j := bytes.IndexByte(body, '"')
		if j < 0 {
			return false
		}
		if bytes.Equal(body[:j], value) {
			return true
		}
		body = body[j+1:]
	}
}

func parseTimestamp(s []byte) (int, int, int) {
	if len(s) < 16 {
		return 0, 0, 0
	}
	y := atoi4(s[0:4])
	m := atoi2(s[5:7])
	d := atoi2(s[8:10])
	hh := atoi2(s[11:13])
	mm := atoi2(s[14:16])
	days := daysFromCivil(y, m, d)
	return hh, weekdayMondayZero(y, m, d), days*1440 + hh*60 + mm
}

func atoi2(s []byte) int {
	return int(s[0]-'0')*10 + int(s[1]-'0')
}

func atoi4(s []byte) int {
	return int(s[0]-'0')*1000 + int(s[1]-'0')*100 + int(s[2]-'0')*10 + int(s[3]-'0')
}

func weekdayMondayZero(y, m, d int) int {
	t := [...]int{0, 3, 2, 5, 0, 3, 5, 1, 4, 6, 2, 4}
	if m < 3 {
		y--
	}
	w := (y + y/4 - y/100 + y/400 + t[m-1] + d) % 7
	return (w + 6) % 7
}

func daysFromCivil(y, m, d int) int {
	y -= btoi(m <= 2)
	era := divFloor(y, 400)
	yoe := y - era*400
	mp := m + 9
	if m > 2 {
		mp = m - 3
	}
	doy := (153*mp+2)/5 + d - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return era*146097 + doe - 719468
}

func divFloor(a, b int) int {
	if a >= 0 {
		return a / b
	}
	return -((-a + b - 1) / b)
}

func btoi(v bool) int {
	if v {
		return 1
	}
	return 0
}
