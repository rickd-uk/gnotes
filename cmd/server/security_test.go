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
