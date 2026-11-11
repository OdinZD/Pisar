// All user-facing interface text lives in strings.json (project rule). The Go side embeds the same
// file for native dialogs and menus, so there is exactly one copy of every text. Components import
// from this file only.
import data from './strings.json';

export const strings = data;
