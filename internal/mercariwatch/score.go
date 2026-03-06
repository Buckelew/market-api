package mercariwatch

import "fmt"

type DealParams struct {
	MinROI             float64
	MinConfidence      float64
	BuyerFeePercent    float64
	DefaultShippingUSD float64
}

type DealScore struct {
	ExpectedSaleUSD float64
	TotalCostUSD    float64
	ProfitUSD       float64
	ROI             float64
	Confidence      float64
	Pass            bool
	Reasons         []string
}

func ScoreResolvedDeal(priceCents int, shippingCents *int, response *ResolveResponse, params DealParams) DealScore {
	reasons := []string{}
	if response == nil {
		return DealScore{
			Confidence: 0,
			Pass:       false,
			Reasons:    []string{"missing resolve response"},
		}
	}

	if !response.MarketMatch.Matched {
		reasons = append(reasons, "no TCGplayer match found")
	}

	var expectedSale float64
	var confidence float64
	if len(response.MarketMatches) > 0 {
		expectedSale = combinedExpectedSale(response.MarketMatches)
		confidence = minMatchConfidence(response.MarketMatches)
	} else {
		expectedSale = chooseExpectedSale(response.MarketMatch)
		confidence = response.MarketMatch.Confidence
	}
	if expectedSale <= 0 {
		reasons = append(reasons, "no expected sale from market data")
	}
	if confidence <= 0 {
		confidence = response.Classification.Confidence
	}
	if confidence < params.MinConfidence {
		reasons = append(reasons, fmt.Sprintf("confidence %.2f < %.2f", confidence, params.MinConfidence))
	}

	priceUSD := float64(priceCents) / 100.0
	buyerFee := priceUSD * params.BuyerFeePercent
	shippingUSD := params.DefaultShippingUSD
	if shippingCents != nil {
		shippingUSD = float64(*shippingCents) / 100.0
	}
	totalCost := priceUSD + buyerFee + shippingUSD
	profit := expectedSale - totalCost
	roi := 0.0
	if totalCost > 0 {
		roi = profit / totalCost
	}

	if roi < params.MinROI {
		reasons = append(reasons, fmt.Sprintf("roi %.2f < %.2f", roi, params.MinROI))
	}

	return DealScore{
		ExpectedSaleUSD: expectedSale,
		TotalCostUSD:    totalCost,
		ProfitUSD:       profit,
		ROI:             roi,
		Confidence:      confidence,
		Pass:            len(reasons) == 0,
		Reasons:         reasons,
	}
}

func combinedExpectedSale(matches []MarketMatch) float64 {
	total := 0.0
	for _, m := range matches {
		total += chooseExpectedSale(m)
	}
	return total
}

func minMatchConfidence(matches []MarketMatch) float64 {
	if len(matches) == 0 {
		return 0
	}
	min := matches[0].Confidence
	for _, m := range matches[1:] {
		if m.Confidence < min {
			min = m.Confidence
		}
	}
	return min
}

func chooseExpectedSale(match MarketMatch) float64 {
	if match.MarketPrice > 0 && match.RecentMedianSale > 0 {
		if match.MarketPrice < match.RecentMedianSale {
			return match.MarketPrice
		}
		return match.RecentMedianSale
	}
	if match.MarketPrice > 0 {
		return match.MarketPrice
	}
	if match.RecentMedianSale > 0 {
		return match.RecentMedianSale
	}
	return 0
}
