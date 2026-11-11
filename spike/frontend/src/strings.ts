// All user-visible text of the interface lives here (project rule).
export const strings = {
  // Shown inside the terminal after Neovim quit on its own.
  nvimExited: 'Neovim je završio. Pritisni Enter za ponovno pokretanje.',
  // Shown inside the terminal when Start failed.
  startFailed: 'Neovim se nije pokrenuo. Pritisni Enter za novi pokušaj.',
} as const
