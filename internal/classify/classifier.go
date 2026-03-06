package classify

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"market-api/internal/models"
)

var cardCodePattern = regexp.MustCompile(`(?i)\b((?:OP|ST|EB|PRB)\s?\d{1,2}|P)[-\s_]?(\d{2,3})\b`)
var setCodePattern = regexp.MustCompile(`(?i)\b(OP|ST|EB|PRB)[-\s]?(\d{1,2})\b`)
var quantityPattern = regexp.MustCompile(`(?i)\b(\d+)\s*(?:x|×)?\s*(box|boxes|pack|packs|deck|decks|collection|collections)\b`)
var lotPattern = regexp.MustCompile(`(?i)\b(lot|bulk|bundle|playset|set of|collection of)\b`)
var unsealedPattern = regexp.MustCompile(`(?i)\b(unsealed|opened|open box|no cards|cards removed|empty)\b`)

type setNameAlias struct {
	Canonical string
	Aliases   []string
}

var onePieceSetNameAliases = []setNameAlias{
	{Canonical: "Royal Blood", Aliases: []string{"royal blood"}},
	{Canonical: "Romance Dawn", Aliases: []string{"romance dawn"}},
	{Canonical: "Paramount War", Aliases: []string{"paramount war"}},
	{Canonical: "Pillars of Strength", Aliases: []string{"pillars of strength"}},
	{Canonical: "Kingdoms of Intrigue", Aliases: []string{"kingdoms of intrigue"}},
	{Canonical: "Awakening of the New Era", Aliases: []string{"awakening of the new era"}},
	{Canonical: "Wings of the Captain", Aliases: []string{"wings of the captain"}},
	{Canonical: "500 Years in the Future", Aliases: []string{"500 years in the future"}},
	{Canonical: "Two Legends", Aliases: []string{"two legends"}},
	{Canonical: "The Three Brothers", Aliases: []string{"the three brothers"}},
	{Canonical: "Emperors in the New World", Aliases: []string{"emperors in the new world"}},
	{Canonical: "Memorial Collection", Aliases: []string{"memorial collection"}},
	{Canonical: "Premium Booster The Best Vol. 2", Aliases: []string{"premium booster the best vol 2", "premium booster the best volume 2", "the best vol 2"}},
}

type Classifier struct{}

func New() *Classifier {
	return &Classifier{}
}

func (c *Classifier) Classify(title, description string) (models.Classification, models.Signals) {
	combined := strings.TrimSpace(title + " " + description)
	lowered := strings.ToLower(combined)
	reasons := make([]string, 0, 2)
	signals := models.Signals{}

	allCodes := ExtractAllOnePieceCardCodes(combined)
	if len(allCodes) > 0 {
		signals.CardCode = allCodes[0]
		if len(allCodes) > 1 {
			signals.CardCodes = allCodes
		}
		reasons = append(reasons, "detected One Piece card code in listing text")
	}
	if setCode := ExtractOnePieceSetCode(combined); setCode != "" {
		signals.SetCode = setCode
	}
	if setName := ExtractOnePieceSetName(combined); setName != "" {
		signals.SetName = setName
	}
	signals.SealedType = detectSealedType(combined)
	signals.LotType = detectLotType(combined)
	signals.Language = detectLanguage(combined)
	signals.Quantity = detectQuantity(combined)
	isUnsealed := detectUnsealed(combined)

	hasOnePiece := strings.Contains(lowered, "one piece")
	hasCardTerms := strings.Contains(lowered, "tcg") || strings.Contains(lowered, "trading card") || strings.Contains(lowered, "single")

	switch {
	case signals.SealedType != "" && isUnsealed:
		reasons = append(reasons, "listing appears to be an unsealed/opened product")
		return models.Classification{
			ProductType: "unknown",
			Category:    "unknown",
			Market:      "unknown",
			Confidence:  0.15,
			Reasons:     reasons,
		}, signals
	case signals.LotType != "" && len(signals.CardCodes) > 1:
		reasons = append(reasons, "listing appears to be a card lot with multiple identifiable cards")
		return models.Classification{
			ProductType: "card_lot",
			Category:    "one_piece_tcg",
			Market:      "tcgplayer",
			Confidence:  0.90,
			Reasons:     reasons,
		}, signals
	case signals.LotType != "" && (hasOnePiece || signals.SetCode != ""):
		reasons = append(reasons, "listing appears to be a card lot")
		return models.Classification{
			ProductType: "card_lot",
			Category:    "one_piece_tcg",
			Market:      "tcgplayer",
			Confidence:  0.70,
			Reasons:     reasons,
		}, signals
	case signals.CardCode != "" && len(signals.CardCodes) <= 1:
		return models.Classification{
			ProductType: "trading_card",
			Category:    "one_piece_tcg",
			Market:      "tcgplayer",
			Confidence:  0.97,
			Reasons:     reasons,
		}, signals
	case signals.CardCode != "" && len(signals.CardCodes) > 1:
		reasons = append(reasons, "detected multiple card codes in listing text")
		return models.Classification{
			ProductType: "card_lot",
			Category:    "one_piece_tcg",
			Market:      "tcgplayer",
			Confidence:  0.88,
			Reasons:     reasons,
		}, signals
	case signals.SealedType != "" && (hasOnePiece || signals.SetCode != ""):
		reasons = append(reasons, "listing matches a One Piece sealed product pattern")
		if signals.SetCode != "" {
			reasons = append(reasons, "detected One Piece set code for sealed product")
		}
		confidence := 0.84
		if signals.SetCode != "" {
			confidence = 0.95
		}
		return models.Classification{
			ProductType: "sealed_product",
			Category:    "one_piece_tcg",
			Market:      "tcgplayer",
			Confidence:  confidence,
			Reasons:     reasons,
		}, signals
	case hasOnePiece && hasCardTerms:
		reasons = append(reasons, "listing mentions One Piece and card-related terms")
		return models.Classification{
			ProductType: "trading_card",
			Category:    "one_piece_tcg",
			Market:      "tcgplayer",
			Confidence:  0.84,
			Reasons:     reasons,
		}, signals
	case hasOnePiece:
		reasons = append(reasons, "listing mentions One Piece")
		return models.Classification{
			ProductType: "trading_card",
			Category:    "one_piece_tcg",
			Market:      "tcgplayer",
			Confidence:  0.66,
			Reasons:     reasons,
		}, signals
	default:
		return models.Classification{
			ProductType: "unknown",
			Category:    "unknown",
			Market:      "unknown",
			Confidence:  0.15,
			Reasons:     []string{"insufficient textual signals"},
		}, signals
	}
}

func ExtractOnePieceSetCode(text string) string {
	match := setCodePattern.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(text)))
	if len(match) != 3 {
		return ""
	}

	numberValue, err := strconv.Atoi(match[2])
	if err != nil {
		return ""
	}

	return fmt.Sprintf("%s%02d", strings.ToUpper(match[1]), numberValue)
}

func ExtractOnePieceCardCode(text string) string {
	match := cardCodePattern.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(text)))
	if len(match) != 3 {
		return ""
	}

	rawSet := strings.ReplaceAll(match[1], " ", "")
	numberValue, err := strconv.Atoi(match[2])
	if err != nil {
		return ""
	}

	if rawSet == "P" {
		return fmt.Sprintf("P-%03d", numberValue)
	}

	prefixEnd := 0
	for prefixEnd < len(rawSet) && rawSet[prefixEnd] >= 'A' && rawSet[prefixEnd] <= 'Z' {
		prefixEnd++
	}
	if prefixEnd == 0 || prefixEnd == len(rawSet) {
		return ""
	}

	setValue, err := strconv.Atoi(rawSet[prefixEnd:])
	if err != nil {
		return ""
	}

	return fmt.Sprintf("%s%02d-%03d", rawSet[:prefixEnd], setValue, numberValue)
}

func ExtractOnePieceSetName(text string) string {
	normalized := normalizeSetNameText(text)
	if normalized == "" {
		return ""
	}

	for _, candidate := range onePieceSetNameAliases {
		for _, alias := range candidate.Aliases {
			if strings.Contains(normalized, normalizeSetNameText(alias)) {
				return candidate.Canonical
			}
		}
	}

	return ""
}

func DetectSealedType(text string) string {
	return detectSealedType(text)
}

func DetectLanguage(text string) string {
	return detectLanguage(text)
}

func DetectQuantity(text string) int {
	return detectQuantity(text)
}

func detectSealedType(text string) string {
	lowered := strings.ToLower(strings.TrimSpace(text))

	switch {
	case strings.Contains(lowered, "gift collection"):
		return "gift_collection"
	case strings.Contains(lowered, "collection box"):
		return "collection_box"
	case strings.Contains(lowered, "double pack"):
		return "double_pack"
	case strings.Contains(lowered, "starter deck") || strings.Contains(lowered, "starter ") || strings.Contains(lowered, " deck"):
		return "starter_deck"
	case strings.Contains(lowered, "booster box") || strings.Contains(lowered, "sealed box") || strings.Contains(lowered, "display box"):
		return "booster_box"
	case strings.Contains(lowered, "booster pack") || strings.Contains(lowered, " pack"):
		return "booster_pack"
	default:
		return ""
	}
}

func detectLanguage(text string) string {
	lowered := " " + strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(text))), " ") + " "

	switch {
	case strings.Contains(lowered, " japanese ") || strings.Contains(lowered, " japanese version ") || strings.Contains(lowered, " jp "):
		return "jp"
	case strings.Contains(lowered, " english ") || strings.Contains(lowered, " english version ") || strings.Contains(lowered, " eng "):
		return "en"
	default:
		return ""
	}
}

func detectQuantity(text string) int {
	match := quantityPattern.FindStringSubmatch(strings.ToLower(text))
	if len(match) != 3 {
		return 0
	}

	value, err := strconv.Atoi(match[1])
	if err != nil {
		return 0
	}

	return value
}

func ExtractAllOnePieceCardCodes(text string) []string {
	upper := strings.ToUpper(strings.TrimSpace(text))
	matches := cardCodePattern.FindAllStringSubmatch(upper, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := map[string]struct{}{}
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) != 3 {
			continue
		}
		rawSet := strings.ReplaceAll(match[1], " ", "")
		numberValue, err := strconv.Atoi(match[2])
		if err != nil {
			continue
		}
		var code string
		if rawSet == "P" {
			code = fmt.Sprintf("P-%03d", numberValue)
		} else {
			prefixEnd := 0
			for prefixEnd < len(rawSet) && rawSet[prefixEnd] >= 'A' && rawSet[prefixEnd] <= 'Z' {
				prefixEnd++
			}
			if prefixEnd == 0 || prefixEnd == len(rawSet) {
				continue
			}
			setValue, err := strconv.Atoi(rawSet[prefixEnd:])
			if err != nil {
				continue
			}
			code = fmt.Sprintf("%s%02d-%03d", rawSet[:prefixEnd], setValue, numberValue)
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	return out
}

func DetectLotType(text string) string {
	return detectLotType(text)
}

func DetectUnsealed(text string) bool {
	return detectUnsealed(text)
}

func detectLotType(text string) string {
	match := lotPattern.FindStringSubmatch(strings.ToLower(strings.TrimSpace(text)))
	if len(match) < 2 {
		return ""
	}
	switch strings.ToLower(match[1]) {
	case "lot":
		return "lot"
	case "bulk":
		return "bulk"
	case "bundle":
		return "bundle"
	case "playset":
		return "playset"
	case "set of", "collection of":
		return "lot"
	default:
		return ""
	}
}

func detectUnsealed(text string) bool {
	return unsealedPattern.MatchString(strings.ToLower(strings.TrimSpace(text)))
}

func normalizeSetNameText(text string) string {
	replacer := strings.NewReplacer("-", " ", "_", " ", "/", " ", "(", " ", ")", " ", ",", " ", ".", " ", "'", "")
	return strings.Join(strings.Fields(strings.ToLower(replacer.Replace(text))), " ")
}
