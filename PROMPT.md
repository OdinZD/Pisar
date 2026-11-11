# PROMPT.md — Pisar (radni naziv)

Radni protokol za Claude Code. Ostali dokumenti:

- `CLAUDE.md` — trajna pravila (stack, pisanje u datoteke, commitovi)
- `docs/plans/2026-11-11-pisar-design.md` — arhitektura, tokovi i **faze s kriterijima prihvaćanja**
- `docs/design/DESIGN.md` — izgled; `docs/design/pisar.png` je ilustracija, tekst je mjerodavan
- `PROGRESS.md` — stanje rada (stvori ga u prvom koraku)

## Što gradimo

Lokalni macOS editor za pisanje: `.md`, `.txt`, `.docx` i uvoz `.doc`. Uređivanje radi **pravi Neovim**
(ne emulacija), pokrenut u PTY-ju i prikazan kroz xterm.js unutar Wails prozora. Markdown je radni
format: `.docx` se pri otvaranju pretvara u radnu `.md` kopiju, a `:w` ga pretvara natrag. Oko editora
su stablo projekata (foldera), pregled dokumenta i tražilica dokumenata.

Ja (Odin) znam Go, vim i Neovim. Uz svaki netrivijalni dio kratko objasni zašto je tako napisan.

## Uloge i modeli

| Uloga | Model | Effort | Radi |
|---|---|---|---|
| Glavna sesija | Fable | medium | plan faze, briefovi, pregled diffa, PROGRESS.md |
| Subagent `coder` | Sonnet | medium | kod i testovi prema briefu |

- Glavna sesija **ne piše produkcijski kod**. Iznimka: ispravak nakon pregleda do otprilike 20 linija.
- **Faze 0 i 3 idu na effort high.** Na početku tih faza podsjeti me da pokrenem `/effort high`, a
  `coder` pozivaj s effort high. Na kraju faze podsjeti me da vratim medium.
- `coder` kreće bez konteksta ovog razgovora. Svaki brief zato sadrži:
  1. cilj u jednoj rečenici
  2. datoteke koje smije mijenjati i one koje ne smije
  3. sučelja (tipove i potpise) koja mora poštovati
  4. kriterije prihvaćanja i testove koje mora napisati
  5. API-je koje mora provjeriti prije upotrebe i gdje
- **Pregled diffa prije svakog commita.** Provjeri redom:
  1. Radi li diff samo ono što je brief tražio?
  2. Piše li išta u korisnikove datoteke izvan `internal/docsafe`?
  3. Je li svaki novi vanjski API potvrđen (go doc, node_modules, `--help`, man), a ne napisan napamet?
  4. Prolaze li `task test` i `task lint`?
  5. Pokrivaju li testovi rubne slučajeve iz kriterija, a ne samo sretan put?
  Ako nešto ne prolazi, vrati `coder`-u s konkretnom primjedbom. Ne popravljaj sam veće stvari.

## Dizajn (sažetak; puni opis je u `docs/design/DESIGN.md`)

- Svijetla tema. Raspored: alatna traka (48 px) iznad tri panela — stablo projekata (240 px),
  Neovim (rastezljiv), pregled (oko 400 px, pali se i gasi).
- Tražilica dokumenata je panel na dnu stupca editora. Otvaraju je `␣f` iz Neovima i `⌘P`.
- Editor je terminalska mreža: samo monospace, a boje dolaze iz Neovim teme.
- Boje su definirane **jednom**, u `frontend/src/theme/tokens.ts`. Iz njih se izvode CSS varijable,
  xterm.js tema i Neovim colorscheme. Nijedna boja ne smije biti upisana drugdje.
- Fontovi su lokalni (woff2 u `frontend/src/assets/fonts/`): Inter, JetBrains Mono, Source Serif 4.
- **Prije nego faza 0 prođe, ne poliraj UI.** Ako spike padne, editor više nije terminal i dio
  dizajna se mijenja.

## Petlja po fazi

1. Pročitaj `PROGRESS.md` i odjeljak faze u planu.
2. Ispiši plan faze u 5–10 koraka i čekaj moje „kreni”.
3. Za svaki korak: brief → `coder` → pregled diffa → testovi → commit.
4. Na kraju faze prođi kriterije prihvaćanja jedan po jedan i uz svaki navedi dokaz (naredba i izlaz).
   Kriterije označene s **[R]** ne možeš provjeriti sam (tipkovnica, fokus, izgled): ne označavaj ih
   kao prošle, nego mi daj popis za ručnu provjeru.
5. Ažuriraj `PROGRESS.md`, napiši sažetak u najviše 10 redaka i **STOP**.

## STOP uvjeti (stani i pitaj)

- Faza 0 ne prolazi neki kriterij. Rezervni plan je CodeMirror 6 + `@replit/codemirror-vim`; odluka
  je moja, ne prelazi na njega sam.
- Treba pisati u korisnikovu datoteku na način koji `CLAUDE.md` ne opisuje.
- Treba nova ovisnost ili promjena stacka.
- Round-trip `.docx`-a gubi nešto što nije u `docs/known-losses.md`.
- `coder` dvaput zaredom vrati diff koji ne prolazi pregled za isti korak.
- Plan i stvarno ponašanje alata se ne slažu (npr. pandoc zastavica ne postoji ili radi drugačije).

## Prvi korak

Ne piši kod. Pročitaj `CLAUDE.md`, plan i `DESIGN.md`, provjeri da `.claude/agents/coder.md` postoji, stvori
`PROGRESS.md` s popisom faza, postavi mi najviše 3 pitanja nužna za fazu 0 i čekaj.
