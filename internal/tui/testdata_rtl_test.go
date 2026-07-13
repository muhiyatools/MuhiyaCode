package tui

// Shared Arabic/RTL test corpus (feature 006 T002). One place so every story's
// tests exercise the same representative inputs: plain/vocalized Arabic, the
// LAM-ALEF ligature, mixed Arabic+English/code/number/path/URL, an English-
// dominant line, pure LTR, long/wrapping, emoji, Arabic-Indic digits, and a
// defective leading-combining-mark sequence.
var (
	corpusPlain     = "مرحبا بالعالم"
	corpusVocalized = "مَرْحَبًا"
	corpusLamAlef   = "لا اله الا الله"
	corpusMixedFile = "افتح الملف main.go الان"
	corpusMixedPath = "عدل internal/tui/rtl.go بسرعة"
	corpusMixedCode = "شغل `go test` من فضلك"
	corpusMixedNum  = "السطر رقم 42 هنا"
	corpusMixedURL  = "زر https://muhiya.com الان"
	corpusEngDom    = "run the امر command now"
	corpusLTROnly   = "func main() { return nil }"
	corpusLong      = "هذا سطر عربي طويل جدا يحتاج الى الالتفاف عبر عدة اسطر في الطرفية لاختبار المحاذاة والتشكيل الصحيح للنص"
	corpusEmoji     = "مرحبا 🌍 بالعالم"
	corpusDigitsAN  = "الرقم ٤٢ هنا"
	corpusDefective = "َّمرحبا"

	// rtlCorpus is every sample, for fuzz/robustness sweeps.
	rtlCorpus = []string{
		corpusPlain, corpusVocalized, corpusLamAlef, corpusMixedFile, corpusMixedPath,
		corpusMixedCode, corpusMixedNum, corpusMixedURL, corpusEngDom, corpusLTROnly,
		corpusLong, corpusEmoji, corpusDigitsAN, corpusDefective, "", " ", "َ",
	}
)
