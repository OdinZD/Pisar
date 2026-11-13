# CLAUDE.md — Pisar

Lokalni macOS editor za `.md`, `.txt`, `.docx` (i uvoz `.doc`) s pravim Neovimom u Wails prozoru.

Plan i faze: `docs/plans/2026-11-11-pisar-design.md`. Radni protokol: `PROMPT.md`. Izgled:
`docs/design/DESIGN.md`. Stanje rada: `PROGRESS.md` (pročitaj prije početka, ažuriraj nakon svakog koraka).

## Stack (fiksno; nova ovisnost traži moje odobrenje)

- Go · Wails v3 (verzija fiksirana u `go.mod`) · Svelte 5 + TypeScript + Vite
- xterm.js u frontendu · PTY u Go-u (`github.com/creack/pty`)
- Neovim kao vanjski proces; upravljanje preko msgpack-RPC socketa (`github.com/neovim/go-client`)
- pandoc (docx ↔ markdown, markdown → html) i macOS `textutil` (.doc → .docx), oba preko `os/exec`
- Bez baze: konfiguracija je jedna JSON datoteka u `~/Library/Application Support/Pisar/`

## Struktura

- `internal/config` — konfiguracija, putanje, popis projekata
- `internal/project` — stablo dokumenata, tražilica po imenu
- `internal/nvim` — proces, PTY, RPC, događaji iz Neovima
- `internal/convert` — pozivi pandoca i textutila, detekcija gubitaka u `.docx`-u
- `internal/docsafe` — backup, atomski zapis, provjera konflikta
- `internal/preview` — markdown → HTML za pregled
- `frontend/src/` — Svelte komponente; `theme/tokens.ts` je jedini izvor boja
- `nvim/` — `init.lua` koji aplikacija isporučuje

## Pisanje u korisnikove datoteke (VAŽNO)

- Jedino mjesto koje smije pisati u korisnikove datoteke je `internal/docsafe`.
  Iznimka: `.md`, `.txt` i `<home>/user.lua` zapisuje sam Neovim na `:w`.
- `.docx`: uvijek backup prije prvog prepisivanja u sesiji; zapis je atomski
  (temp u istom folderu + fsync + rename, uz prijenos dozvola i xattr-a).
- `.docx` s komentarima, praćenim izmjenama ili tekstualnim okvirima otvara se samo za čitanje.
- `.doc` se nikad ne prepisuje; sprema se kao novi `.docx` pored izvornika.
- Nikad autosave u korisnikovu datoteku. Brisanje ide u Trash, nikad `rm`.
- Ako konverzija ne uspije, izvornik ostaje netaknut, a radna kopija sačuvana.
- Testovi koji pišu rade isključivo u `t.TempDir()`.

## Go pravila

- Za Go koristi skillove iz `samber/cc-skills-golang` (umjesto `golang-pro`). Ako u sesiji nisu
  dostupni, reci to na početku i nastavi po pravilima ispod.
- `gofmt` i `golangci-lint` moraju proći.
- `context.Context` je prvi argument svake blokirajuće operacije; vanjski procesi idu kroz
  `exec.CommandContext` s vremenskim ograničenjem.
- Greške: `fmt.Errorf("...: %w", err)`; sentinel greške provjeravaj s `errors.Is` / `errors.As`.
- Svaka goroutina ima jasan životni vijek (errgroup ili WaitGroup + context).
- Male interfejse definiraj kod potrošača. Testovi su table-driven; konverzija ima golden testove.
- Nema hardkodiranih putanja ni limita: sve ide kroz `internal/config`.
- Korisnički unos nikad ne ide u shell string; argumenti procesa su zasebni elementi.

## Frontend pravila

- Boje, razmaci i fontovi dolaze samo iz `theme/tokens.ts`.
- Pregled se prikazuje u sandboxed iframeu bez skripti; HTML iz dokumenta nikad ne ulazi u DOM aplikacije.
- Tekst sučelja je na hrvatskom i živi u jednoj datoteci (`frontend/src/strings.ts`).
- Kad terminal ima fokus, on dobiva sve tipke osim prečaca s `⌘`.

## Provjera prije upotrebe

Prije prve upotrebe bilo kojeg API-ja (Wails v3, xterm.js, go-client, creack/pty, pandoc zastavice,
textutil) provjeri ga u `go doc`, `node_modules`, `--help` ili man stranici. Ne piši potpise ni
zastavice napamet. Ako nešto ne možeš potvrditi, reci to.

## Commitovi

- Mali commitovi, jedan logički korak po commitu.
- Bez „Co-Authored-By” i drugih Claude trailera.
- Ne commitaj ako `task test` ili `task lint` pada.
