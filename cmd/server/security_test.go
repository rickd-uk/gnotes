package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMarkdownRendererRejectsActiveContent(t *testing.T) {
	rendered := strings.ToLower(mdToHTML(`<script>alert(1)</script>

![image](javascript:alert(2))

[link](javascript:alert(3))`))
	for _, unsafe := range []string{"<script", "javascript:"} {
		if strings.Contains(rendered, unsafe) {
			t.Fatalf("rendered Markdown contains unsafe content %q: %s", unsafe, rendered)
		}
	}
}

func TestMarkdownRendererHighlightsFencedCode(t *testing.T) {
	examples := map[string]string{
		"c":      "int main(void) { return 0; }",
		"go":     "func main() { println(\"hello\") }",
		"html":   "<main>Hello</main>",
		"python": "def hello(): return True",
		"rust":   "fn main() { println!(\"hello\"); }",
	}
	for language, source := range examples {
		rendered := mdToHTML("```" + language + "\n" + source + "\n```")
		if !strings.Contains(rendered, `<span style=`) || !strings.Contains(rendered, "color:") {
			t.Errorf("fenced %s code was not highlighted: %s", language, rendered)
		}
	}
}

func TestMarkdownRendererEscapesHighlightedHTML(t *testing.T) {
	rendered := strings.ToLower(mdToHTML("```html\n<script>alert('no')</script>\n```"))
	if strings.Contains(rendered, "<script") {
		t.Fatalf("highlighted code contains executable markup: %s", rendered)
	}
	if !strings.Contains(rendered, "&lt;") {
		t.Fatalf("highlighted HTML was not escaped: %s", rendered)
	}
}

func TestMarkdownCodeLanguageLabels(t *testing.T) {
	rendered := mdToHTML("    indented\n\n```go\nfunc main() {}\n```\n\n```\nplain\n```\n\n~~~unknown\ncustom\n~~~\n\n```bad\"onclick=evil\nx\n```")
	for _, attribute := range []string{`data-code-language="go"`, `data-code-language="unknown"`, `data-code-language="bad&#34;onclick=evil"`} {
		if !strings.Contains(rendered, attribute) {
			t.Fatalf("missing escaped language attribute %s: %s", attribute, rendered)
		}
	}
	if strings.Count(rendered, `data-code-language=""`) != 1 {
		t.Fatalf("only explicitly fenced plain code should have an empty language: %s", rendered)
	}
}

func TestNoteRendererTreatsIndentationAsText(t *testing.T) {
	for _, raw := range []string{"    first\n        second", "\tfirst\n\t\tsecond"} {
		rendered := mdToHTML(raw)
		if strings.Contains(rendered, "<pre") || strings.Contains(rendered, "<code") ||
			!strings.Contains(rendered, "<p>"+strings.Repeat("\u00a0", 4)+"first<br>\n"+strings.Repeat("\u00a0", 8)+"second</p>") {
			t.Errorf("indentation became code or lost its spacing: %q", rendered)
		}
	}
	// Literal text must not become HTML, Markdown formatting or decoded entities.
	rendered := mdToHTML("    <script>alert(1)</script> **text** &amp; \\\\path")
	if strings.Contains(rendered, "<script") || strings.Contains(rendered, "<strong>") ||
		!strings.Contains(rendered, "&lt;script&gt;") || !strings.Contains(rendered, "&amp;amp;") || !strings.Contains(rendered, `\\path`) {
		t.Fatalf("indented literal text changed or became active: %s", rendered)
	}
	for _, raw := range []string{"```\n    explicit code\n```", "~~~go\n    func main() {}\n~~~"} {
		if rendered := mdToHTML(raw); !strings.Contains(rendered, "<pre") || !strings.Contains(rendered, "<code") {
			t.Errorf("explicit fenced code lost its block: %s", rendered)
		}
	}
}

func TestIndentedTextPreservesMarkdownContainers(t *testing.T) {
	rendered := mdToHTML(">     quoted text\n\n- item\n\n      nested text\n\n```\n    explicit code\n```")
	for _, expected := range []string{"<blockquote>", "<ul>", strings.Repeat("\u00a0", 4) + "quoted text", strings.Repeat("\u00a0", 4) + "nested text"} {
		if !strings.Contains(rendered, expected) {
			t.Errorf("missing %q from nested indentation: %s", expected, rendered)
		}
	}
	if strings.Count(rendered, "<pre") != 1 {
		t.Fatalf("only fenced code should be enclosed: %s", rendered)
	}
}

func TestNoteRendererPreservesOrdinaryLineBreaks(t *testing.T) {
	raw := "[Sriniously](https://www.youtube.com/@sriniously)  *System Engineering\n[Zachary Huang](https://www.youtube.com/@ZacharyLLM/videos)  *AI Dives"
	rendered := mdToHTML(raw)
	if strings.Count(rendered, "<br>") != 1 || strings.Count(rendered, "<a href=") != 2 {
		t.Fatalf("two linked lines should stay separate: %s", rendered)
	}
	if got := mdToHTML("first\nsecond\n\nthird"); strings.Count(got, "<p>") != 2 || strings.Count(got, "<br>") != 1 {
		t.Fatalf("paragraphs and ordinary breaks should both survive: %s", got)
	}
	if got := mdToHTML("```\nfirst\nsecond\n```"); strings.Contains(got, "<br>") {
		t.Fatalf("code block newlines must stay literal: %s", got)
	}
}

func TestMarkdownRendererSupportsToolbarFormatting(t *testing.T) {
	rendered := mdToHTML(`~~finished~~

- [ ] pending

| Column 1 | Column 2 |
| --- | --- |
| Value | Value |`)
	for _, element := range []string{"<del>", `type="checkbox"`, "<table>"} {
		if !strings.Contains(rendered, element) {
			t.Errorf("toolbar Markdown did not render %s: %s", element, rendered)
		}
	}
}

func TestClientIPOnlyTrustsLoopbackProxy(t *testing.T) {
	trusted := httptest.NewRequest(http.MethodPost, "/", nil)
	trusted.RemoteAddr = "127.0.0.1:1234"
	trusted.Header.Set("X-Forwarded-For", "203.0.113.9, 127.0.0.1")
	if got := clientIP(trusted); got != "203.0.113.9" {
		t.Fatalf("trusted proxy client IP = %q", got)
	}

	untrusted := httptest.NewRequest(http.MethodPost, "/", nil)
	untrusted.RemoteAddr = "198.51.100.7:1234"
	untrusted.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := clientIP(untrusted); got != "198.51.100.7" {
		t.Fatalf("untrusted proxy spoofed client IP: %q", got)
	}
}

func TestSecurityHeaders(t *testing.T) {
	t.Setenv("GNOTES_SECURE_COOKIES", "true")
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	for header, want := range map[string]string{
		"Content-Security-Policy":      "frame-ancestors 'none'",
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
		"Strict-Transport-Security":    "max-age=31536000",
		"X-Content-Type-Options":       "nosniff",
	} {
		if got := response.Header().Get(header); !strings.Contains(got, want) {
			t.Errorf("%s = %q, want it to contain %q", header, got, want)
		}
	}
}

func TestNoteInputLimits(t *testing.T) {
	if err := validateNoteText("", ""); err == nil {
		t.Fatal("empty note was accepted")
	}
	if err := validateNoteText(strings.Repeat("t", maxNoteTitleBytes+1), "body"); err == nil {
		t.Fatal("oversized title was accepted")
	}
	if err := validateNoteText("title", strings.Repeat("b", maxNoteBodyBytes+1)); err == nil {
		t.Fatal("oversized body was accepted")
	}
	if err := validateNoteText(strings.Repeat("t", maxNoteTitleBytes), strings.Repeat("b", maxNoteBodyBytes)); err != nil {
		t.Fatalf("maximum-sized note was rejected: %v", err)
	}
}
