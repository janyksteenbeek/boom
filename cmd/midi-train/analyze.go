package main

// detectType infers the control type (button/fader) from the captured min/max
// passes. Notes are always treated as buttons; CCs with very few distinct
// values are treated as toggle-style buttons.
func detectType(minMsgs, maxMsgs []MIDIMessage) string {
	if len(minMsgs) == 0 && len(maxMsgs) == 0 {
		return "button"
	}

	var primary MIDIMessage
	if len(maxMsgs) > 0 {
		primary = maxMsgs[0]
	} else if len(minMsgs) > 0 {
		primary = minMsgs[0]
	}

	if primary.Status == "note" {
		return "button"
	}

	valueSet := make(map[uint8]bool)
	for _, m := range maxMsgs {
		if m.Status == primary.Status && m.Number == primary.Number && m.Channel == primary.Channel {
			valueSet[m.Value] = true
		}
	}

	if len(valueSet) <= 3 {
		return "button"
	}

	return "fader"
}

// dominantMessage finds the most frequent (channel, status, number) triple in
// the captured messages and returns it with the maximum value seen for that
// triple. Returns nil when the slice is empty.
func dominantMessage(msgs []MIDIMessage) *MIDIMessage {
	if len(msgs) == 0 {
		return nil
	}

	type key struct {
		ch     uint8
		status string
		num    uint8
	}
	counts := make(map[key]int)
	for _, m := range msgs {
		k := key{m.Channel, m.Status, m.Number}
		counts[k]++
	}

	var bestKey key
	bestCount := 0
	for k, c := range counts {
		if c > bestCount {
			bestKey = k
			bestCount = c
		}
	}

	var minVal uint8 = 127
	var maxVal uint8 = 0
	for _, m := range msgs {
		if m.Channel == bestKey.ch && m.Status == bestKey.status && m.Number == bestKey.num {
			if m.Value < minVal {
				minVal = m.Value
			}
			if m.Value > maxVal {
				maxVal = m.Value
			}
		}
	}
	_ = minVal // kept for clarity; max is what we surface

	return &MIDIMessage{
		Channel: bestKey.ch,
		Status:  bestKey.status,
		Number:  bestKey.num,
		Value:   maxVal,
	}
}

// extractRange returns the min/max value seen for the dominant control in the
// supplied min and max passes, plus the filtered sample.
func extractRange(dom *MIDIMessage, minMsgs, maxMsgs []MIDIMessage) (minVal, maxVal uint8, sample []MIDIMessage) {
	minVal = dom.Value
	for _, m := range minMsgs {
		if m.Channel == dom.Channel && m.Status == dom.Status && m.Number == dom.Number {
			minVal = m.Value
		}
	}
	for _, m := range maxMsgs {
		if m.Channel == dom.Channel && m.Status == dom.Status && m.Number == dom.Number {
			if m.Value > maxVal {
				maxVal = m.Value
			}
		}
	}
	if maxVal == 0 {
		maxVal = dom.Value
	}

	all := append(minMsgs, maxMsgs...)
	for _, m := range all {
		if m.Channel == dom.Channel && m.Status == dom.Status && m.Number == dom.Number {
			sample = append(sample, m)
		}
	}
	return minVal, maxVal, sample
}
