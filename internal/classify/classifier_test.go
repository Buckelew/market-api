package classify

import (
	"testing"
)

func TestClassifyTradingCard(t *testing.T) {
	classifier := New()

	classification, signals := classifier.Classify("OP14-033 Perona", "English trading card")
	if classification.ProductType != "trading_card" {
		t.Fatalf("expected trading_card, got %q", classification.ProductType)
	}
	if signals.CardCode != "OP14-033" {
		t.Fatalf("expected OP14-033, got %q", signals.CardCode)
	}
}

func TestClassifySealedBoosterBox(t *testing.T) {
	classifier := New()

	classification, signals := classifier.Classify("One Piece OP10 Booster Box", "English sealed")
	if classification.ProductType != "sealed_product" {
		t.Fatalf("expected sealed_product, got %q", classification.ProductType)
	}
	if signals.SealedType != "booster_box" {
		t.Fatalf("expected booster_box, got %q", signals.SealedType)
	}
	if signals.SetCode != "OP10" {
		t.Fatalf("expected OP10, got %q", signals.SetCode)
	}
	if signals.Language != "en" {
		t.Fatalf("expected en language, got %q", signals.Language)
	}
}

func TestClassifySealedStarterDeckWithoutOnePieceWords(t *testing.T) {
	classifier := New()

	classification, signals := classifier.Classify("ST-21 Starter Deck", "")
	if classification.ProductType != "sealed_product" {
		t.Fatalf("expected sealed_product, got %q", classification.ProductType)
	}
	if signals.SetCode != "ST21" {
		t.Fatalf("expected ST21, got %q", signals.SetCode)
	}
	if signals.SealedType != "starter_deck" {
		t.Fatalf("expected starter_deck, got %q", signals.SealedType)
	}
}

func TestClassifyUnsealedBoosterBoxAsUnknown(t *testing.T) {
	classifier := New()

	classification, _ := classifier.Classify("One Piece TCG - The Azure Sea's Seven OP-14 Booster Box -unsealed", "")
	if classification.ProductType != "unknown" {
		t.Fatalf("expected unknown for unsealed box, got %q", classification.ProductType)
	}
	if classification.Confidence > 0.2 {
		t.Fatalf("expected low confidence for unsealed box, got %f", classification.Confidence)
	}
}

func TestClassifyOpenedBoxAsUnknown(t *testing.T) {
	classifier := New()

	classification, _ := classifier.Classify("One Piece OP14 Booster Box opened", "no cards included")
	if classification.ProductType != "unknown" {
		t.Fatalf("expected unknown for opened box, got %q", classification.ProductType)
	}
}

func TestClassifyCardLotWithMultipleCodes(t *testing.T) {
	classifier := New()

	classification, signals := classifier.Classify("One Piece card lot OP14-033 OP14-055 OP14-112", "")
	if classification.ProductType != "card_lot" {
		t.Fatalf("expected card_lot, got %q", classification.ProductType)
	}
	if len(signals.CardCodes) != 3 {
		t.Fatalf("expected 3 card codes, got %d: %v", len(signals.CardCodes), signals.CardCodes)
	}
	if signals.LotType != "lot" {
		t.Fatalf("expected lot type 'lot', got %q", signals.LotType)
	}
}

func TestClassifyMultipleCodesWithoutLotKeyword(t *testing.T) {
	classifier := New()

	classification, signals := classifier.Classify("OP14-033 OP14-055 OP14-112", "")
	if classification.ProductType != "card_lot" {
		t.Fatalf("expected card_lot for multiple codes, got %q", classification.ProductType)
	}
	if len(signals.CardCodes) != 3 {
		t.Fatalf("expected 3 card codes, got %d", len(signals.CardCodes))
	}
}

func TestClassifyBulkLotWithoutCodes(t *testing.T) {
	classifier := New()

	classification, signals := classifier.Classify("One Piece bulk lot 50 cards", "")
	if classification.ProductType != "card_lot" {
		t.Fatalf("expected card_lot, got %q", classification.ProductType)
	}
	if signals.LotType != "bulk" {
		t.Fatalf("expected lot type 'bulk', got %q", signals.LotType)
	}
}

func TestExtractAllOnePieceCardCodes(t *testing.T) {
	codes := ExtractAllOnePieceCardCodes("OP14-033 and OP14-055 plus P-074")
	if len(codes) != 3 {
		t.Fatalf("expected 3 codes, got %d: %v", len(codes), codes)
	}
}

func TestExtractAllOnePieceCardCodesDeduplicates(t *testing.T) {
	codes := ExtractAllOnePieceCardCodes("OP14-033 OP14-033 OP14-055")
	if len(codes) != 2 {
		t.Fatalf("expected 2 unique codes, got %d: %v", len(codes), codes)
	}
}

func TestDetectLotType(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"card lot one piece", "lot"},
		{"bulk cards for sale", "bulk"},
		{"one piece bundle", "bundle"},
		{"playset of Luffy", "playset"},
		{"single card OP14-033", ""},
	}
	for _, tt := range tests {
		got := DetectLotType(tt.input)
		if got != tt.want {
			t.Errorf("DetectLotType(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDetectUnsealed(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"OP14 Booster Box -unsealed", true},
		{"opened booster box", true},
		{"open box OP14", true},
		{"OP14 Booster Box sealed", false},
		{"OP14-033 Perona", false},
	}
	for _, tt := range tests {
		got := DetectUnsealed(tt.input)
		if got != tt.want {
			t.Errorf("DetectUnsealed(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
