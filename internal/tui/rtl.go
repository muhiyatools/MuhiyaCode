package tui

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/bidi"
)

var rtlText = regexp.MustCompile(`[\x{0590}-\x{08FF}\x{FB1D}-\x{FDFF}\x{FE70}-\x{FEFF}]`)

type arabicForms struct{ isolated, final, initial, medial rune }

var arabic = map[rune]arabicForms{
	'ء': {'ﺀ', 'ﺀ', 0, 0}, 'آ': {'ﺁ', 'ﺂ', 0, 0}, 'أ': {'ﺃ', 'ﺄ', 0, 0}, 'ؤ': {'ﺅ', 'ﺆ', 0, 0}, 'إ': {'ﺇ', 'ﺈ', 0, 0},
	'ئ': {'ﺉ', 'ﺊ', 'ﺋ', 'ﺌ'}, 'ا': {'ﺍ', 'ﺎ', 0, 0}, 'ب': {'ﺏ', 'ﺐ', 'ﺑ', 'ﺒ'}, 'ة': {'ﺓ', 'ﺔ', 0, 0}, 'ت': {'ﺕ', 'ﺖ', 'ﺗ', 'ﺘ'},
	'ث': {'ﺙ', 'ﺚ', 'ﺛ', 'ﺜ'}, 'ج': {'ﺝ', 'ﺞ', 'ﺟ', 'ﺠ'}, 'ح': {'ﺡ', 'ﺢ', 'ﺣ', 'ﺤ'}, 'خ': {'ﺥ', 'ﺦ', 'ﺧ', 'ﺨ'}, 'د': {'ﺩ', 'ﺪ', 0, 0},
	'ذ': {'ﺫ', 'ﺬ', 0, 0}, 'ر': {'ﺭ', 'ﺮ', 0, 0}, 'ز': {'ﺯ', 'ﺰ', 0, 0}, 'س': {'ﺱ', 'ﺲ', 'ﺳ', 'ﺴ'}, 'ش': {'ﺵ', 'ﺶ', 'ﺷ', 'ﺸ'},
	'ص': {'ﺹ', 'ﺺ', 'ﺻ', 'ﺼ'}, 'ض': {'ﺽ', 'ﺾ', 'ﺿ', 'ﻀ'}, 'ط': {'ﻁ', 'ﻂ', 'ﻃ', 'ﻄ'}, 'ظ': {'ﻅ', 'ﻆ', 'ﻇ', 'ﻈ'}, 'ع': {'ﻉ', 'ﻊ', 'ﻋ', 'ﻌ'},
	'غ': {'ﻍ', 'ﻎ', 'ﻏ', 'ﻐ'}, 'ف': {'ﻑ', 'ﻒ', 'ﻓ', 'ﻔ'}, 'ق': {'ﻕ', 'ﻖ', 'ﻗ', 'ﻘ'}, 'ك': {'ﻙ', 'ﻚ', 'ﻛ', 'ﻜ'}, 'ل': {'ﻝ', 'ﻞ', 'ﻟ', 'ﻠ'},
	'م': {'ﻡ', 'ﻢ', 'ﻣ', 'ﻤ'}, 'ن': {'ﻥ', 'ﻦ', 'ﻧ', 'ﻨ'}, 'ه': {'ﻩ', 'ﻪ', 'ﻫ', 'ﻬ'}, 'و': {'ﻭ', 'ﻮ', 0, 0}, 'ى': {'ﻯ', 'ﻰ', 0, 0},
	'ي': {'ﻱ', 'ﻲ', 'ﻳ', 'ﻴ'}, 'پ': {'ﭖ', 'ﭗ', 'ﭘ', 'ﭙ'}, 'چ': {'ﭺ', 'ﭻ', 'ﭼ', 'ﭽ'}, 'ژ': {'ﮊ', 'ﮋ', 0, 0}, 'ک': {'ﮎ', 'ﮏ', 'ﮐ', 'ﮑ'}, 'گ': {'ﮒ', 'ﮓ', 'ﮔ', 'ﮕ'},
}

func IsRTL(value string) bool { return rtlText.MatchString(value) }

func RenderRTL(value, mode string) string {
	if mode == "off" || mode == "native" || !IsRTL(value) {
		return value
	}
	// auto intentionally uses the visual fallback: this is the most reliable
	// default across Windows Terminal and legacy console/font combinations.
	shaped := shapeArabic(value)
	var paragraph bidi.Paragraph
	if _, err := paragraph.SetString(shaped, bidi.DefaultDirection(bidi.RightToLeft)); err != nil {
		return shaped
	}
	order, err := paragraph.Order()
	if err != nil {
		return shaped
	}
	var result strings.Builder
	for index := 0; index < order.NumRuns(); index++ {
		run := order.Run(index)
		text := run.String()
		if run.Direction() == bidi.RightToLeft {
			text = bidi.ReverseString(text)
		}
		result.WriteString(text)
	}
	return result.String()
}

func shapeArabic(value string) string {
	runes := []rune(value)
	result := append([]rune(nil), runes...)
	for index, char := range runes {
		forms, ok := arabic[char]
		if !ok {
			continue
		}
		previous := previousArabic(runes, index)
		next := nextArabic(runes, index)
		previousForms, hasPrevious := arabic[previous]
		_, hasNext := arabic[next]
		connectPrevious := hasPrevious && previousForms.initial != 0
		connectNext := hasNext && forms.initial != 0
		switch {
		case connectPrevious && connectNext && forms.medial != 0:
			result[index] = forms.medial
		case connectPrevious:
			result[index] = forms.final
		case connectNext:
			result[index] = forms.initial
		default:
			result[index] = forms.isolated
		}
	}
	return string(result)
}

func previousArabic(runes []rune, index int) rune {
	for i := index - 1; i >= 0; i-- {
		if unicode.Is(unicode.Mn, runes[i]) {
			continue
		}
		if _, ok := arabic[runes[i]]; ok {
			return runes[i]
		}
		break
	}
	return 0
}

func nextArabic(runes []rune, index int) rune {
	for i := index + 1; i < len(runes); i++ {
		if unicode.Is(unicode.Mn, runes[i]) {
			continue
		}
		if _, ok := arabic[runes[i]]; ok {
			return runes[i]
		}
		break
	}
	return 0
}
