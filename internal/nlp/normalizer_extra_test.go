package nlp

import "testing"

// TestUnifyChars covers every rune class handled by UnifyChars.
// Critical runes are written as \u escapes to pin the exact code points.
func TestUnifyChars(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"already Persian unchanged", "گوشی همراه", "گوشی همراه"},
		{"Arabic yeh U+064A", "ي", "ی"},
		{"Arabic yeh final U+0649", "دى", "دی"},
		{"Arabic kaf U+0643", "كاف", "کاف"},
		{"Arabic teh marbuta U+0629", "بنت", "بنت"},
		{"Arabic heh with yeh above U+06C0", "\u06C0", "\u0647\u200C\u06CC"},
		{"tatweel U+0640 removed", "اـسـت", "است"},
		{"harakat removed (fatha, shadda, kasra)", "فَصْل", "فصل"},
		{"fathat removed", "بَيت", "بیت"},
		{"mixed conversions", "شركه", "شرکه"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UnifyChars(tt.input)
			if got != tt.expected {
				t.Errorf("UnifyChars(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// TestUnifyCharsExplicitCodePoints pins the mapping for each Arabic rune.
func TestUnifyCharsExplicitCodePoints(t *testing.T) {
	input := "\u064A\u0649\u0643\u0629\u06C0\u0640\u064B\u064C\u064D\u064E\u064F\u0650\u0651\u0652"
	// yeh->ی, yeh final->ی, kaf->ک, teh marbuta->ه, heh-with-yeh->ه+ZWNJ+ی,
	// tatweel and all harakat removed.
	want := "\u06CC\u06CC\u06A9\u0647\u0647\u200C\u06CC"
	got := UnifyChars(input)
	if got != want {
		t.Errorf("UnifyChars() = %q (runes %q), want %q (runes %q)", got, []rune(got), want, []rune(want))
	}
}

// TestNormalizeWithOptionsEmptyInput covers the empty-text fast path.
func TestNormalizeWithOptionsEmptyInput(t *testing.T) {
	for _, opts := range []NormalizerOptions{DefaultOptions(), {}} {
		if got := NormalizeWithOptions("", opts); got != "" {
			t.Errorf("NormalizeWithOptions(\"\") = %q, want \"\"", got)
		}
	}
}

// TestNormalizeWithOptionsZeroOptions covers all option flags disabled.
func TestNormalizeWithOptionsZeroOptions(t *testing.T) {
	input := "  ي   ۱۲۳\n\n\n\n  "
	got := NormalizeWithOptions(input, NormalizerOptions{})
	if got != input {
		t.Errorf("NormalizeWithOptions with zero options modified input: got %q, want %q", got, input)
	}
}

// TestNormalizeWithOptionsDefaultOptions exercises every enabled option at once.
func TestNormalizeWithOptionsDefaultOptions(t *testing.T) {
	// Guillemets, Arabic yeh with fathat+tatweel+kaf, Arabic+Persian
	// digits, ZWNJ-space, double ZWNJ, excess spaces and 4 newlines.
	// All Persian runes are pinned with \u escapes to avoid ambiguity:
	//   آيفون = alif-madda, yeh, fe, waw, noon
	//   يَـك   = arabic yeh (064A), fathat (064E), tatweel (0640), kaf (0643)
	//   ٥۵۵    = arabic 5 (0665), persian 5 (06F5), persian 5 (06F5)
	input := "\u00AB\u0622\u06CC\u0641\u0648\u0646\u00BB \u064A\u064E\u0640\u0643 \u0665\u06F5\u06F5 \u200c \u200c\u200c \n\n\n\n"
	// Expected: guillemets -> "; yeh -> \u06CC, fathat+tatweel removed,
	// kaf -> \u06A9; ٥ -> 5; ZWNJ-space and double ZWNJ collapse to a single
	// space; excess spaces and 4 newlines collapse; TrimSpace trims ends.
	want := "\"\u0622\u06CC\u0641\u0648\u0646\" \u06CC\u06A9 555"
	got := NormalizeWithOptions(input, DefaultOptions())
	if got != want {
		t.Errorf("NormalizeWithOptions() = %q, want %q", got, want)
	}
}
