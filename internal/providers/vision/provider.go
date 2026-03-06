package vision

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"sort"
	"image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	_ "image/jpeg"

	"market-api/internal/classify"
	"market-api/internal/models"
)

type Provider struct {
	client       *http.Client
	tesseractBin string
	langs        string
	maxImages    int
}

type ocrObservation struct {
	ImageURL         string   `json:"image_url"`
	Variant          string   `json:"variant"`
	Text             string   `json:"text"`
	DetectedCodes    []string `json:"detected_codes,omitempty"`
	DetectedSetCode  string   `json:"detected_set_code,omitempty"`
	DetectedSetName  string   `json:"detected_set_name,omitempty"`
	DetectedType     string   `json:"detected_sealed_type,omitempty"`
	DetectedLanguage string   `json:"detected_language,omitempty"`
	DetectedQuantity int      `json:"detected_quantity,omitempty"`
	JapaneseChars    int      `json:"japanese_chars"`
	EnglishSignals   []string `json:"english_signals,omitempty"`
	JapaneseHints    []string `json:"japanese_hints,omitempty"`
}

func New(tesseractBin, langs string, maxImages int) *Provider {
	if strings.TrimSpace(tesseractBin) == "" {
		tesseractBin = "tesseract"
	}
	if strings.TrimSpace(langs) == "" {
		langs = "eng+jpn"
	}
	if maxImages < 1 {
		maxImages = 4
	}

	return &Provider{
		client:       &http.Client{Timeout: 20 * time.Second},
		tesseractBin: tesseractBin,
		langs:        langs,
		maxImages:    maxImages,
	}
}

func (p *Provider) AnalyzeOnePiece(ctx context.Context, title, description string, imageURLs []string) (*models.VisionAnalysisResult, error) {
	cleaned := limitImageURLs(imageURLs, p.maxImages)
	if len(cleaned) == 0 {
		return nil, nil
	}

	observations := make([]ocrObservation, 0, len(cleaned)*2)
	warnings := make([]string, 0, len(cleaned))
	codeScore := map[string]int{}
	codeOrder := make([]string, 0, 4)
	setCodeScore := map[string]int{}
	setCodeOrder := make([]string, 0, 4)
	setNameScore := map[string]int{}
	setNameOrder := make([]string, 0, 4)
	sealedTypeScore := map[string]int{}
	sealedTypeOrder := make([]string, 0, 4)
	languageScore := map[string]int{}
	languageOrder := make([]string, 0, 2)
	quantityScore := map[int]int{}
	quantityOrder := make([]int, 0, 2)

	for _, imageURL := range cleaned {
		localPath, err := p.downloadImage(ctx, imageURL)
		if err != nil {
			warnings = append(warnings, "image download failed: "+err.Error())
			continue
		}

		variants, cleanup, err := buildOCRVariants(localPath)
		if err != nil {
			_ = os.Remove(localPath)
			warnings = append(warnings, "image preprocessing failed: "+err.Error())
			continue
		}

		for _, variant := range variants {
			text, err := p.runOCR(ctx, variant.path)
			if err != nil {
				warnings = append(warnings, "image OCR failed: "+err.Error())
				continue
			}

			text = normalizeOCRText(text)
			if text == "" {
				continue
			}

			codes := extractCodes(text)
			signals := extractOCRSignals(text)
			for _, code := range codes {
				if _, ok := codeScore[code]; !ok {
					codeOrder = append(codeOrder, code)
				}
				codeScore[code] += variant.weight
			}
			addWeightedString(setCodeScore, &setCodeOrder, signals.SetCode, variant.weight)
			addWeightedString(setNameScore, &setNameOrder, signals.SetName, variant.weight)
			addWeightedString(sealedTypeScore, &sealedTypeOrder, signals.SealedType, variant.weight)
			addWeightedString(languageScore, &languageOrder, signals.Language, variant.weight)
			addWeightedInt(quantityScore, &quantityOrder, signals.Quantity, variant.weight)

			observations = append(observations, ocrObservation{
				ImageURL:         imageURL,
				Variant:          variant.name,
				Text:             text,
				DetectedCodes:    codes,
				DetectedSetCode:  signals.SetCode,
				DetectedSetName:  signals.SetName,
				DetectedType:     signals.SealedType,
				DetectedLanguage: signals.Language,
				DetectedQuantity: signals.Quantity,
				JapaneseChars:    countJapaneseRunes(text),
				EnglishSignals:   matchedKeywords(strings.ToLower(text), englishCardKeywords),
				JapaneseHints:    matchedKeywords(text, japaneseCardKeywords),
			})
		}

		cleanup()
		_ = os.Remove(localPath)
	}

	selectedCode := selectBestCode(codeScore, codeOrder)
	allCodes := selectAllCodes(codeScore, codeOrder)
	selectedSetCode := selectBestString(setCodeScore, setCodeOrder)
	selectedSetName := selectBestString(setNameScore, setNameOrder)
	selectedSealedType := selectBestString(sealedTypeScore, sealedTypeOrder)
	selectedLanguage := selectBestString(languageScore, languageOrder)
	selectedQuantity := selectBestInt(quantityScore, quantityOrder)
	language, confidence, languageReason := detectLanguage(title, description, observations)

	reasons := make([]string, 0, 2)
	if selectedCode != "" {
		reasons = append(reasons, "detected One Piece card code in image OCR")
	}
	if selectedSetCode != "" {
		reasons = append(reasons, "detected One Piece sealed set code in image OCR")
	}
	if selectedSealedType != "" {
		reasons = append(reasons, "detected sealed packaging type in image OCR")
	}
	if selectedSetName != "" {
		reasons = append(reasons, "detected One Piece set name in image OCR")
	}
	if languageReason != "" {
		reasons = append(reasons, languageReason)
	}

	rawPayload, err := marshalAny(map[string]any{
		"image_urls":    cleaned,
		"title":         title,
		"description":   description,
		"observations":  observations,
		"selected_code": selectedCode,
		"all_codes":     allCodes,
		"set_code":      selectedSetCode,
		"set_name":      selectedSetName,
		"sealed_type":   selectedSealedType,
		"ocr_language":  selectedLanguage,
		"quantity":      selectedQuantity,
		"language":      language,
		"confidence":    confidence,
	})
	if err != nil {
		return nil, err
	}

	normalizedJSON, err := marshalAny(map[string]any{
		"card_code":         selectedCode,
		"set_code":          selectedSetCode,
		"set_name":          selectedSetName,
		"sealed_type":       selectedSealedType,
		"language":          selectedLanguage,
		"quantity":          selectedQuantity,
		"image_language":    language,
		"image_confidence":  confidence,
		"images_considered": len(cleaned),
		"ocr_passes":        len(observations),
	})
	if err != nil {
		return nil, err
	}

	signals := models.Signals{
		CardCode:   selectedCode,
		CardCodes:  allCodes,
		SetCode:    selectedSetCode,
		SetName:    selectedSetName,
		SealedType: selectedSealedType,
		Language:   selectedLanguage,
		Quantity:   selectedQuantity,
	}
	if language != "unknown" {
		signals.ImageLanguage = language
		signals.ImageConfidence = confidence
	}

	return &models.VisionAnalysisResult{
		Signals:  signals,
		Reasons:  reasons,
		Warnings: warnings,
		ProviderResult: models.ProviderResult{
			Provider:       "vision",
			Market:         "unknown",
			Query:          providerQuery(signals, cleaned),
			RawPayload:     rawPayload,
			NormalizedJSON: normalizedJSON,
			Confidence:     confidence,
		},
	}, nil
}

type ocrVariant struct {
	name   string
	path   string
	weight int
}

func buildOCRVariants(localPath string) ([]ocrVariant, func(), error) {
	variants := []ocrVariant{{name: "original", path: localPath, weight: 2}}
	tempFiles := make([]string, 0, 2)
	cleanup := func() {
		for _, path := range tempFiles {
			_ = os.Remove(path)
		}
	}

	file, err := os.Open(localPath)
	if err != nil {
		return variants, cleanup, fmt.Errorf("open image: %w", err)
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return variants, cleanup, nil
	}

	bounds := img.Bounds()
	height := bounds.Dy()
	if height < 200 {
		return variants, cleanup, nil
	}

	bottom := image.Rect(bounds.Min.X, bounds.Min.Y+height*2/3, bounds.Max.X, bounds.Max.Y)
	bottomPath, err := writeCrop(img, bottom, "bottom-third")
	if err == nil {
		tempFiles = append(tempFiles, bottomPath)
		variants = append(variants, ocrVariant{name: "bottom-third", path: bottomPath, weight: 3})
	}

	lowerHalf := image.Rect(bounds.Min.X, bounds.Min.Y+height/2, bounds.Max.X, bounds.Max.Y)
	lowerHalfPath, err := writeCrop(img, lowerHalf, "lower-half")
	if err == nil {
		tempFiles = append(tempFiles, lowerHalfPath)
		variants = append(variants, ocrVariant{name: "lower-half", path: lowerHalfPath, weight: 2})
	}

	return variants, cleanup, nil
}

func writeCrop(img image.Image, rect image.Rectangle, prefix string) (string, error) {
	cropped := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	for y := 0; y < rect.Dy(); y++ {
		for x := 0; x < rect.Dx(); x++ {
			cropped.Set(x, y, img.At(rect.Min.X+x, rect.Min.Y+y))
		}
	}

	file, err := os.CreateTemp("", prefix+"-*.png")
	if err != nil {
		return "", err
	}
	defer file.Close()

	if err := png.Encode(file, cropped); err != nil {
		_ = os.Remove(file.Name())
		return "", err
	}

	return file.Name(), nil
}

func (p *Provider) downloadImage(ctx context.Context, imageURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return "", fmt.Errorf("build request for %s: %w", imageURL, err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", imageURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: status %d", imageURL, resp.StatusCode)
	}

	ext := filepath.Ext(strings.Split(imageURL, "?")[0])
	if ext == "" {
		ext = ".img"
	}

	file, err := os.CreateTemp("", "market-api-vision-*"+ext)
	if err != nil {
		return "", fmt.Errorf("create temp image: %w", err)
	}
	defer file.Close()

	if _, err := io.Copy(file, io.LimitReader(resp.Body, 10<<20)); err != nil {
		_ = os.Remove(file.Name())
		return "", fmt.Errorf("write temp image: %w", err)
	}

	return file.Name(), nil
}

func (p *Provider) runOCR(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, p.tesseractBin, path, "stdout", "-l", p.langs, "--psm", "6")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	return string(output), nil
}

func detectLanguage(title, description string, observations []ocrObservation) (string, float64, string) {
	combined := strings.Builder{}
	totalJapaneseChars := 0
	englishHits := map[string]struct{}{}
	japaneseHits := map[string]struct{}{}

	for _, observation := range observations {
		totalJapaneseChars += observation.JapaneseChars
		combined.WriteString(observation.Text)
		combined.WriteString("\n")
		for _, hit := range observation.EnglishSignals {
			englishHits[hit] = struct{}{}
		}
		for _, hit := range observation.JapaneseHints {
			japaneseHits[hit] = struct{}{}
		}
	}

	metadata := strings.ToLower(strings.TrimSpace(title + " " + description))
	metadataJP := matchedKeywords(metadata, metadataJapaneseKeywords)
	metadataEN := matchedKeywords(metadata, metadataEnglishKeywords)

	jpScore := len(japaneseHits)*3 + len(metadataJP)*4
	if totalJapaneseChars >= 8 {
		jpScore += 5
	} else if totalJapaneseChars > 0 {
		jpScore += 2
	}

	enScore := len(englishHits)*2 + len(metadataEN)*3
	if enScore >= 2 && strings.Contains(strings.ToLower(combined.String()), "don") {
		enScore++
	}

	switch {
	case jpScore >= 4 && jpScore > enScore+1:
		confidence := 0.62 + float64(min(jpScore, 5))*0.07
		if totalJapaneseChars >= 8 {
			confidence += 0.08
		}
		return "jp", minFloat(confidence, 0.95), "image analysis suggests Japanese card printing"
	case enScore >= 4 && enScore >= jpScore:
		confidence := 0.6 + float64(min(enScore, 5))*0.06
		return "en", minFloat(confidence, 0.9), "image analysis suggests English card printing"
	default:
		return "unknown", 0, ""
	}
}

func normalizeOCRText(text string) string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return strings.Join(out, "\n")
}

func extractCodes(text string) []string {
	parts := strings.FieldsFunc(strings.ToUpper(text), func(r rune) bool {
		switch {
		case r >= 'A' && r <= 'Z':
			return false
		case r >= '0' && r <= '9':
			return false
		case r == '-' || r == '_' || r == ' ':
			return false
		default:
			return true
		}
	})

	seen := map[string]struct{}{}
	out := make([]string, 0, 2)
	for _, part := range parts {
		if code := classify.ExtractOnePieceCardCode(part); code != "" {
			if _, ok := seen[code]; ok {
				continue
			}
			seen[code] = struct{}{}
			out = append(out, code)
		}
	}

	slices.Sort(out)
	return out
}

func selectAllCodes(score map[string]int, order []string) []string {
	if len(order) <= 1 {
		return nil
	}
	out := make([]string, len(order))
	copy(out, order)
	sort.Slice(out, func(i, j int) bool {
		return score[out[i]] > score[out[j]]
	})
	return out
}

func selectBestCode(score map[string]int, order []string) string {
	bestCode := ""
	bestScore := 0
	for _, code := range order {
		current := score[code]
		if current > bestScore {
			bestCode = code
			bestScore = current
		}
	}
	return bestCode
}

func providerQuery(signals models.Signals, imageURLs []string) string {
	if signals.CardCode != "" {
		return signals.CardCode
	}
	if signals.SetCode != "" && signals.SealedType != "" {
		return signals.SetCode + " " + strings.ReplaceAll(signals.SealedType, "_", " ")
	}
	if signals.SetName != "" && signals.SealedType != "" {
		return signals.SetName + " " + strings.ReplaceAll(signals.SealedType, "_", " ")
	}
	if signals.SetCode != "" {
		return signals.SetCode
	}
	if signals.SetName != "" {
		return signals.SetName
	}
	if len(imageURLs) > 0 {
		return imageURLs[0]
	}
	return "vision"
}

func limitImageURLs(imageURLs []string, maxImages int) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, min(maxImages, len(imageURLs)))
	for _, imageURL := range imageURLs {
		imageURL = strings.TrimSpace(imageURL)
		if imageURL == "" {
			continue
		}
		if _, ok := seen[imageURL]; ok {
			continue
		}
		seen[imageURL] = struct{}{}
		out = append(out, imageURL)
		if len(out) == maxImages {
			break
		}
	}
	return out
}

func matchedKeywords(text string, keywords []string) []string {
	hits := make([]string, 0, len(keywords))
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			hits = append(hits, keyword)
		}
	}
	return hits
}

func countJapaneseRunes(text string) int {
	count := 0
	for _, r := range text {
		switch {
		case r >= 0x3040 && r <= 0x309f:
			count++
		case r >= 0x30a0 && r <= 0x30ff:
			count++
		case r >= 0x4e00 && r <= 0x9faf:
			count++
		}
	}
	return count
}

func marshalAny(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal vision payload: %w", err)
	}
	return string(data), nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

var englishCardKeywords = []string{
	"character",
	"leader",
	"event",
	"booster box",
	"booster pack",
	"starter deck",
	"double pack",
	"gift collection",
	"collection box",
	"english version",
	"counter",
	"trigger",
	"blocker",
	"activate",
	"on play",
	"once per turn",
	"your turn",
	"rested don",
	"trash this character",
}

var japaneseCardKeywords = []string{
	"キャラ",
	"リーダー",
	"イベント",
	"ブースターパック",
	"ブースター",
	"スタートデッキ",
	"スターターデッキ",
	"カードゲーム",
	"日本語",
	"トリガー",
	"ブロッカー",
	"登場",
	"相手",
	"自分",
	"手札",
}

var metadataJapaneseKeywords = []string{
	"japanese",
	" jp ",
	"japan",
	"日本",
}

var metadataEnglishKeywords = []string{
	"english",
	" eng ",
	"english edition",
}

func extractOCRSignals(text string) models.Signals {
	return models.Signals{
		SetCode:    classify.ExtractOnePieceSetCode(text),
		SetName:    classify.ExtractOnePieceSetName(text),
		SealedType: classify.DetectSealedType(text),
		Language:   classify.DetectLanguage(text),
		Quantity:   classify.DetectQuantity(text),
	}
}

func addWeightedString(score map[string]int, order *[]string, value string, weight int) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if _, ok := score[value]; !ok {
		*order = append(*order, value)
	}
	score[value] += weight
}

func addWeightedInt(score map[int]int, order *[]int, value int, weight int) {
	if value <= 0 {
		return
	}
	if _, ok := score[value]; !ok {
		*order = append(*order, value)
	}
	score[value] += weight
}

func selectBestString(score map[string]int, order []string) string {
	best := ""
	bestScore := 0
	for _, value := range order {
		if score[value] > bestScore {
			best = value
			bestScore = score[value]
		}
	}
	return best
}

func selectBestInt(score map[int]int, order []int) int {
	best := 0
	bestScore := 0
	for _, value := range order {
		if score[value] > bestScore {
			best = value
			bestScore = score[value]
		}
	}
	return best
}
