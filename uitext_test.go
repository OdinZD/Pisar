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

// The new menu and quit texts exist, and the templates keep their placeholders.
func TestUITextMenuAndQuit(t *testing.T) {
	text, err := loadUIText()
	if err != nil {
		t.Fatal(err)
	}
	for name, v := range map[string]string{
		"menuEdit": text.MenuEdit, "menuSave": text.MenuSave, "menuCopy": text.MenuCopy, "menuPaste": text.MenuPaste,
		"quitTitle": text.QuitTitle, "quitUnnamed": text.QuitUnnamed, "quitSave": text.QuitSave,
		"quitDiscard": text.QuitDiscard, "quitSaveFailedTitle": text.QuitSaveFailedTitle,
		"quitUnresponsiveTitle": text.QuitUnresponsiveTitle, "quitUnresponsive": text.QuitUnresponsive,
		"quitAnyway": text.QuitAnyway, "errorUnnamedBuffer": text.ErrorUnnamedBuffer,
	} {
		if strings.TrimSpace(v) == "" {
			t.Errorf("%s is empty", name)
		}
	}
	for _, c := range []struct{ name, tmpl, ph string }{
		{"quitMessage", text.QuitMessage, "names"},
		{"quitMore", text.QuitMore, "count"},
		{"quitSaveFailed", text.QuitSaveFailed, "reason"},
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

// The Word document texts exist, and the templates keep their placeholders.
func TestUITextWordDocuments(t *testing.T) {
	text, err := loadUIText()
	if err != nil {
		t.Fatal(err)
	}
	for name, v := range map[string]string{
		"recoverTitle": text.RecoverTitle, "recoverUse": text.RecoverUse, "recoverDiscard": text.RecoverDiscard,
		"conflictTitle": text.ConflictTitle, "conflictOverwrite": text.ConflictOverwrite,
		"conflictReload": text.ConflictReload, "saveCopyTitle": text.SaveCopyTitle,
		"saveCopySuffix": text.SaveCopySuffix, "importSaveTitle": text.ImportSaveTitle,
	} {
		if strings.TrimSpace(v) == "" {
			t.Errorf("%s is empty", name)
		}
	}
	for _, c := range []struct{ name, tmpl, ph string }{
		{"recoverMessage", text.RecoverMessage, "name"},
		{"conflictMessage", text.ConflictMessage, "name"},
	} {
		if !strings.Contains(c.tmpl, "{"+c.ph+"}") {
			t.Errorf("%s has no {%s} placeholder", c.name, c.ph)
		}
	}
}

// The save failures of Word documents the quit dialog explains.
func TestUITextSaveFailures(t *testing.T) {
	text, err := loadUIText()
	if err != nil {
		t.Fatal(err)
	}
	for name, v := range map[string]string{
		"errorConflict": text.ErrorConflict, "errorConvertFailed": text.ErrorConvertFailed,
		"errorPandocMissing": text.ErrorPandocMissing, "errorReadOnly": text.ErrorReadOnly,
		"errorDamagedWork": text.ErrorDamagedWork, "errorDocumentTooLarge": text.ErrorDocumentTooLarge,
		"errorNotWord": text.ErrorNotWord,
	} {
		if strings.TrimSpace(v) == "" {
			t.Errorf("%s is empty", name)
		}
	}
}

func TestUITextMenuFormat(t *testing.T) {
	text, err := loadUIText()
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct{ got, want string }{
		"menuFormat": {text.MenuFormat, "Oblikovanje"},
		"menuBold":   {text.MenuBold, "Podebljano"},
		"menuItalic": {text.MenuItalic, "Kurziv"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", name, c.got, c.want)
		}
	}
}

func TestUITextMenuSearch(t *testing.T) {
	text, err := loadUIText()
	if err != nil {
		t.Fatal(err)
	}
	if text.MenuSearch != "Traži dokument…" {
		t.Errorf("menuSearch = %q", text.MenuSearch)
	}
}
