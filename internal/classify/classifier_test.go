package classify

import "testing"

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
