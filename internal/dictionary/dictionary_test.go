package dictionary

import (
	"strings"
	"testing"
)

func TestOfflineDictionary(t *testing.T) {
	for _, word := range []string{"dictionary", "serendipity", "engineering", "children", "running", "better", "ice cream"} {
		entry, err := Lookup(word)
		if err != nil || len(entry.Senses) == 0 {
			t.Fatalf("%s: %v, %+v", word, err, entry)
		}
		if entry.Source != "WordNet 3.1" || entry.Senses[0].Definition == "" {
			t.Fatalf("missing definition for %s", word)
		}
	}
	entry, err := Lookup("gnotesunknownword")
	if err != nil || len(entry.Senses) != 0 {
		t.Fatal("unknown words should return an empty definition list")
	}
	entry, err = Lookup("dictionary")
	if err != nil || !strings.Contains(entry.Senses[0].Definition, "reference book") {
		t.Fatalf("dictionary sense order: %+v, %v", entry, err)
	}
}

func TestNormalize(t *testing.T) {
	if got, err := Normalize("  CHILDREN  "); err != nil || got != "children" {
		t.Fatal(got, err)
	}
	for _, word := range []string{"", "<script>", "123", strings.Repeat("a", 97), "a b c d e f g"} {
		if _, err := Normalize(word); err == nil {
			t.Fatalf("accepted %q", word)
		}
	}
}
