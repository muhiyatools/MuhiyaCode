package tui

import (
	"regexp"
	"strings"
	"unicode"
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

// TerminalBiDiControl returns the BDSM (Bi-Directional Support Mode) control
// sequence MuhiyaCode emits at startup for the given RTL mode (feature 006 T033,
// research R7). In auto/visual the app owns BiDi, so it emits explicit mode
// (CSI 8 l) to stop a BiDi-capable terminal from double-reversing already-visual
// output; in native the terminal owns BiDi (implicit, CSI 8 h). off emits nothing.
// The sequence is ignored by BiDi-agnostic terminals, so it is safe everywhere.
func TerminalBiDiControl(mode string) string {
	switch mode {
	case "off":
		return ""
	case "native":
		return "\x1b[8h"
	default: // auto / visual
		return "\x1b[8l"
	}
}

// CopyRoundTrip renders logical to its visual form and recovers logical from it,
// reporting whether the round-trip preserved the text (feature 006; used by the
// `doctor` diagnostics to confirm clipboard fidelity).
func CopyRoundTrip(logical, mode string) (recovered string, ok bool) {
	recovered = recoverLogical(renderForDisplay(logical, mode, "auto").Visual)
	return recovered, recovered == logical
}

// RenderRTL is the back-compat wrapper over the centralized display pass; it
// returns only the visual string (no alignment), matching its historic callers.
// New code uses renderForDisplay directly to also get alignment.
func RenderRTL(value, mode string) string {
	return renderForDisplay(value, mode, "left").Visual
}

// renderForDisplay is the single, centralized RTL display pass (feature 006 T008,
// contracts/rtl-render.md). It turns one LOGICAL line into a DisplayLine: the
// visual string for the screen plus the resolved alignment. It is a pure function
// — no timestamps, no locale calls — so identical inputs give byte-identical
// output. LTR-only and mode=off are byte-identical no-ops; mode=native emits
// logical text (the terminal reorders); visual/auto shape + reorder per run with
// grapheme-aware reversal so Arabic reads right-to-left while LTR runs stay intact.
func renderForDisplay(logical, mode, align string) DisplayLine {
	if mode == "off" || !IsRTL(logical) {
		return DisplayLine{Visual: logical, Logical: logical, Align: "left"}
	}
	a := resolveAlign(logical, align)
	if mode == "native" {
		// The terminal's own BiDi engine reorders/shapes; the app emits logical text.
		return DisplayLine{Visual: logical, Logical: logical, Align: a}
	}
	// visual / auto (the reliable default across Windows Terminal and legacy
	// consoles): split into directional runs, order them for the screen (RTL base ⇒
	// runs run right-to-left), and shape+reverse only RTL runs — grapheme-aware, so
	// a base letter keeps its harakat (unlike bidi.ReverseString, Go #50633) — while
	// LTR runs (paths, code, f(x), numbers, English) stay intact.
	segs := segmentRuns(logical)
	rtlBase := dominantRTL(logical)
	var b strings.Builder
	emit := func(seg dirRun) {
		if seg.rtl {
			b.WriteString(reverseGraphemes(shapeArabic(seg.text)))
		} else {
			b.WriteString(seg.text)
		}
	}
	if rtlBase {
		for i := len(segs) - 1; i >= 0; i-- {
			emit(segs[i])
		}
	} else {
		for _, seg := range segs {
			emit(seg)
		}
	}
	return DisplayLine{Visual: b.String(), Logical: logical, Align: a}
}

func shapeArabic(value string) string {
	runes := []rune(value)
	out := make([]rune, 0, len(runes))
	for index := 0; index < len(runes); index++ {
		char := runes[index]
		// LAM + ALEF is a mandatory ligature: two logical runes render as one glyph.
		// It takes the final form when the LAM connects to a preceding letter, else
		// the isolated form.
		if char == 'ل' && index+1 < len(runes) { // LAM
			if lig, ok := lamAlefLigature(runes[index+1]); ok {
				if prev, hasPrev := arabic[previousArabic(runes, index)]; hasPrev && prev.initial != 0 {
					out = append(out, lig.final)
				} else {
					out = append(out, lig.isolated)
				}
				index++ // consume the ALEF
				continue
			}
		}
		forms, ok := arabic[char]
		if !ok {
			out = append(out, char)
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
			out = append(out, forms.medial)
		case connectPrevious:
			out = append(out, forms.final)
		case connectNext:
			out = append(out, forms.initial)
		default:
			out = append(out, forms.isolated)
		}
	}
	return string(out)
}

// lamAlef holds the isolated and final presentation forms of a LAM+ALEF ligature.
type lamAlef struct{ isolated, final rune }

// lamAlefLigature returns the ligature forms when alef is an ALEF variant that
// combines with a preceding LAM (ok=false otherwise). Covers plain ALEF and the
// madda/hamza-above/hamza-below variants (U+FEF5–U+FEFC).
func lamAlefLigature(alef rune) (lamAlef, bool) {
	switch alef {
	case 'ا': // ALEF
		return lamAlef{'ﻻ', 'ﻼ'}, true
	case 'آ': // ALEF WITH MADDA ABOVE
		return lamAlef{'ﻵ', 'ﻶ'}, true
	case 'أ': // ALEF WITH HAMZA ABOVE
		return lamAlef{'ﻷ', 'ﻸ'}, true
	case 'إ': // ALEF WITH HAMZA BELOW
		return lamAlef{'ﻹ', 'ﻺ'}, true
	}
	return lamAlef{}, false
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
