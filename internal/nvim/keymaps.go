package nvim

import (
	"context"
	"fmt"

	nvimrpc "github.com/neovim/go-client/nvim"
)

// Keymap is one mapping that has a description.
type Keymap struct {
	Mode string // "n", "x" or "i"
	Keys string // as keytrans() writes them: "<Space>f", "<Down>"
	Desc string
}

// Keymaps is what Process.Keymaps returns.
type Keymaps struct {
	Maps   []Keymap
	Groups map[string]string // vim.g.pisar_groups: keys after the leader -> name
}

// keymapsWire is the shape pisar_keys() returns (see init.lua); it is decoded
// as it comes, with the field names the Lua table uses.
type keymapsWire struct {
	Maps []struct {
		Mode string `msgpack:"mode"`
		Keys string `msgpack:"keys"`
		Desc string `msgpack:"desc"`
	} `msgpack:"maps"`
	Groups map[string]string `msgpack:"groups"`
}

// Keymaps asks the running Neovim for the shortcuts to show: the global
// mappings of normal, visual and insert mode that have a description (Pisar's
// own and those of the user's user.lua, not the ones Neovim itself makes), and
// the names of the groups the user gave in vim.g.pisar_groups. At most maxMaps
// mappings and maxMaps group names come back, each text cut to maxText
// characters (not bytes); both must be at least 1, otherwise the error is
// returned without a call to Neovim. Maps and Groups are never nil.
//
// Like Format, it asks for the mode first and refuses while Neovim waits in a
// hit-enter prompt or another blocking read (ErrPromptOpen, wrapped): a Lua
// call would not be answered there (measured with Neovim 0.12), and the list is
// not worth a wait until the deadline. Nothing is sent to Lua then.
func (p *Process) Keymaps(ctx context.Context, maxMaps, maxText int) (Keymaps, error) {
	if maxMaps < 1 || maxText < 1 {
		return Keymaps{}, fmt.Errorf("nvim: keymaps: limits must be at least 1 (maps %d, text %d)", maxMaps, maxText)
	}
	var wire keymapsWire
	err := p.call(ctx, "keymaps", func(nv *nvimrpc.Nvim) error {
		m, err := nv.Mode()
		if err != nil {
			return err
		}
		if m.Blocking {
			return ErrPromptOpen
		}
		return nv.ExecLua(`return pisar_keys(...)`, &wire, maxMaps, maxText)
	})
	if err != nil {
		return Keymaps{}, err
	}
	out := Keymaps{Maps: make([]Keymap, 0, len(wire.Maps)), Groups: wire.Groups}
	for _, m := range wire.Maps {
		out.Maps = append(out.Maps, Keymap{Mode: m.Mode, Keys: m.Keys, Desc: m.Desc})
	}
	if out.Groups == nil {
		out.Groups = map[string]string{}
	}
	return out, nil
}
