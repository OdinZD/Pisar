package main

import (
	"strings"
	"testing"
)

func TestLoadUIText(t *testing.T) {
	text, err := loadUIText()
	if err != nil {
		t.Fatal(err)
	}
	if text.AppName == "" || text.TrashConfirm == "" {
		t.Fatalf("unexpected text: %+v", text)
	}
	for _, c := range []struct{ name, tmpl, ph string }{
		{"trashMessage", text.TrashMessage, "name"},
		{"configRecovered", text.ConfigRecovered, "path"},
	} {
		if !strings.Contains(c.tmpl, "{"+c.ph+"}") {
			t.Errorf("%s has no {%s} placeholder", c.name, c.ph)
		}
	}
}

func TestParseUIText(t *testing.T) {
	full, err := loadUIText()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseUIText([]byte(`{"appName": "x"`)); err == nil {
		t.Error("invalid JSON must fail")
	}
	if _, err := parseUIText([]byte(`[]`)); err == nil {
		t.Error("wrong JSON shape must fail")
	}
	_, err = parseUIText([]byte(`{"appName": "x", "cancel": "  "}`))
	if err == nil || !strings.Contains(err.Error(), "cancel") || !strings.Contains(err.Error(), "trashTitle") {
		t.Errorf("missing keys must be named, got %v", err)
	}
	// Unknown keys (added by the frontend) are fine.
	if _, err := parseUIText(append([]byte(`{"extra": {"a": "b"},`), stringsJSON[1:]...)); err != nil {
		t.Errorf("unknown key rejected: %v (%+v)", err, full)
	}
}

func TestFill(t *testing.T) {
	if got := fill("a {name} b {name}", "name", "{name}x"); got != "a {name}x b {name}x" {
		t.Errorf("got %q", got)
	}
	if got := fill("no placeholder", "name", "x"); got != "no placeholder" {
		t.Errorf("got %q", got)
	}
}
