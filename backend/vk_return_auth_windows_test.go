//go:build windows

package backend

import "testing"

func TestExtractModernRUReturnAuthFromWindowInitHTMLExactPath(t *testing.T) {
	body := `
<script>
var decoy = {"return_auth":"WRONG"};
window.init = {
  "data": {
    "hash": {
      "return_auth": "RIGHT_HASH",
      "nested": {"brace": "}"}
    }
  },
  "other": {"return_auth":"ALSO_WRONG"}
};
</script>`

	if got := extractModernRUReturnAuthFromWindowInitHTML(body); got != "RIGHT_HASH" {
		t.Fatalf("return_auth = %q, want RIGHT_HASH", got)
	}
}

func TestExtractModernRUReturnAuthFromWindowInitHTMLRequiresExactPath(t *testing.T) {
	body := `<script>window.init = {"data":{"other":{"return_auth":"WRONG"}}};</script>`
	if got := extractModernRUReturnAuthFromWindowInitHTML(body); got != "" {
		t.Fatalf("unexpected return_auth = %q", got)
	}
}

func TestExtractWindowInitJSONObjectHandlesEscapedBraces(t *testing.T) {
	body := `<script>window.init = {"text":"\\\"} still string","data":{"hash":{"return_auth":"HASH"}}}; tail</script>`
	raw := extractWindowInitJSONObject(body)
	if raw == "" {
		t.Fatal("window.init JSON was not extracted")
	}
	if got := extractModernRUReturnAuthFromWindowInitHTML(body); got != "HASH" {
		t.Fatalf("return_auth = %q", got)
	}
}
