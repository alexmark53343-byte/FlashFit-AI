package main

import "testing"

func TestEveryLanguageHasEveryEnglishKey(t *testing.T) {
	for code, lang := range textByLanguage {
		for key := range textByLanguage["en"] {
			if lang[key] == "" {
				t.Errorf("language %s is missing %s", code, key)
			}
		}
	}
	for code, lang := range premiumTextByLanguage {
		for key := range premiumTextByLanguage["en"] {
			if lang[key] == "" {
				t.Errorf("premium language %s is missing %s", code, key)
			}
		}
	}
}

func TestNormalizeLanguage(t *testing.T) {
	for in, want := range map[string]string{"it-IT": "it", "EN_us": "en", "fr": "fr", "xx": "it"} {
		if got := normalizeLanguage(in); got != want {
			t.Fatalf("normalizeLanguage(%q)=%q want %q", in, got, want)
		}
	}
}
