package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// The interface text lives in one file shared with the frontend
// (frontend/src/strings.json); Go needs it for native menus and dialogs.
//
//go:embed frontend/src/strings.json
var stringsJSON []byte

// uiText holds the texts the Go side uses. Keys the frontend adds later are
// ignored here on purpose: the file is shared.
type uiText struct {
	AppName               string `json:"appName"`
	MenuFile              string `json:"menuFile"`
	MenuNewDocument       string `json:"menuNewDocument"`
	MenuAddProject        string `json:"menuAddProject"`
	MenuEdit              string `json:"menuEdit"`
	MenuSave              string `json:"menuSave"`
	MenuCopy              string `json:"menuCopy"`
	MenuPaste             string `json:"menuPaste"`
	MenuSearch            string `json:"menuSearch"`
	MenuShortcuts         string `json:"menuShortcuts"`
	MenuFormat            string `json:"menuFormat"`
	MenuBold              string `json:"menuBold"`
	MenuItalic            string `json:"menuItalic"`
	MenuView              string `json:"menuView"`
	MenuTheme             string `json:"menuTheme"`
	ThemePisar            string `json:"themePisar"`
	ThemeCatppuccinMocha  string `json:"themeCatppuccinMocha"`
	ThemeDracula          string `json:"themeDracula"`
	QuitTitle             string `json:"quitTitle"`
	QuitMessage           string `json:"quitMessage"` // placeholder {names}
	QuitMore              string `json:"quitMore"`    // placeholder {count}
	QuitUnnamed           string `json:"quitUnnamed"`
	QuitSave              string `json:"quitSave"`
	QuitDiscard           string `json:"quitDiscard"`
	QuitSaveFailedTitle   string `json:"quitSaveFailedTitle"`
	QuitSaveFailed        string `json:"quitSaveFailed"` // placeholder {reason}
	QuitUnresponsiveTitle string `json:"quitUnresponsiveTitle"`
	QuitUnresponsive      string `json:"quitUnresponsive"`
	QuitAnyway            string `json:"quitAnyway"`
	NvimPromptOpen        string `json:"nvimPromptOpen"`
	NvimFormatFailed      string `json:"nvimFormatFailed"`   // placeholder {reason}
	NvimUserLuaError      string `json:"nvimUserLuaError"`   // placeholder {reason}
	NvimSpellMissing      string `json:"nvimSpellMissing"`   // placeholder {languages}
	NvimEventsRestored    string `json:"nvimEventsRestored"` // placeholder {events}
	NvimNoFileName        string `json:"nvimNoFileName"`     // placeholder {buffer}
	NvimRestoreFailed     string `json:"nvimRestoreFailed"`  // placeholder {reason}
	NvimMapRows           string `json:"nvimMapRows"`
	NvimMapSpell          string `json:"nvimMapSpell"`
	NvimMapNumbers        string `json:"nvimMapNumbers"`
	NvimMapTree           string `json:"nvimMapTree"`
	NvimMapPreview        string `json:"nvimMapPreview"`
	NvimMapSearch         string `json:"nvimMapSearch"`
	NvimMapBold           string `json:"nvimMapBold"`
	NvimMapItalic         string `json:"nvimMapItalic"`
	NvimMapHeading        string `json:"nvimMapHeading"`
	NvimMapList           string `json:"nvimMapList"`
	NvimMapKeys           string `json:"nvimMapKeys"`
	NvimMapShortcuts      string `json:"nvimMapShortcuts"`
	NvimKeysTitle         string `json:"nvimKeysTitle"`
	ErrorUnnamedBuffer    string `json:"errorUnnamedBuffer"`
	ErrorConflict         string `json:"errorConflict"`
	ErrorConvertFailed    string `json:"errorConvertFailed"`
	ErrorPandocMissing    string `json:"errorPandocMissing"`
	ErrorReadOnly         string `json:"errorReadOnly"`
	ErrorDamagedWork      string `json:"errorDamagedWork"`
	ErrorDocumentTooLarge string `json:"errorDocumentTooLarge"`
	ErrorNotWord          string `json:"errorNotWord"`
	ChooseFolderTitle     string `json:"chooseFolderTitle"`
	TrashTitle            string `json:"trashTitle"`
	TrashMessage          string `json:"trashMessage"` // placeholder {name}
	TrashConfirm          string `json:"trashConfirm"`
	Cancel                string `json:"cancel"`
	OK                    string `json:"ok"`
	ConfigRecoveredTitle  string `json:"configRecoveredTitle"`
	ConfigRecovered       string `json:"configRecovered"` // placeholder {path}
	ConfigNewerTitle      string `json:"configNewerTitle"`
	ConfigNewer           string `json:"configNewer"`
	RecoverTitle          string `json:"recoverTitle"`
	RecoverMessage        string `json:"recoverMessage"` // placeholder {name}
	RecoverUse            string `json:"recoverUse"`
	RecoverDiscard        string `json:"recoverDiscard"`
	ConflictTitle         string `json:"conflictTitle"`
	ConflictMessage       string `json:"conflictMessage"` // placeholder {name}
	ConflictOverwrite     string `json:"conflictOverwrite"`
	ConflictReload        string `json:"conflictReload"`
	SaveCopyTitle         string `json:"saveCopyTitle"`
	SaveCopySuffix        string `json:"saveCopySuffix"`
	ImportSaveTitle       string `json:"importSaveTitle"`
	StartFailedTitle      string `json:"startFailedTitle"`
	StartFailed           string `json:"startFailed"` // placeholder {reason}
}

// nvimText are the texts Neovim shows (internal/nvim, Options.Text): every key
// that starts with "nvim", under its name in strings.json. The package takes
// them as data: it must not read the file or import this package. A new nvim*
// field goes there without a line here. nvimPromptOpen is among them although
// Neovim never prints it: it is what the quit dialog shows for
// nvim.ErrPromptOpen, and it costs nothing to carry.
func nvimText(t uiText) map[string]string {
	out := map[string]string{}
	rt, rv := reflect.TypeOf(t), reflect.ValueOf(t)
	for i := range rt.NumField() {
		key := rt.Field(i).Tag.Get("json")
		if strings.HasPrefix(key, "nvim") {
			out[key] = rv.Field(i).String()
		}
	}
	return out
}

// parseUIText decodes the texts and reports every key that is missing or empty,
// so a broken file gives a clear error at startup instead of blank dialogs.
func parseUIText(data []byte) (uiText, error) {
	var t uiText
	if err := json.Unmarshal(data, &t); err != nil {
		return uiText{}, fmt.Errorf("interface texts: %w", err)
	}
	var missing []string
	rt, rv := reflect.TypeOf(t), reflect.ValueOf(t)
	for i := range rt.NumField() {
		if strings.TrimSpace(rv.Field(i).String()) == "" {
			missing = append(missing, rt.Field(i).Tag.Get("json"))
		}
	}
	if len(missing) > 0 {
		return uiText{}, fmt.Errorf("interface texts: missing or empty keys: %s", strings.Join(missing, ", "))
	}
	return t, nil
}

// loadUIText reads the embedded texts.
func loadUIText() (uiText, error) { return parseUIText(stringsJSON) }

// fill replaces one {placeholder} in a single pass, so a value that itself
// contains "{name}" is not substituted again.
func fill(template, placeholder, value string) string {
	return strings.NewReplacer("{"+placeholder+"}", value).Replace(template)
}
