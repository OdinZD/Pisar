// All user-visible text of the frontend lives here. The menu labels and the
// window title are in Go (menu.go, main.go), because Go builds the menu; the
// project rule asks for one file, which phase 1 has to settle.
export const strings = {
  // Shown inside the terminal after Neovim quit on its own.
  nvimExited: 'Neovim je završio. Pritisni Enter za ponovno pokretanje.',
  // Shown inside the terminal when Start failed.
  startFailed: 'Neovim se nije pokrenuo. Pritisni Enter za novi pokušaj.',
} as const
