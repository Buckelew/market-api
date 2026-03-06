package vision

import (
	"image"
	"strings"
	"testing"
)

func TestExtractCodes(t *testing.T) {
	t.Parallel()

	text := "ONE PIECE CARD GAME\nSabo\nPRB02-014\nCounter +2000\nP-053"
	got := extractCodes(text)

	if len(got) != 2 {
		t.Fatalf("extractCodes() length = %d, want 2 (%v)", len(got), got)
	}
	if got[0] != "P-053" || got[1] != "PRB02-014" {
		t.Fatalf("extractCodes() = %v, want [P-053 PRB02-014]", got)
	}
}

func TestDetectLanguageEnglish(t *testing.T) {
	t.Parallel()

	observations := []ocrObservation{
		{
			Text:           "On Play\nYou may trash this Character.\nGive up to 2 rested DON!! cards.\nBoa Hancock\nOP12-014",
			JapaneseChars:  0,
			EnglishSignals: []string{"on play", "character", "rested don", "trash this character"},
		},
	}

	lang, confidence, reason := detectLanguage("Boa Hancock OP12-014 English Edition", "", observations)
	if lang != "en" {
		t.Fatalf("detectLanguage() lang = %q, want en", lang)
	}
	if confidence < 0.7 {
		t.Fatalf("detectLanguage() confidence = %f, want >= 0.7", confidence)
	}
	if reason == "" {
		t.Fatal("detectLanguage() reason was empty")
	}
}

func TestDetectLanguageJapanese(t *testing.T) {
	t.Parallel()

	observations := []ocrObservation{
		{
			Text:          "ST21-014",
			JapaneseChars: 0,
		},
	}

	lang, confidence, reason := detectLanguage("one piece magazine vol 20 Japanese promo card game monkey D Luffy SR ST21-014", "", observations)
	if lang != "jp" {
		t.Fatalf("detectLanguage() lang = %q, want jp", lang)
	}
	if confidence < 0.7 {
		t.Fatalf("detectLanguage() confidence = %f, want >= 0.7", confidence)
	}
	if reason == "" {
		t.Fatal("detectLanguage() reason was empty")
	}
}

func TestExtractOCRSignalsBoosterBox(t *testing.T) {
	t.Parallel()

	text := "ONE PIECE CARD GAME\nRoyal Blood\nOP-10\nBooster Box\n24 packs per box"
	got := extractOCRSignals(text)

	if got.SetCode != "OP10" {
		t.Fatalf("extractOCRSignals() set code = %q, want OP10", got.SetCode)
	}
	if got.SetName != "Royal Blood" {
		t.Fatalf("extractOCRSignals() set name = %q, want Royal Blood", got.SetName)
	}
	if got.SealedType != "booster_box" {
		t.Fatalf("extractOCRSignals() sealed type = %q, want booster_box", got.SealedType)
	}
	if got.Quantity != 24 {
		t.Fatalf("extractOCRSignals() quantity = %d, want 24", got.Quantity)
	}
}

func TestExtractOCRSignalsStarterDeck(t *testing.T) {
	t.Parallel()

	text := "ST-21\nStarter Deck\nEnglish Version"
	got := extractOCRSignals(text)

	if got.SetCode != "ST21" {
		t.Fatalf("extractOCRSignals() set code = %q, want ST21", got.SetCode)
	}
	if got.SealedType != "starter_deck" {
		t.Fatalf("extractOCRSignals() sealed type = %q, want starter_deck", got.SealedType)
	}
	if got.Language != "en" {
		t.Fatalf("extractOCRSignals() language = %q, want en", got.Language)
	}
}

func TestDetectLanguageJapanesePackaging(t *testing.T) {
	t.Parallel()

	text := "ONE PIECEカードゲーム\nブースターパック\n日本語版\nOP-10"
	observations := []ocrObservation{
		{
			Text:           text,
			JapaneseChars:  countJapaneseRunes(text),
			EnglishSignals: matchedKeywords(strings.ToLower(text), englishCardKeywords),
			JapaneseHints:  matchedKeywords(text, japaneseCardKeywords),
		},
	}

	lang, confidence, reason := detectLanguage("one piece booster box", "", observations)
	if lang != "jp" {
		t.Fatalf("detectLanguage() lang = %q, want jp", lang)
	}
	if confidence < 0.7 {
		t.Fatalf("detectLanguage() confidence = %f, want >= 0.7", confidence)
	}
	if reason == "" {
		t.Fatal("detectLanguage() reason was empty")
	}
}

func TestResizeToMaxDimensionShrinksLargeImages(t *testing.T) {
	t.Parallel()

	img := image.NewRGBA(image.Rect(0, 0, 4000, 2000))
	resized, changed := resizeToMaxDimension(img, 1000)
	if !changed {
		t.Fatal("resizeToMaxDimension() changed = false, want true")
	}
	if resized.Bounds().Dx() != 1000 {
		t.Fatalf("resizeToMaxDimension() width = %d, want 1000", resized.Bounds().Dx())
	}
	if resized.Bounds().Dy() != 500 {
		t.Fatalf("resizeToMaxDimension() height = %d, want 500", resized.Bounds().Dy())
	}
}

func TestResizeToMaxDimensionKeepsSmallImages(t *testing.T) {
	t.Parallel()

	img := image.NewRGBA(image.Rect(0, 0, 800, 600))
	resized, changed := resizeToMaxDimension(img, 1000)
	if changed {
		t.Fatal("resizeToMaxDimension() changed = true, want false")
	}
	if resized.Bounds().Dx() != 800 || resized.Bounds().Dy() != 600 {
		t.Fatalf("resizeToMaxDimension() bounds = %v, want 800x600", resized.Bounds())
	}
}
