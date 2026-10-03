package blocknote2md

import (
	"strings"
	"testing"
)

func TestConvert_EmptyDocuments(t *testing.T) {
	for _, in := range []string{"", "  ", "null", "[]", " [ ] "} {
		got, err := Convert([]byte(in))
		if err != nil || got != "" {
			t.Errorf("Convert(%q) = %q, %v; want empty, nil", in, got, err)
		}
	}
}

func TestConvert_RejectsNonArrays(t *testing.T) {
	for _, in := range []string{"{}", `"text"`, "42", "[", `{"type":"paragraph"}`} {
		if _, err := Convert([]byte(in)); err == nil {
			t.Errorf("Convert(%q) should fail", in)
		}
	}
}

func TestConvert_Basic(t *testing.T) {
	doc := `[
	  {"type":"heading","props":{"level":2},"content":[{"type":"text","text":"Title","styles":{}}]},
	  {"type":"paragraph","content":[
	    {"type":"text","text":"Some ","styles":{}},
	    {"type":"text","text":"bold","styles":{"bold":true}},
	    {"type":"text","text":" text","styles":{}}]},
	  {"type":"bulletListItem","content":[{"type":"text","text":"item","styles":{}}]}
	]`
	got, err := Convert([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	want := "## Title\n\nSome **bold** text\n\n* item\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWithOrigin_AnnotationCard(t *testing.T) {
	doc := `[{"type":"annotationCard","props":{"id":"a","projectId":"p","environmentId":"e","portForwardId":"f"}}]`
	got, _ := Convert([]byte(doc), WithOrigin("https://paca.example.com/"))
	want := "[Comment](https://paca.example.com/projects/p/environments/e/port-forwards/f/comments/a)\n"
	if got != want {
		t.Errorf("with origin: got %q, want %q", got, want)
	}
	got, _ = Convert([]byte(doc))
	if !strings.HasPrefix(got, "[Comment](/projects/p/") {
		t.Errorf("without origin the link should be relative, got %q", got)
	}
}

func TestConvert_UnknownBlockKeepsText(t *testing.T) {
	got, _ := Convert([]byte(`[{"type":"somethingNew","content":[{"type":"text","text":"kept","styles":{}}]}]`))
	if got != "kept\n" {
		t.Errorf("got %q", got)
	}
}

func TestConvertBlocks_MatchesConvert(t *testing.T) {
	blocks := []any{map[string]any{"type": "paragraph", "content": "plain"}}
	if got := ConvertBlocks(blocks); got != "plain\n" {
		t.Errorf("got %q", got)
	}
}

func TestUTF16Helpers(t *testing.T) {
	// An emoji is a surrogate pair; slicing one unit off leaves a lone
	// surrogate that the final pass turns into U+FFFD, as Node would.
	s := "a🎉"
	if jsLen(s) != 3 {
		t.Errorf("jsLen = %d, want 3", jsLen(s))
	}
	if got := finalizeString(dropLastUnit(s)); got != "a\uFFFD" {
		t.Errorf("dropLastUnit = %q", got)
	}
	if got := finalizeString(dropLastUnit(s) + sliceLastUnit(s)); got != s {
		t.Errorf("rejoined halves = %q, want %q", got, s)
	}
	if firstUnit("") != -1 || lastUnit("") != -1 {
		t.Error("empty string has no code unit")
	}
}
