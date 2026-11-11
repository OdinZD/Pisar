# PROGRESS

Plan: `docs/plans/2026-11-11-pisar-design.md` · Protokol: `PROMPT.md` · Izgled: `docs/design/DESIGN.md`

## Stanje

- Trenutna faza: 3 — `.docx` i `.doc` (plan odobren 2026-11-11, koraci 1–6a gotovi). Faza 2 je prihvaćena 2026-11-11: vlasnik je prošao
  ručnu provjeru i potvrdio da sve radi. Faza 1 je prihvaćena 2026-11-11: vlasnik je prošao
  ručni pogled i potvrdio da sve radi. Faza 0 je prihvaćena 2026-11-11:
  vlasnik je potvrdio svih sedam kriterija **[R]** i odlučio **idemo** (pravi Neovim u PTY-ju)
- Sljedeći korak: faza 3, korak 6b (uvezivanje u `EditorService`)
- Effort: vlasnik je 2026-11-11 odlučio da glavna sesija i `coder` rade na `high` u svim fazama; `PROMPT.md`,
  `.claude/agents/coder.md` i plan su usklađeni. (Faza 1 i korak 1 faze 2 još su išli s `coder`-om na `medium`.)
- Zadnja izmjena: 2026-11-12 (faza 3, korak 6a)

## Priprema (2026-11-11)

- [x] `CLAUDE.md`, `PROMPT.md` i `.claude/agents/coder.md` premješteni iz `pisar-start/` u korijen
- [x] putanje `../docs` u `CLAUDE.md` i `PROMPT.md` ispravljene u stvarne
- [x] `git init`, `.gitignore`, `PROGRESS.md`

## Odluke i nalazi prije faze 0

- Wails v3 je fiksiran na **v3.1.0**, isto kao u Tragu (`github.com/wailsapp/wails/v3 v3.1.0`,
  `@wailsio/runtime 3.1.0`). Predložak se zove `svelte` (Svelte + TypeScript + Vite); provjereno s
  `wails3 init -l`.
- Alati na stroju: go 1.27.2, nvim 0.12.0, task 3.54.0, node 22.22.0, golangci-lint 2.15.0, `textutil`.
- **`pandoc` nije instaliran.** Ne treba za fazu 0; prije faze 3 treba `brew install pandoc`.
- Raspored tipkovnice je Croatian: `⌥` mora slagati znakove, ne smije biti Meta.
- JetBrains Mono nema lokalno. Odobreno: preuzeti woff2 iz službenog izdanja JetBrains/JetBrainsMono (OFL).
- Odobreni xterm.js paketi za spike: jezgra, dodatak fit, dodatak WebGL. Točna imena i verzije
  potvrditi u registru prije instalacije.
- `docs/design/pisar.png` ne postoji; tekst `DESIGN.md` je mjerodavan.
- samber Go skillovi dostupni u sesiji: `golang-how-to`, `golang-project-layout`,
  `golang-dependency-management`, `golang-pkg-go-dev`, `golang-popular-libraries`,
  `golang-continuous-integration`, `golang-stay-updated`. Za ostalo vrijede pravila iz `CLAUDE.md`.

## Faza 0 — Spike (rizik)

Effort high (glavna sesija i `coder`). Kod je za bacanje i živi u `spike/`.

### Pretpostavke

- `spike/` je zaseban Go modul (`spike/go.mod`) s vlastitim `frontend/` i `Taskfile.yml`, da brisanje
  mape ne ostavi trag u korijenskom `go.mod`. Provjere: `task -d spike test`, `task -d spike lint`.
- Neovim se pokreće izolirano: `NVIM_APPNAME=pisar-spike`, `-u spike/nvim/init.lua`,
  `--listen <socket u privremenoj mapi>`. Korisnikova konfiguracija se ne učitava i ne dira.
- Izlaz PTY-ja ide Wails događajima; propusnost se mjeri. Drugi prijenos je promjena stacka (STOP).
- Koraci idu redom, bez paralelnih agenata.
- Korake 1 i 2 radio je general-purpose agent na Sonnetu s uputama iz `.claude/agents/coder.md` na
  početku briefa, jer `coder` još nije bio registriran (odobrio vlasnik 2026-11-11).

### Ovisnosti (odobrene)

- Go: `github.com/wailsapp/wails/v3` v3.1.0, `github.com/creack/pty`, `github.com/neovim/go-client`
- npm: ono što donosi predložak `svelte` (uz `@wailsio/runtime` 3.1.0), xterm.js jezgra, fit, WebGL

### Koraci

- [x] 1. Kostur: `wails3 init -n spike -t svelte -mod Pisar/spike`, Wails na v3.1.0, zadaci `test` i
      `lint`, `.golangci.yml` kao u Tragu; aplikacija se gradi (`spike/bin/spike`) i pokreće
- [x] 2. Neovim proces (Go): `spike/internal/nvim` — PTY, promjena veličine, RPC preko socketa, uredno
      gašenje; 14 integracijskih testova s pravim `nvim`-om, čisto i s `-race`
- [x] 3. Most PTY ↔ frontend: `spike/internal/term` (`Session`, grupiranje 8 ms / 16 KiB, bez rezanja
      usred UTF-8 znaka, brojači) i `TerminalService`; gašenje kroz `OnShutdown`
- [x] 4. Terminal (Svelte): `@xterm/xterm` 6.0.0 + fit 0.11.0 + WebGL 0.19.0, JetBrains Mono v2.304 učitan
      prije otvaranja terminala, promjena veličine → fit → `Resize`, serijsko slanje tipki;
      `bash spike/scripts/smoke.sh` prolazi (dom i webgl)
- [x] 5. Tipke i clipboard: meni aplikacije (`spike/menu.go`) — „Spremi” `⌘S` → Go → RPC, „Kopiraj” `⌘C`
      (vizualni odabir u Neovimu, inače odabir u xterm.js-u), „Zalijepi” `⌘V` (uloga sustava → `paste`
      događaj → bracketed paste); tipke s `⌘` xterm.js ne šalje Neovimu. Dolazak tipki do menija je **[R]**
- [x] 6. Mjerenje: `task -d spike bench` (bez tipkovnice: Go šalje `Ctrl-d` kroz PTY), datoteka od 5000
      redaka (`task -d spike sample` → `spike/bin/sample-5000.md`), oba renderera; rezultati dolje
- [x] 7. Gašenje aplikacije: `OnShutdown` zove `Close`; `bash spike/scripts/shutdown.sh` gradi, pokreće i
      gasi aplikaciju sa SIGTERM, SIGINT, SIGHUP i SIGKILL te provjerava da nema Neovima
- [x] 8. Popis za ručnu provjeru **[R]** predan vlasniku: `docs/spike-report.md`, odjeljak 3
- [x] 9. `docs/spike-report.md`, kriteriji s dokazima (dolje), `plan-reviewer`: krug 1 FAIL (5 odstupanja
      u dokumentaciji), krug 2 FAIL (3 sitna u dokumentaciji), krug 3 PASS; sažetak predan, STOP

### Nalazi tijekom faze (ulaze u `docs/spike-report.md`)

- Neovim 0.12 su dva procesa: TUI (dijete u PTY-ju) i server iza RPC socketa, koji je u vlastitoj
  sesiji. Signal grupi procesa pogađa samo TUI, pa `Close` pamti PID servera i gasi ga zasebno.
- Neovim pri pokretanju šalje terminalu upit DSR i čeka odgovor 100 ms; bez odgovora ispiše upozorenje
  s hit-enter promptom i pokretanje stane. Pri izlasku šalje DA1 i čeka do 1 s. Zato na ta dva upita
  odgovara Go (`pump`), neovisno o tome je li xterm.js već priključen. Kasni dvostruki odgovori
  terminala ne ulaze u buffer kao tipke (`TestLateReplies`); s pravim xterm.js-om potvrditi u koraku 4.
- Proces se pokreće s `exec.Command`, ne `CommandContext`: kontekst `Start`-a ograničava samo
  pokretanje, inače bi njegov istek kasnije ubio Neovim. Odstupanje od pravila iz `CLAUDE.md` za
  dugoživuće procese; odlučiti u fazi 1 kako ga zapisati.
- Putanja RPC socketa mora stati u `sun_path` (103 bajta na macOS-u); `Start` to provjerava unaprijed.
- `coder` je registriran od koraka 3 (agent se pojavio u sesiji nakon koraka 2).
- Wails v3.1.0: metoda servisa smije primiti `context.Context` kao prvi argument (generator ga
  izostavlja iz bindinga); `kill` (SIGTERM) aplikaciji pokreće `OnShutdown`.
- `npm audit`: 2 niska nalaza, oba `deepmerge` kroz `@sveltejs/vite-plugin-svelte` (alat za build, bez
  dostupnog popravka).
- Wails v3.1.0 svaki poziv vezane metode obrađuje u vlastitoj goroutini, pa redoslijed poziva nije
  zajamčen. Frontend zato šalje tipke serijski (`serialSender.ts`): jedan `Write` u letu, ostalo se spaja.
- xterm.js `lineHeight` množi izmjerenu visinu znaka, ne veličinu fonta: 21/13 daje redak od 27 px.
  `calibrateLineHeight()` mjeri prirodnu visinu i računa faktor za točno 21 px. Ćelija je 7.80×21 (DOM)
  i 7.50×21 (WebGL zaokružuje širinu na fizičke piksele), pa je mreža 141×33 odnosno 146×33.
- xterm.js 6.0.0 pri pokretanju sam šalje Neovimu odgovore na upite terminala, uključujući DSR i DA1
  po drugi put (uz Go). Ti stvarni odgovori su u `TestLateReplies` i ne ulaze u buffer.
- Poruka o grešci iz `Save` ispisuje se s `ErrorMsg` + `:redraw`, a ne kao prava greška: u insert i
  visual modu Neovim 0.12 inače drži poruku na zaslonu i ne prima tipke (izmjereno: 1 s nakon greške,
  3 s nakon obične poruke „written” u visualu). Isti `:redraw` ide i nakon uspješnog zapisa.
- Wails v3.1.0 ima pojedinačne uloge menija `Copy` i `Paste`; stavke iz uloga `AppMenu` i `WindowMenu`
  ostaju na engleskom (Wails ih sam generira) — za fazu 1 treba vlastiti meni ako želimo hrvatski.
- Testovi clipboarda koriste lažni `g:clipboard` i stub `pbcopy`/`pbpaste` na PATH-u; pravi sistemski
  clipboard se u testovima ne dira. Ponašanje s pravim `pbcopy` provjerava vlasnik ručno.
- Mjerenje (`task -d spike bench`, 2026-11-11, prozor 1100×700, Neovim 0.12.0). Tablica je iz zadnjeg
  pokretanja (`spike/bin/bench-*.json`, izvan gita); dva ranija pokretanja u koraku 6 dala su gotovo
  iste brojke, ali njihove datoteke nisu sačuvane. Faza `repeat`: `Ctrl-d` svaka 33 ms do dna (345 pritisaka, 11,4 s). Faza
  `flood`: `Ctrl-d` pa `Ctrl-u` bez stanke (720 pritisaka, oko 5 s).

  | renderer | faza | događaja/s | MB/s | emit→obrađeno p50/p95/max (ms) | kadar p95/max (ms) | >33 ms | >100 ms |
  |---|---|---|---|---|---|---|---|
  | DOM | repeat | 42 | 0,04 | 0,1 / 7,3 / 9,9 | 18 / 19 | 0 | 0 |
  | DOM | flood | 12 | 0,16 | 1,1 / 1,9 / 9,1 | 18 / 19 | 0 | 0 |
  | WebGL | repeat | 44 | 0,04 | <1 / <1 / 3,5 | 18 / 20 | 0 | 0 |
  | WebGL | flood | 12 | 0,16 | <1 / 0,9 / 1,3 | 18 / 19 | 0 | 0 |

  U svim fazama svi događaji su stigli redom i iste duljine (`eventsMatch`), a zaslon xterm.js-a na
  kraju je jednak onome što Neovim prikazuje (`screenMatches`). Nijedan kadar nije kasnio preko 33 ms.
  Vrijednosti ispod 1 ms su na granici razlučivosti sata u WKWebViewu (zaokružen na oko 1 ms; zato se
  pojavljuju i blago negativne razlike). Propusnost ograničava Neovim (0,16 MB/s), ne prijenos Wails
  događajima: prijenos nije zasićen ni u najgorem slučaju.
- Gašenje: nakon SIGKILL ili SIGHUP aplikacije oba Neovim procesa nestaju sama (PTY se zatvori), ali
  ostaje privremena mapa `pisar-spike-*` s `init.lua`. Zaglavljeni server uz SIGKILL aplikacije ostao
  bi živ; uredno gašenje taj slučaj rješava.
- Neovimovo stanje aplikacije je izvan `spike/`: `~/.local/state/pisar-spike/` (log, shada, swap) i
  `~/.local/share/pisar-spike/`. Obrisati ih zajedno sa `spike/`; u fazi 1 postaviti `XDG_*`.
- Ulazni put tipke (keydown → `Write` → PTY) nije vremenski mjeren; bench mjeri samo izlaz.
- Trag za dijagnostiku: `PISAR_SPIKE_TRACE=<datoteka>` bilježi pozive i SVE tipke; samo za spike,
  ne prenositi u aplikaciju. Renderer: `PISAR_SPIKE_RENDERER=webgl|dom`.

### API-ji koje treba potvrditi prije upotrebe

- Wails v3 (servisi, događaji, meni i prečaci, hook za gašenje): `go doc`, izvor u module cacheu,
  generirani predložak, `../Trag/main.go` i `../Trag/menu.go` kao radni primjer
- `creack/pty`, `neovim/go-client`: `go doc`
- xterm.js i dodaci (opcije fonta i tipke `⌥`, `write`, `paste`, odabir, fit, WebGL): tipovi u `node_modules`
- Neovim (`--listen`, `-u`, `NVIM_APPNAME`, `rpcnotify`, clipboard provider): `nvim --help`, `:help`

### Kriteriji prihvaćanja

- [x] **[R]** `Esc`, `Ctrl-d`, `Ctrl-u`, `Ctrl-v`, `Ctrl-w`, `Ctrl-r` stižu do Neovima
- [x] **[R]** č ć š ž đ i znakovi koji na mojoj tipkovnici traže `⌥` upisuju se ispravno
- [x] **[R]** `⌘C` / `⌘V` rade sa sistemskim clipboardom; `"+y` i `"+p` također
- [x] **[R]** `⌘S` sprema preko RPC-a i ne presreće ga Wails meni
- [x] **[R]** promjena veličine prozora: Neovim se preslaže, bez odsječenih redaka
- [x] **[R]** font je učitan prije otvaranja terminala; ćelije su poravnate
- [x] **[R]** držanje `Ctrl-d` kroz datoteku od 5000 redaka: bez vidljivog zastoja i razbijenog prikaza
- [x] Go otvara datoteku u pokrenutom Neovimu preko RPC socketa — dokaz: `go test -v -run TestOpen ./internal/nvim/`
      → `--- PASS: TestOpen` (7 podtestova); `bash spike/scripts/smoke.sh` → `neovim says: '141 33 sample.md'`
- [x] Go prima obavijest kad Neovim spremi buffer — dokaz: `--- PASS: TestSave`, `--- PASS: TestColonW`,
      `--- PASS: TestWQ` (`task -d spike test` → `ok Pisar/spike/internal/nvim`)
- [x] nakon gašenja aplikacije nema živog `nvim` procesa — dokaz: `bash spike/scripts/shutdown.sh` →
      `shutdown: all checks passed` (četiri signala); `--- PASS: TestClose`, `--- PASS: TestHung`;
      `task -d spike bench` → `no Neovim process of ours is left` nakon `Quit`; `pgrep -fl nvim` prazan
- [x] `docs/spike-report.md`: što radi, što ne, zaobilaznice, preporuka — dokaz: `test -s docs/spike-report.md`

Sedam kriterija **[R]** potvrdio je vlasnik ručno 2026-11-11 (popis s radnjama: `docs/spike-report.md`,
odjeljak 3); agent ih nije provjeravao. Odluka vlasnika: **idemo**.

## Faza 1 — Kostur, projekti i stablo

Effort medium. `spike/` ostaje netaknut do faze 2 (radni primjer za Neovim).

### Odluke vlasnika (2026-11-11)

- Praćenje promjena na disku: `github.com/fsnotify/fsevents` v0.2.0 (kao Trag).
- Frontend testovi: `vitest` kao razvojna ovisnost (kao Trag). `jsdom` ili bilo što drugo → pitati.
- Radnje u stablu: tipke kad stablo ima fokus — `a` nova datoteka, `r` preimenuj, `d` u Smeće uz
  potvrdu; uz gumb „Novi dokument” i `⌘N`. Bez kontekstnog menija.
- Folderi bez podržanih dokumenata (ni u podfolderima) su skriveni.
- Font Inter (woff2, OFL) iz službenog izdanja rsms/inter; JetBrains Mono se kopira iz `spike/`.

### Ugovor Go servis ↔ frontend

Frontend šalje putanje relativne prema korijenu projekta; Go ih razrješava i provjerava da ostaju
unutar projekta (i nakon `EvalSymlinks`). Redoslijed u stablu određuje frontend
(`Intl.Collator('hr', {numeric: true})`, folderi pa dokumenti).

```go
type ProjectInfo struct { ID, Name, Path string; Missing bool }
type Node struct { Name, Rel, Kind string; Children []Node } // Kind: "dir" | "md" | "txt" | "docx" | "doc"

ProjectService.Projects() []ProjectInfo
ProjectService.AddProject() (ProjectInfo, error)                      // dijalog; odustajanje → prazno, nil
ProjectService.RemoveProject(id string) error                         // samo konfiguracija
ProjectService.Tree(id string) ([]Node, error)
ProjectService.CreateDocument(id, dirRel, name string) (Node, error)  // .md ako ime nema .md/.txt
ProjectService.RenameDocument(id, rel, newName string) (Node, error)  // ekstenzija ostaje
ProjectService.TrashDocument(id, rel string) error                    // potvrda: nativni dijalog u Go-u
```
Događaji: `projects:changed` (payload `""`), `project:changed` (payload ID projekta), te (dodano u
koraku 6) `menu:new-document` (payload `""`).

### Pravila za sve briefove

Bez slanja tipki preko `osascript`-a; bez dodira sistemskog clipboarda; testovi pišu samo u
`t.TempDir()`; pravi `/usr/bin/trash` se u testovima ne poziva (lažni izvršitelj); pravi
`~/Library/Application Support/Pisar` se u testovima ne dira (`PISAR_HOME`).

### Koraci

- [x] 1. Kostur: predložak `svelte` generiran u privremenoj mapi i prenesen u korijen (modul `Pisar`,
      `go 1.27`, Wails v3.1.0); `Taskfile.yml` s `dev`, `build`, `test`, `test:race`, `test:frontend`
      (vitest 5.1.0), `lint`; `.golangci.yml` kao u Tragu; prozor „Pisar” 1280×800, najmanje 1000×600.
      Zadaci imaju `summary`, pa ih prikazuje `task --list-all`, ne `task --list`
- [x] 2. `internal/config`: `Store` nad `<home>/config.json` (verzija 1), projekti, limiti na jednom mjestu
      (debounce 200 ms, 20000 čvorova, 10 s), `PISAR_HOME`, atomski zapis, pokvarena datoteka →
      `config.bad-<vrijeme>.json`, novija verzija se ne dira; 16 testova (14 u ovom koraku, 2 dodana u
      koracima 5 i 6), pokrivenost 83,5 %
- [x] 3. `internal/project` — stablo: `Scan` (podržane vrste, bez skrivenih i `~$…`, linkovi se ne prate ni
      ne prikazuju, prazni folderi izostavljeni, limit čvorova, nečitljiv podfolder ide u `Skipped`) i
      `Resolve` (relativna putanja → putanja unutar projekta; odbija `..`, linkove prema van i viseće
      linkove); 20 testova, pokrivenost 92 %. APFS na ovom stroju normalizira Unicode imena, pa je
      test NFC/NFD preskočen
- [x] 4. `internal/docsafe`: `CreateFile` (isključivo stvaranje, `.md` ako nema `.md`/`.txt`), `Rename`
      (tvrdi link pa brisanje starog imena; promjena samo veličine slova; ekstenzija ostaje), `Trash`
      (`trash -s <putanja>`, zatim provjera da datoteke više nema); 18 testova (17 se izvodi,
      `TestRealTrash` je preskočen), pokrivenost u tom koraku 87,8 %.
      - Pravi `trash` se u testovima ne poziva; `TestRealTrash` radi samo uz `PISAR_TEST_REAL_TRASH=1`.
      - Dvotočka u imenu je zabranjena (Finder je prikazuje kao `/`) — odluka glavne sesije.
      - Preimenovanje na drugi tvrdi link iste datoteke vraća `ErrExists` (ispravak glavne sesije
        nakon pregleda, s testom `TestRenameOntoHardLink`).
      - Na volumenu bez tvrdih linkova ostaje „provjeri pa preimenuj” s malim prozorom utrke; ta
        staza nema test
- [x] 5. `internal/project` — praćenje: `Watcher` (debounce 200 ms, najdulja odgoda 2 s, jedan `OnChange` po
      naletu, projekti neovisni, gašenje bez zaostalih goroutina) i `FSEventsSource` (fsevents v0.2.0,
      latencija 50 ms); 11 testova s lažnim izvorom i 5 s pravim FSEventsom, pokrivenost paketa 94,5 %.
      - Novi limit u konfiguraciji: `WatchMaxDelay` (2 s), da neprekidne promjene ne odgađaju osvježavanje.
      - Zanemaruje se sve što stablo ne prikazuje (skriveno, `~$…`, nepodržane vrste) i događaj za sam
        korijen (FSEvents ga javlja pri pokretanju streama); premješten ili obrisan korijen stiže kao
        `Rescan`.
      - `OnChange` ne smije zvati metode `Watcher`-a (`Remove` i `Close` čekaju na njega)
- [x] 6. `ProjectService` i meni: servis prema ugovoru (`projectservice.go`), Wails dijalozi za folder i
      potvrdu brisanja (`dialogs.go`), meni (`menu.go`), povezivanje i gašenje u `main.go`; 17 Go testova u
      paketu `main` (14 za servis s lažnim dijalozima, izvršiteljem i izvorom promjena, 3 za tekstove).
      - Odstupanje od tablice „Datoteke” u planu: uz `projectservice.go` i `menu.go` nastali su i
        `dialogs.go`, `uitext.go` i `uitext_test.go` (Wails adapter za dijaloge i čitanje tekstova).
      - Odstupanje od ugovora u planu: uz dva događaja postoji i treći, `menu:new-document`
        (payload `""`), koji meni šalje na `⌘N`; metode servisa primaju `context.Context` kao prvi
        argument, koji Wails izostavlja iz bindinga.
      - Tekst sučelja ima JEDAN izvor: `frontend/src/strings.json`. Frontend ga uvozi kroz `strings.ts`,
        Go ga ugrađuje s `go:embed` (`uitext.go`). Time je riješeno odstupanje „tekst na dva mjesta”.
      - Greške prema frontendu nose stabilan kod (`not-found`, `missing`, `too-large`, `invalid-path`,
        `exists`, `invalid-name`, `unsupported-kind`, `not-document`, `not-a-directory`, `canceled`,
        `internal`). U JS stižu kao `RuntimeError` s `cause.code` (Wailsov `MarshalError`) i s tekstom
        koji počinje kodom; čita ih `frontend/src/lib/apiError.ts`.
      - Putanja alata za Smeće je samo u `internal/config` (`DefaultTrashBin`, `Store.TrashBin()`).
      - Greška iz stavke menija „Dodaj projekt…” se samo logira, bez nativne poruke.
      - Nije provjereno bez klikanja: oba dijaloga, stavke menija, poruke o oštećenoj i novijoj
        konfiguraciji (ide na popis za ručni pogled)
- [x] 7. Tema: `theme/tokens.ts` (16 boja iz `DESIGN.md`, tipografija, veličine, razmaci), `cssVars.ts`
      (varijable se postavljaju pri pokretanju), `contrast.ts`, `fonts.ts` (Inter v4.1 400/600 iz
      službenog izdanja rsms/inter, JetBrains Mono 400 iz spikea; registrirani iz TS-a da imena obitelji
      ostanu samo u tokenima); 64 vitest testa.
      - Testovi čuvaju: tokeni = tablica u `DESIGN.md`; kontrast teksta ≥ 4.5:1; nijedna boja ni ime
        fonta izvan `tokens.ts`.
      - Par `text.linenr` na `bg.cursorline` ima 4.25:1 (ispod 4.5). U fazi 1 se ne koristi i nije u
        testu; odluka u fazi 2 uz Neovim colorscheme.
      - `main.go` zadržava bijelu `BackgroundColour` prozora (boja izvan `tokens.ts`): bez nje je
        pozadina prozora prozirna dok webview ne iscrta stranicu. Poznato odstupanje.
      - Testovi čitaju `DESIGN.md` i izvore kroz Viteov `?raw` uvoz, jer projekt nema `@types/node`.
      - Glavna sesija je nakon pregleda ispravila ime varijable prstena fokusa u `app.css`
        (`--size-focus-ring-width`); čuvar nedefiniranih varijabli dolazi u koraku 8
- [x] 8. Raspored i stablo: `App.svelte`, `components/` (`Toolbar`, `Sidebar`, `TreeRow`, `TreeField`,
      `EmptyState`), logika u čistim modulima `lib/tree.ts`, `treeKeys.ts`, `latest.ts`,
      `errorMessage.ts`, stanje u `lib/treeController.svelte.ts`; 143 vitest testa, svelte-check bez
      grešaka i upozorenja.
      - Novi čuvar `theme/usedVariables.test.ts`: svaka `var(--…)` mora postojati u tokenima.
      - Odluke koje plan nije odredio: `d` na projektu uklanja ga s popisa uz potvrdu u stablu;
        projekti su zadano rašireni, folderi skupljeni (ne pamti se između pokretanja); prazno stanje
        bez natuknica `␣f` / `⌘P` do faze 4; `k` bez odabira odabire prvi red.
      - Dok traje jedna radnja servisa (i dok je otvoren nativni dijalog), ostale radnje čekaju.
      - Komponente i `treeController` nemaju testove (vitest radi bez DOM-a; `jsdom` nije odobren).
        Izgled, fokus i tipke provjerava vlasnik ručno
- [x] 9. Kraj faze: `scripts/smoke.sh` prolazi, kriteriji s dokazima i popis za ručni pogled (dolje);
      `plan-reviewer`: krug 1 FAIL (6 manjih odstupanja), krug 2 FAIL (jedna netočna brojka pokrivenosti
      u ovom dokumentu), krug 3 PASS; sažetak predan, STOP.
      - `scripts/smoke.sh` i dijagnostiku `PISAR_LOG=debug` (ispis poziva servisa na stderr; tri mjesta,
        petnaestak redaka) napisala je glavna sesija, da se frontend i osvježavanje mogu dokazati bez
        klikanja.
      - Ispravci nakon kruga 1 pregleda (glavna sesija, oko 30 redaka koda i testova, nešto iznad
        iznimke od 20 redaka iz `PROMPT.md`): preimenovanje u drugu vrstu dokumenta sada vraća
        `unsupported-kind` umjesto `invalid-name` (korisnik dobiva pravu poruku); neuspjelo vraćanje
        nakon neuspjelog preimenovanja više se ne prešućuje; veličina prozora je u `internal/config`
        (`WindowWidth`…`WindowMinHeight`), a nekorišteni tokeni `windowMin*` su uklonjeni; brojevi
        testova u ovom dokumentu su ispravljeni

### Dokazi po kriterijima (2026-11-11)

Naredbe iz korijena repozitorija; sve su pokrenute nakon zadnje izmjene koda.

- `task test` → exit 0: `ok Pisar`, `ok Pisar/internal/config`, `ok Pisar/internal/docsafe`,
  `ok Pisar/internal/project`; vitest `Test Files 12 passed`, `Tests 143 passed`.
- `task test:race` → exit 0, bez `DATA RACE`. `task lint` → `0 issues.`, svelte-check
  `0 ERRORS 0 WARNINGS`. `task build` → exit 0, `bin/pisar`.
- Pokrivenost (`go test -count=1 -cover ./...`): `Pisar` 69,2 %, `config` 83,5 %, `docsafe` 87,1 %,
  `project` 94,5 %. Grana u `docsafe` za neuspjelo vraćanje nakon neuspjelog preimenovanja nema test
  (ne da se pouzdano izazvati), pa je pokrivenost tog paketa pala s 87,8 % na 87,1 %.
  Broj Go testova (`go test -list`): `Pisar` 17, `config` 16, `docsafe` 18, `project` 36 (20 za stablo
  i putanje, 11 za praćenje s lažnim izvorom, 5 s pravim FSEventsom).
- `bash scripts/smoke.sh` → `smoke: all checks passed` (prava aplikacija, bez tipki i klikova):
  frontend je zatražio popis (1 projekt) i stablo (2 stavke: skrivena datoteka i slika izostavljene);
  dokument stvoren izvana osvježio je stablo (3 stavke); slika i `.DS_Store` nisu izazvali
  osvježavanje; SIGTERM gasi aplikaciju; `config.json` nepromijenjen; prava mapa
  `~/Library/Application Support/Pisar` nije nastala.

Uz svaki kriterij iz odjeljka „Kriteriji prihvaćanja” ispod:

1. Struktura i zadaci: `test -d internal/config -a -d internal/project -a -d internal/docsafe -a -f
   frontend/src/theme/tokens.ts` → exit 0; `task --list-all` sadrži `dev`, `build`, `test`, `lint`
   (zadaci imaju `summary`, pa ih `task --list` ne prikazuje). `internal/nvim`, `convert`, `preview`
   i mapa `nvim/` namjerno još ne postoje (dolaze s fazama 2–4).
2. Projekti i konfiguracija: `internal/config` (`TestPersistence`, `TestAddDuplicate`, `TestRemove`,
   `TestDamagedFile`, `TestNewerVersion`); servis (`projectservice_test.go`: dodavanje kroz lažni
   dijalog, odustajanje, uklanjanje, folder koji nestane i vrati se).
3. Stablo i vrste: `internal/project` (`TestScanMixedAndNested`, `TestScanHidden`, `TestScanSymlinks`,
   `TestScanEmptyDirs`); frontend `lib/tree.test.ts` (naziv bez ekstenzije, oznaka vrste, hrvatski
   redoslijed); dimna provjera (2 stavke od 4 datoteke).
4. Nova datoteka, preimenovanje, Smeće: `internal/docsafe` (18 testova, uključujući postojeće ime,
   promjenu samo veličine slova, tvrdi link, izlazak iz projekta, argumente za `trash`); servis
   (kodovi grešaka, potvrda brisanja). Pravi `trash` NIJE pozvan ni u jednom testu.
5. Osvježavanje izvana: `internal/project` (`watch_test.go` s lažnim izvorom, `watch_darwin_test.go` s
   pravim FSEventsom); dimna provjera od diska do frontenda.
6. Tokeni i CSS varijable: `theme/tokens.test.ts` (tokeni = tablica u `DESIGN.md`), `cssVars.test.ts`,
   `contrast.test.ts`, `noHardcodedStyle.test.ts`, `usedVariables.test.ts`; `grep -rnE
   '#[0-9a-fA-F]{3,8}\b' frontend/src` izvan `theme/tokens.ts` i testova → bez pogodaka.

### Ručni pogled — za vlasnika (agent ovo ne može provjeriti)

Pokretanje bez diranja prave konfiguracije: `PISAR_HOME=/tmp/pisar-proba bin/pisar` (ili bez varijable,
tada nastaje `~/Library/Application Support/Pisar/`).

1. „+ Dodaj projekt” i Datoteka → „Dodaj projekt…”: otvara se dijalog za folder s naslovom „Odaberi
   folder projekta”; odustajanje ne mijenja ništa; odabrani folder se pojavi u stablu i ostaje nakon
   ponovnog pokretanja.
2. Stablo prema `DESIGN.md`: traka 48 px, stablo 240 px, naslov „PROJEKTI”, redovi 28 px, naziv bez
   ekstenzije i oznaka vrste desno, odabrani dokument u boji akcenta; skrivene datoteke, slike i
   prazni folderi se ne vide; č ć š ž đ su na pravom mjestu u redoslijedu.
3. Fokus i kretanje: klik u stablo pokaže prsten fokusa; `j` / `k` / strelice; `↵` širi i skuplja
   projekt i folder; tipke s `⌘` stablo ne dira.
4. `a`, gumb „Novi dokument” i `⌘N`: polje u ciljnom folderu; postojeće ime → poruka, polje ostaje;
   `Esc`, prazno ime i klik izvan polja → odustajanje.
5. `r` na dokumentu: naziv bez ekstenzije, cijeli označen; ekstenzija ostaje.
6. `d` na dokumentu: dijalog „Premjestiti u Smeće?” (Return i Esc = „Odustani”); nakon potvrde je
   dokument stvarno u Smeću, a odabir prijeđe na susjedni red.
7. `d` na projektu: potvrda u stablu; folder ostaje na disku.
8. Promjene iz Findera (nova datoteka, brisanje, preimenovanje) pojave se same; preimenovan folder
   projekta dobije napomenu „folder nije dostupan” i oporavi se kad ga vratiš i vratiš fokus u prozor.
9. Najmanja veličina prozora 1000×600; meni Uređivanje (kopiraj, zalijepi, poništi) radi u poljima.

### Izvan opsega

Otvaranje dokumenta u Neovimu, terminal, `internal/nvim`, prijenos i brisanje `spike/`, konverzija
`.docx` / `.doc` i novi `.docx`, pregled, tražilica, stanje „Izmijenjeno / Spremljeno”, radnje nad
folderima, povlačenje, kontekstni meni, hrvatski meni umjesto Wailsovih uloga, tamna tema, pakiranje.

### Kriteriji prihvaćanja

Dokazi su u odjeljku „Dokazi po kriterijima” gore. Dio koji traži klikanje (dijalog za folder, tipke u
prozoru, stvarno Smeće) vlasnik je provjerio ručno 2026-11-11 i potvrdio da radi; agent to nije provjeravao.

- [x] struktura repozitorija iz `CLAUDE.md`, `task dev/build/test/lint` — dokaz 1
- [x] dodavanje i uklanjanje projekta (odabir foldera); konfiguracija preživljava ponovno pokretanje —
      dokaz 2; odabir foldera kroz nativni dijalog potvrdio vlasnik
- [x] stablo prikazuje samo podržane vrste, s oznakom vrste; skrivene datoteke i folderi su izostavljeni
      — dokaz 3 (izgled stabla je u ručnom pogledu, stavka 2)
- [x] nova datoteka, preimenovanje, brisanje u Trash — dokaz 4; pravi `trash` i tipke `a` / `r` / `d` u
      prozoru potvrdio vlasnik
- [x] stablo se osvježava kad se datoteke promijene izvana — dokaz 5 (dimna provjera, od diska do frontenda)
- [x] `theme/tokens.ts` postoji i iz njega izlaze CSS varijable — dokaz 6

## Faza 2 — `.md` i `.txt`

Kod iz `spike/` se prepisuje u pravu aplikaciju; `spike/` se briše zadnjim korakom faze.

### Odluke vlasnika (2026-11-11)

- Novi token `danger` = `#B42318` (greške) u `DESIGN.md` i `tokens.ts`.
- Broj retka na retku kursora je u boji `text`; ostali brojevi ostaju `text.linenr`.
- `↵` / klik na `.docx` i `.doc`: red se odabere, ispod njega poruka da otvaranje dolazi u sljedećoj fazi.
- `spike/` se briše na kraju faze (`git rm -r spike`), kad sve provjere prođu.
- Meni „Uređivanje” postaje vlastiti (samo „Kopiraj” i „Zalijepi”): u poljima za unos više nema `⌘Z` i `⌘A`.

### Ovisnosti (odobrene)

- Go: `github.com/creack/pty` v1.1.24, `github.com/neovim/go-client` v1.2.1
- npm: `@xterm/xterm` 6.0.0, `@xterm/addon-fit` 0.11.0 (bez WebGL dodatka)

### Gdje živi Neovimovo stanje

Sve pod `<home>/nvim/` (`<home>` = `~/Library/Application Support/Pisar` ili `PISAR_HOME`): `XDG_CONFIG_HOME`,
`XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME` na podmape, `NVIM_APPNAME=pisar`. `init.lua` i
colorscheme aplikacija zapisuje pri pokretanju. Vlasnikova Neovim konfiguracija se ne učitava. RPC
socket ide u kratku privremenu mapu; zaostale mape se čiste pri pokretanju. Imena i putanje su u
`internal/config`.

### Ugovor Go ↔ frontend

```go
EditorService.Start(cols, rows int, colorscheme string) error // pokreće Neovim ako ne radi; inače Resize + Redraw
EditorService.Write(data string) error
EditorService.Resize(cols, rows int) error
EditorService.OpenDocument(projectID, rel string) error        // samo .md i .txt; .docx/.doc → unsupported-kind
EditorService.Save() error
EditorService.Copy() (handled bool, err error)                 // vizualni odabir u Neovimu → "+y
EditorService.CopyText(text string) error                      // odabir iz xterm.js-a
```
Događaji (payload string; složeni kao JSON): `pty:data` · `nvim:exit` · `document:active`
(`{projectId, rel, path, name}`) · `document:state` (`{path, modified}`) · `ui:focus-tree` · `menu:copy`.

Odstupanja od ugovora u planu (izvedeno stanje):

- Dodana je vezana metoda `EditorService.HasInitialDocument() bool` (izvan plana): kaže frontendu da je
  na naredbenom retku zadan dokument, pa editor treba pokrenuti odmah. Dokument zatim otvara Go
  nakon prvog uspješnog `Start`.
- Plan kaže „pokretanje na prvi `OpenDocument`”. Izvedeno je drugačije: Neovim pokreće FRONTEND pozivom
  `Start` (terminal mora prvo izmjeriti veličinu mreže), a `OpenDocument` bez pokrenutog Neovima vraća
  `not-running`. Za korisnika je isto: prvo otvaranje dokumenta pokreće editor (`lib/editorState.ts`
  pamti zahtjev i izvršava ga nakon uspješnog pokretanja).
- `document:active` i `document:state` su tipizirane strukture, ne JSON stringovi.
- Metode servisa primaju `context.Context` kao prvi argument (Wails ga izostavlja iz bindinga).
- Kodovi grešaka dodani u fazi 2: `not-running`, `nvim-missing`, `unnamed-buffer`, `unsaved-changes`.

Datoteke nastale ili mijenjane izvan tablice „Datoteke” u planu: `colorscheme.go`, `menu_test.go`,
`testdata/colorscheme/pisar.lua`; `components/EditorPane.svelte`, `EmptyState.svelte`;
`lib/activeDocument.ts`, `editorState.ts`, `copyAction.ts`, `focus.ts`, `apiError.ts`, `errorMessage.ts`
(s testovima); `theme/cssVars.ts`, `theme/fonts.ts`, token `bold` u `tokens.ts`,
`assets/fonts/JetBrainsMono-Bold.woff2`; `internal/term` i `internal/nvim` su dobili i metode za korak
6b. S popisa „ne diram” dirano je samo ono što je vlasnik izričito tražio: effort u `PROMPT.md`,
`.claude/agents/coder.md` i jedan redak plana (commitovi `3aafaaf`, `2229409`; vidi „Effort” u
zaglavlju). `internal/docsafe` i `CLAUDE.md` su netaknuti; `DESIGN.md` ima točno jedan novi redak (`danger`).

### Koraci

- [x] 1. `internal/nvim`: prijenos iz spikea (gašenje oba procesa, odgovori na upite terminala i ostalo
      dokazano ponašanje nepromijenjeni) + događaji `EventBufferEntered`, `EventModifiedChanged`,
      `EventFocusTree`, metode `Current`, `Modified`, `SaveAll`; `nvim/init.lua`; 43 testa s pravim
      Neovimom, pokrivenost 88,4 %. Vremenska ograničenja su u `internal/config`
      (`NvimStartTimeout` 10 s, `NvimStopTimeout` 3 s). Prvi prolaz radio je `coder` na `medium`,
      doradu nakon pregleda na `high`.
      - Neovim 0.12 ne okida `BufModifiedSet` kad se zapiše buffer koji nije trenutačan; `init.lua`
        zato stanje izmjene šalje i iz `BufWritePost`.
      - Poruka šira od retka za poruke, ispisana unutar RPC poziva, otvara hit-enter prompt i
        zamrzava Neovim. Greške se zato skraćuju na širinu retka (`columns` − 12, odnosno − 30 bez
        statusne linije; izmjereno na 11 kombinacija opcija), a Go dobiva puni tekst. Uspješne
        poruke `:write` i `:edit` Neovim skraćuje sam (`shortmess` `t`/`T`, izričito u `init.lua`).
      - **Opasnost od gubitka teksta, nađena i zatvorena:** dok stoji pitanje o swap datoteci, `Save`
        je zapisivao prazan, još neučitan buffer preko prave datoteke. `Save` sada odbija pisati dok
        je bilo koji prompt otvoren (`TestSwap`, `TestSaveAsk`).
      - Swap pitanje (nakon rušenja s nespremljenim izmjenama) se namjerno NE utišava. Dok stoji:
        `OpenFile` se vraća s istekom konteksta, ostali pozivi ne traju dulje od svog konteksta,
        `Close` završava i ne ostavlja procese. Za politiku oporavka (kasnija faza): u terminalu od
        24 retka pitanje počinje s „-- More --”, a tipka `q` ondje ugasi cijeli Neovim.
- [x] 2. `internal/term`: `Session` i grupiranje izlaza preneseni iz spikea; `Emitter` je sada sučelje s
      tipiziranim metodama (`Data`, `Saved`, `Active`, `Modified`, `FocusTree`, `Exited`); nove metode
      `Running`, `Current`, `Modified`, `SaveAll`; 32 testa, pokrivenost 93,7 %. Ograničenja grupiranja
      su u `internal/config` (`TermFlushInterval` 8 ms, `TermMaxChunk` 16 KiB).
      - Bez procesa `Modified` i `Current` vraćaju prazno bez greške (treba sloju za gašenje).
      - `Exited()` je zajamčeno zadnji poziv za proces koji je sam završio.
      - `Close` se ne smije zvati iz metode `Emitter`-a (čeka da poziv završi)
- [x] 3. Tema za terminal i Neovim: token `danger` (`#B42318`; jedan novi redak u `DESIGN.md`, kontrast
      5,65–6,57 na podlogama), `theme/terminalTheme.ts` (xterm.js tema + 16 ANSI boja iz tokena),
      `theme/nvimColorscheme.ts` (colorscheme `pisar`: sučelje, sintaksa, Markdown klasično i
      treesitter); 298 vitest testova. U pravom Neovimu 0.12 učitava se bez greške, `Normal`,
      `CursorLineNr`, `ErrorMsg`, `Visual` i Markdown naslov imaju boje tokena (ručna provjera agenta).
      - Odluke koje plan nije odredio: `Visual` i `Search` izričito postavljaju boju teksta (inače
        `syntax.list` na `bg.selected` ima samo 4,03:1); `Search` je podcrtan da se razlikuje od
        odabira; magenta = `accent`, cijan = `syntax.code` (nema tih tokena); `Delimiter` je
        `text.muted`; `Diff*` koriste `bg.chrome` i `bg.selected`; poruke o grešci u stablu su `danger`.
      - Jedina iznimka kontrasta: `WinSeparator` (crta između prozora, nije tekst).
      - Nije pokriveno: LSP i `Diagnostic*` grupe (Pisar ih ne koristi), `g:terminal_color_*`
- [x] 4. `EditorService` (`editorservice.go`): servis prema ugovoru; Neovimovo stanje pod `<home>/nvim/`
      (`XDG_*`, `NVIM_APPNAME=pisar`); `init.lua` i colorscheme se zapisuju pri pokretanju; putanja
      buffera se preslikava u projekt (najdublji korijen, kroz simboličke linkove); 51 Go test u paketu
      `main`, pokrivenost 78,6 %; vitest 305.
      - Događaji `document:active` i `document:state` su tipizirane strukture (u JS stižu kao objekti
        `{projectId, rel, path, name}` i `{path, modified}`).
      - **Colorscheme se provjerava prije izvršavanja** (`colorscheme.go`): prihvaćaju se samo točni
        redci zaglavlja i redci `vim.api.nvim_set_hl(0, "<ime>", { … })` s bojama `#RRGGBB`, `true` ili
        imenom grupe; sve ostalo se odbija i Neovim se ne pokreće.
      - `testdata/colorscheme/pisar.lua` je stvarni izlaz generatora (vitest `toMatchFileSnapshot`);
        Go testovi ga učitavaju u pravi Neovim i provjeravaju boje `Normal`.
      - Novi kodovi grešaka: `not-running`, `nvim-missing`, `unnamed-buffer`.
      - Socket je u `$TMPDIR/pisar-run-<pid>-…` (putanja oko 95 od dopuštenih 103 bajta); zaostale
        mape mrtvih procesa čiste se pri pokretanju.
      - Ispravci glavne sesije nakon pregleda (s testovima `TestEdActiveState`, `TestEdRunDirGone`):
        uz `document:active` šalje se i `document:state`, da traka ne zadrži stanje napuštenog buffera;
        mapa socketa se ponovno stvara ako ju je sustav očistio dok Neovim nije radio.
      - `⌘S` na bufferu bez imena vraća kod `internal`; korisnik vidi E32 u samom Neovimu.
      - `exec.ErrNotFound` se općenito preslikava u `nvim-missing`; kad dođe pandoc, treba vlastitu
        grešku.
      - `Copy` s pravim vizualnim odabirom nije testiran na ovoj razini (dirao bi sistemski
        clipboard); pokriva ga `internal/nvim` s lažnim clipboardom
- [x] 5. Terminal u frontendu: `components/Terminal.svelte` (xterm.js 6.0.0 + fit 0.11.0, DOM renderer,
      tema i font iz tokena, serijsko slanje tipki, kalibracija visine retka), `EditorPane.svelte`
      (prazno stanje / terminal / poruka o grešci), `lib/editorState.ts` (čisti stroj stanja) i
      `lib/editorController.svelte.ts`; JetBrains Mono Bold (700) za podebljanje; vitest 339.
      - Dimna provjera prave aplikacije (glavna sesija, bez tipki): `bin/pisar <dokument>` → frontend
        zove `Start cols=133 rows=35`; Neovim preko socketa javlja `133 35 proba.md pisar`, `Normal`
        `1D2125` na `FFFFFF`, stanje pod `<home>/nvim/`; nakon gašenja nema procesa ni socketa.
      - Ispravak glavne sesije (s testom `TestEdRestartReports`): kad se stranica ponovno učita dok
        Neovim radi, `Start` ponovno javlja aktivni dokument i njegovo stanje.
      - Tipke i `Resize` pristigli prije nego `Start` završi se odbacuju.
      - Komponente i `editorController` nemaju testove (vitest bez DOM-a); logika je u `editorState`
- [x] 6. Otvaranje i praćenje (frontend): `↵` / klik na `.md` / `.txt` zove `editor.open` i fokus prelazi
      u terminal; `.docx` / `.doc` daju prigušenu poruku bez poziva servisa; aktivni dokument je u
      akcentu (`aria-current`), odabrani red u `bg.selected`; stablo se raširi do aktivnog dokumenta i
      odabir ga prati; alatna traka pokazuje naziv, putanju i „Izmijenjeno” / „Spremljeno”
      (`lib/activeDocument.ts`); vitest 362.
      - Promjene prema fazi 1: akcentom je ispunjen AKTIVNI dokument (ne odabrani); traka pokazuje
        aktivni dokument (ne odabrani red). Očekivanja u `treeKeys.test.ts` i `tree.test.ts` su
        prilagođena tome.
      - Otvaranje nije pod zaštitom „jedna radnja servisa odjednom”, da pokretanje Neovima ne zaključa
        stablo; vrijedi samo najnoviji pokušaj.
      - Dimna provjera agenta (bez tipki; naredbe Neovimu preko njegova socketa): promjena buffera →
        `document:active` s novim `rel`; izmjena → `modified=true`; zapis → `modified=false`, sadržaj
        na disku promijenjen.
- [x] 6b. (dodano izvan plana, odobrio vlasnik 2026-11-11) Preimenovanje i Smeće za dokument otvoren u
      Neovimu: s nespremljenim izmjenama se odbija (kod `unsaved-changes`, poruka „Spremi ga (⌘S) pa
      pokušaj ponovno”), i to prije izmjene na disku odnosno prije dijaloga potvrde; bez izmjena
      Neovim zatvara stari buffer (`:bwipeout`) i, ako je bio aktivan, otvara dokument pod novim
      imenom. Novo: `nvim.Process.Buffers` i `Wipe`, `openDocuments` sučelje u `projectservice.go`;
      Go testovi: `main` 59, `internal/nvim` 51, `internal/term` 33; vitest 364.
      - Test s oba prava servisa i pravim Neovimom: nakon preimenovanja otvorenog `a.md` u `b`, izmjena
        i `Save` završavaju u `b.md`, a `a.md` ne nastaje ponovno.
      - Ako radnja na disku uspije, a obavještavanje editora ne, radnja se ne poništava: greška se
        logira, stablo se osvježi (disk je istina).
      - Poznato: usporedba putanja je osjetljiva na veličinu slova; promjene iz Findera (izvan
        aplikacije) nisu pokrivene ovim korakom
- [x] 7. Meni, prečaci i fokus (rađeno u jednom briefu i jednom commitu s korakom 8, jer diraju iste
      datoteke): „Datoteka” → „Spremi” `⌘S` (Go → RPC, bez JavaScripta); vlastiti meni „Uređivanje” s
      „Kopiraj” `⌘C` (događaj `menu:copy`; frontend odlučuje po fokusu: terminal → vizualni odabir u
      Neovimu, inače odabir xterm.js-a; polje za unos → označeni tekst) i „Zalijepi” `⌘V` (uloga
      sustava); bez `⌘Z` / `⌘X` / `⌘A`. `␣e` fokusira stablo, `Esc` u stablu vraća fokus terminalu dok
      Neovim radi, nakon `nvim:exit` fokus ide u stablo. Logika u `lib/copyAction.ts` i `lib/focus.ts`.
      - Dodano izvan briefa: `macOptionClickForcesSelection` u terminalu, jer se bez njega odabir
        mišem ne može forsirati dok Neovim prati miš (`⌥` + povlačenje).
      - Nije potvrđeno bez tipkovnice: da WKWebView bez stavki menija stvarno ne reagira na `⌘Z` /
        `⌘X` / `⌘A`; ide na popis **[R]**.
- [x] 8. Gašenje (`quit.go`): `⌘Q` i crveni gumb idu kroz isti `quitGate`. Zahtjev se prvo odbije, Neovim
      se u pozadini pita za izmijenjene buffere (ne na UI niti); bez izmjena aplikacija se gasi, s
      izmjenama dijalog Spremi / Odbaci / Odustani s popisom naziva (najviše 5, zatim „…i još N”);
      neuspjelo spremanje ne gasi aplikaciju i pokaže razlog; ako Neovim ne odgovori, zaseban dijalog
      „Uređivač ne odgovara…”. 13 testova u `quit_test.go`; Go testovi u `main`: 78; vitest 388.
      - Glavna sesija je nakon pregleda premjestila broj naziva u dijalogu u `internal/config`
        (`QuitDialogNames`).
      - Nije potvrđeno bez klikanja: sam dijalog, raspored gumba i `Esc` kao „Odustani” (Wails koristi
        `Esc` samo za gumb koji nije zadani; isto vrijedi za potvrdu Smeća iz faze 1)
- [x] 9. Dimne provjere prave aplikacije (napisala glavna sesija; bez tipki i klikova, Neovim se vodi
      preko svog socketa): `scripts/smoke.sh` (24 provjere: stablo, pokretanje editora s dokumentom
      s naredbenog retka, veličina mreže, colorscheme i boje `Normal`, stanje Neovima pod `<home>`,
      promjena buffera, izmjena i zapis, `␣e`, gašenje bez ostataka) i `scripts/shutdown.sh` (SIGTERM,
      SIGINT, SIGHUP, SIGKILL; nakon rušenja sljedeće pokretanje čisti zaostale mape socketa).
- [x] 10. Kraj faze: `spike/` uklonjen (`git rm -r spike`, commit `0ec6386`; preostali generirani ostaci
      na disku obrisani), kriteriji s dokazima i popis **[R]** (dolje); `plan-reviewer`: krug 1 FAIL
      (8 odstupanja: 3 u kodu, 5 u ovom dokumentu; ispravljena), krug 2 FAIL (simbolički link kao
      početni dokument; jedna netočna rečenica u ovom dokumentu; ispravljeno), krug 3 PASS; sažetak
      predan, STOP.
      - Ispravci u kodu nakon kruga 1 (`coder` na `high`): dokument s naredbenog retka se prihvaća samo
        ako je `.md` ili `.txt` (inače se zanemaruje uz zapis u log; zajednička provjera
        `neovimMayEdit` s `OpenDocument`; testovi `TestEdInitialRefused`, `TestEdInitialAccepted`);
        `SaveAll` odbija pisati dok je u Neovimu otvoren prompt, kao `Save` (izmjereno: napola učitan
        buffer nije `modified`, ali bi se bez zaštite usred pitanja zapisao DRUGI izmijenjeni buffer);
        ispravljen komentar u `colorscheme.go`.
      - Poznato nakon tih ispravaka: kad „Spremi” pri gašenju ne uspije jer je u Neovimu otvoren
        prompt, dijalog pokaže sirovi tekst greške s Lua tragom (ružno, ali točno); `Wipe` ima vlastitu
        kopiju provjere prompta.
      - Ispravak nakon kruga 2 (glavna sesija, desetak redaka + test `TestEdInitialSymlink`): početni
        dokument mora biti obična datoteka (`Lstat`) ili još ne postojati; simbolički link `x.md` na
        `.docx` se odbija, jer bi Neovimov `:w` pisao kroz link u `.docx` mimo `internal/docsafe`

### Dokazi po kriterijima (2026-11-11)

Naredbe iz korijena repozitorija, pokrenute nakon zadnje izmjene koda i nakon uklanjanja `spike/`.

- `task test` → exit 0 (Go paketi `ok`; vitest `Test Files 19 passed`, `Tests 388 passed`);
  `task test:race` → exit 0; `task lint` → `0 issues.`, svelte-check `0 ERRORS 0 WARNINGS`;
  `task build` → exit 0.
- Go testovi (`go test -list`): `Pisar` 81, `config` 22, `docsafe` 18, `nvim` 51, `project` 36,
  `term` 33; preskočeni samo `TestRealTrash` i `TestScanNFCandNFD`. Pokrivenost
  (`go test -count=1 -cover ./...`, nakon ispravaka iz kruga 2): `Pisar` 77,6 %, `config` 83,9 %,
  `docsafe` 87,1 %, `nvim` 87,3–88,3 %, `project` 94,5 %, `term` 93,3 %. Pokrivenost paketa `nvim` se
  mijenja između pokretanja (tri uzastopna: 88,3 %, 87,3 %, 88,3 %), jer grane ovise o tome koja faza
  gašenja pobijedi.
- `bash scripts/smoke.sh` → `smoke: all checks passed`; `bash scripts/shutdown.sh` →
  `shutdown: all checks passed`. `test ! -e spike` → exit 0. `pgrep -fl 'nvim|bin/pisar'` → ništa.
  `~/Library/Application Support/Pisar`, `~/.local/state/pisar`, `~/.config/pisar` ne postoje.

Uz svaki kriterij iz odjeljka „Kriteriji prihvaćanja” ispod:

1. Otvaranje iz stabla: Go `TestEdOpen`, `TestEdOpenErrors`, `TestEdOpenNotRunning` (pravi Neovim;
   `.docx` → `unsupported-kind`); vitest `treeKeys.test.ts` i `activeDocument.test.ts` (`↵` i klik →
   radnja otvaranja); `smoke.sh`: Neovim javlja `poglavlje.md`. Sam klik i `↵` u prozoru: **[R]** stavka 1.
2. Aktivni dokument kad se buffer promijeni iz Neovima: Go `TestEdSwitch`, `TestEdNestedOpen`,
   `TestEdLocate`, `TestEdActiveState`; vitest `activeDocument.test.ts` (označavanje, širenje do
   dokumenta, odabir); `smoke.sh`: `:edit` poslan Neovimu → `document:active` s novim `rel`. Boja reda
   u stablu: **[R]** stavka 2.
3. „Izmijenjeno” / „Spremljeno”: Go `TestEdSave`, `TestEdActiveState`, `internal/nvim` `TestModEvents`,
   `TestOrder`; vitest `editorState.test.ts`, `activeDocument.test.ts` (tekst stanja); `smoke.sh`:
   izmjena → `modified=true`, zapis → `modified=false` i tekst na disku. Prikaz u traci: **[R]** stavka 3.
4. Potvrda pri gašenju: `quit_test.go` (13 testova: editor ne radi, bez izmjena, Spremi, greška spremanja, Odbaci,
   Odustani, Neovim ne odgovara, drugi zahtjev dok je dijalog otvoren, popis naziva). Sam dijalog:
   **[R]** stavka 6.
5. Tema i colorscheme iz tokena: vitest `terminalTheme.test.ts`, `nvimColorscheme.test.ts`
   (svaka boja je token; kontrast ≥ 4.5:1), čuvari boja; Go `TestEdScheme`, `TestEdSampleAccepted`,
   `TestEdBadScheme` (pravi Neovim učita `testdata/colorscheme/pisar.lua`; nevaljan tekst se odbija);
   `smoke.sh`: `g:colors_name` je `pisar`, `Normal` je `1D2125` na `FFFFFF`.
6. **[R]** fokus — ne označavam; popis dolje.

### Ručna provjera [R] — za vlasnika

Pokretanje bez diranja prave konfiguracije: `PISAR_HOME=/tmp/pisar-proba bin/pisar`, dodaj projekt
koji ima `.md`, `.txt` i `.docx` (ili pokreni `bin/pisar <dokument.md>`).

| # | Što napraviti | Što treba vidjeti |
|---|---|---|
| 1 | klik na `.md`; `j`/`k` do `.txt` pa `↵`; klik i `↵` na `.docx` | dokument se otvori u Neovimu i odmah se može tipkati; za `.docx` prigušena poruka ispod reda, Neovim se ne mijenja |
| 2 | u Neovimu `:e <drugi.md>` i `:bnext` | akcent u stablu prijeđe na taj red; skupljen folder se raširi |
| 3 | `ihello<Esc>`, zatim `:w` | traka: „Izmijenjeno” (podebljano), pa „Spremljeno”; naziv bez ekstenzije i „Projekt / folder” |
| 4 | **[R] kriterij:** s fokusom u terminalu tipkaj `j`, `k`, `a`, `r`, `d`; s fokusom u stablu iste tipke | stablo se ne miče dok tipkaš u terminalu; Neovim ne dobiva ništa dok si u stablu |
| 5 | `␣e` u Neovimu; `Esc` u stablu; klik u terminal i u stablo; `:q` | fokus prelazi kako piše; nakon `:q` prazno stanje i fokus u stablu |
| 6 | izmjena pa `⌘Q`, i isto crvenim gumbom; sva tri izbora; bez izmjena | dijalog s nazivom dokumenta; „Spremi” sprema i gasi, „Odbaci” gasi, „Odustani” ostaje; bez izmjena gasi odmah |
| 7 | `⌘S` u normal, insert i visual modu te s fokusom u stablu | sprema, mod ostaje isti |
| 8 | `v` + odabir + `⌘C`; odabir mišem uz `⌥` + `⌘C`; `⌘V` u insert modu (više redaka) | tekst je u clipboardu odnosno zalijepljen bez stepenastog uvlačenja |
| 9 | `⌘Z`, `⌘X`, `⌘A` u terminalu | ne događa se ništa |
| 10 | `r` na OTVORENOM dokumentu bez izmjena; isto s nespremljenim izmjenama; `d` isto | bez izmjena: Neovim nastavlja na novom imenu; s izmjenama: poruka „Spremi ga (⌘S)…”, ništa se ne mijenja |
| 11 | izgled terminala | svijetla podloga, JetBrains Mono, naslovi u akcentu, oznake lista narančaste, kod zelen, broj trenutačnog retka taman, redak oko 21 px; povlačenje ruba prozora preslaguje Neovim |
| 12 | `⌘C` / `⌘V` u polju za preimenovanje | rade; `⌘Z` i `⌘A` u polju više ne rade (odluka uz plan) |

Kriterij **[R]** iz plana je stavka 4; ostale stavke su ono što se inače ne da provjeriti bez tipkovnice.

Ostaci faze 0 izvan repozitorija (nisam ih brisao): `rm -r ~/.local/state/pisar-spike ~/.local/share/pisar-spike`.

### Izvan opsega

Konverzija `.docx` / `.doc`, „Samo za čitanje”, oporavak radne kopije, pregled, tražilica, postavke za
prozu i pravopis, `user.lua`, WebGL, zaslon „nedostaje `nvim`”, hrvatski nazivi u Wailsovim ulogama
menija, tamna tema, pakiranje, brisanje `~/.local/state/pisar-spike/` i `~/.local/share/pisar-spike/`.

### Kriteriji prihvaćanja

Dokazi su u odjeljku „Dokazi po kriterijima” gore. Ono što traži gledanje i tipkanje u prozoru (klik,
boja reda, tekst u traci, nativni dijalog, fokus) vlasnik je provjerio ručno 2026-11-11 i potvrdio da
radi; agent to nije provjeravao.

- [x] klik ili `↵` u stablu otvara dokument u Neovimu — dokaz 1; klik i `↵` u prozoru potvrdio vlasnik
- [x] aktivni dokument je označen u stablu i kad se buffer promijeni iz Neovima — dokaz 2; boju reda
      potvrdio vlasnik
- [x] alatna traka prikazuje „Izmijenjeno” / „Spremljeno” prema stanju buffera — dokaz 3; prikaz u traci
      potvrdio vlasnik
- [x] gašenje s nespremljenim izmjenama traži potvrdu — dokaz 4; sam dijalog potvrdio vlasnik
- [x] xterm.js tema i Neovim colorscheme generirani su iz tokena — dokaz 5
- [x] **[R]** fokus: tipke iz terminala ne aktiviraju stablo, i obratno — potvrdio vlasnik ručno 2026-11-11

## Faza 3 — `.docx` i `.doc`

Effort high. Preduvjet: `pandoc` instaliran (3.13 u `/opt/homebrew/bin`). Plan faze odobren 2026-11-11.

### Odluke vlasnika (2026-11-11)

- Uvoz `.doc` ostaje, uz jasno upozorenje da je uvoz pojednostavio strukturu; gubici idu u
  `docs/known-losses.md`.
- Probni dokumenti: generirani skup u repozitoriju (`testdata/docx/`) + vlasnikovi pravi Wordovi
  dokumenti za ručno mjerenje gubitaka. Vlasnikovi dokumenti ne ulaze u repozitorij; mapa se dogovara
  prije koraka 8.

### Ovisnosti (odobrene)

- Go: `golang.org/x/sys` postaje izravna ovisnost (ista verzija koja je već neizravna, v0.48.0), za
  prijenos xattr-a pri atomskom zapisu.
- Vanjski alati: `pandoc`, `textutil` (oba iz stacka). Nema novih npm ovisnosti.

### Izmjereno prije plana (pandoc 3.13, `textutil`)

- `--reference-doc=<izvornik>` pri svakom spremanju prenosi i slike izvornika (1 → 2 → 3 → 4). S
  referentnim dokumentom bez `word/media/` tri su spremanja dala bajt-identičan `.docx` s jednom slikom.
- Zadani `markdown` mijenja `”` u `"` i `—` u `---`; s `-smart` znakovi ostaju doslovni.
- Zadane (multiline) tablice mijenjaju tekst pri svakom krugu. Pipe tablice su stabilne; tablica sa
  složenim ćelijama u tom obliku postaje sirovi HTML koji se pri md → docx gubi. Grid tablicama se
  širine stupaca promijene jednom pa miruju. `gfm` gubi slike.
- `--extract-media=.` daje `./media/rIdN.png`; ime slike se mijenja pri svakom ponovnom čitanju
  (`rId13` → `rId26`).
- Obično čitanje tiho odbacuje komentare i obrisani tekst praćenih izmjena. `word/comments.xml`
  postoji i bez komentara, pa otkrivanje gleda elemente (`w:comment`, `w:ins`, `w:del`,
  `w:txbxContent`). pandoc zna zapisati komentare i praćene izmjene (za probne dokumente).
- `textutil -convert docx` zadrži tekst, a izgubi naslove, liste, tablice, poveznice i fusnote; vraća
  0 i kad ne uspije.

### Odstupanja od dizajna (odobrena planom faze)

- 4.3, korak 5: referentni dokument nije izvornik nego njegova kopija u radnoj mapi. **Ispravak plana
  (vlasnik odobrio 2026-11-11, pitanje tijekom koraka 2):** iz kopije se uklanjaju samo slike TIJELA
  dokumenta i njihove veze u `word/_rels/document.xml.rels`; slike zaglavlja i podnožja ostaju.
  Izmjereno na dokumentu s logom u zaglavlju: s kopijom bez ijedne slike (kako je pisalo u planu)
  logo nestaje, a u spremljenom dokumentu ostaje veza na sliku koje nema (i u `basic.docx`). S užom
  varijantom: logo bajt-identičan, jedna slika u tijelu, tri spremanja bajt-identična, nema visećih veza.
  Dodaje se probni dokument `header-image.docx`.
- 4.2: format je `markdown-smart` s pipe tablicama; grid tablice samo kad dokument ima složene ćelije.
- §5: kad `pandoc` nije instaliran, u ovoj fazi je to poruka o grešci pri otvaranju; zaslon i provjera
  pri pokretanju su u fazi 6.

### Kako radi

- Otvaranje: pregled arhive → referentni dokument bez slika → pandoc u `<home>/work/<id>/` (`doc.md`,
  `media/`, `reference.docx`, `meta.json` s veličinom, vremenom i hashom izvornika) → Neovim otvara
  `doc.md`. Stablo i alatna traka pokazuju izvorni dokument.
- Samo za čitanje: nađeni gubici, datoteka bez dozvole pisanja ili s više tvrdih linkova. Traka kaže
  razlog i nudi „Spremi kao kopiju”.
- Spremanje: konflikt (Prepiši / Učitaj ponovno / Odustani) → backup u `<home>/backups/` pri prvom
  prepisivanju u sesiji (zadnjih 10 po dokumentu) → pandoc u privremenu datoteku u istom folderu →
  provjera arhive → `fsync` + atomski `rename` s dozvolama i xattr-ima. „Učitaj ponovno” prije zamjene
  ostavlja dotadašnju radnu kopiju kao `doc.prev.md`.
- `.doc`: `textutil` → privremeni `.docx` u radnoj mapi → dalje kao `.docx`; prvo spremanje stvara
  `Ime.docx` pored izvornika (ako postoji, pita za ime). Izvorni `.doc` se ne dira.
- Oporavak: spremljena, a neizvezena radna kopija pri otvaranju nudi Oporavi / Odbaci.

### Pravila za sve briefove

Kao u fazi 2 (bez tipki i klikova preko `osascript`-a, bez sistemskog clipboarda, testovi samo u
`t.TempDir()`, pravi Pisarov home se ne dira, pravi `trash` se ne poziva), uz: Word se ne pokreće
skriptom; vlasnikovi pravi dokumenti se samo čitaju i prije obrade kopiraju u privremenu mapu.

### Koraci

- [x] 1. Konfiguracija (putanje `work/` i `backups/`, imena alata, ograničenja) i probni dokumenti
      (`scripts/make-fixtures.sh`, `testdata/docx/`). `config`: `PandocBin()`, `TextutilBin()`, `WorkDir()`,
      `BackupsDir()`, `Limits.ConvertTimeout` (60 s), `Limits.MaxBackups` (10). Probni dokumenti:
      `basic.docx`, `complex-table.docx`, `comments.docx`, `tracked-changes.docx`, `textbox.docx` (VML
      okvir umetnut u pandocov izlaz), `legacy.doc` (`textutil -convert doc`); izvori u `testdata/docx/src/`.
      Skripta ima 23 provjere i bajt-stabilan izlaz (`SOURCE_DATE_EPOCH`, fiksna vremena u arhivi).
      - Sintaksa za pandoc: `[tekst komentara]{.comment-start id="0" author="A" date="…"}raspon[]{.comment-end id="0"}`,
        `[tekst]{.insertion author="A" date="…"}`, `[tekst]{.deletion author="A" date="…"}`.
      - `footnotes.xml` postoji i bez fusnota (kao i `comments.xml`); provjera gleda `<w:footnoteReference`.
      - `textutil` iz `legacy.doc` gubi naslov, poveznicu, tekst fusnote i strukturu tablice; listu zadrži
        kao natuknice „•”.
      - Ispravak glavne sesije (5 redaka): `valid_docx` je imao `unzip -Z1 | grep -qx` pod `pipefail`, pa je
        provjera povremeno padala; popis sada ide u datoteku. 25 uzastopnih pokretanja bez pada.
- [x] 2. `internal/convert`: pregled dokumenta. `Inspect(ctx, path, InspectLimits) ([]Loss, error)`,
      `Loss` = `comments` / `tracked-changes` / `text-boxes`, greške `ErrNotDocx`, `ErrTooLarge`. Strujno
      čita sve XML dijelove izravno u `word/` (i zaglavlja, podnožja, fusnote), uspoređuje prostor imena
      + lokalno ime (prefiks ne mora biti `w`; i strogi OOXML prostor imena). Praćene izmjene su `ins`,
      `del`, `moveFrom`, `moveTo` i zapisi promjene oblikovanja (`rPrChange`, `pPrChange`, …).
      Limiti u `config`: `MaxDocxEntries` (10 000), `MaxDocxPartBytes` (256 MiB); dubina XML-a najviše
      1024 (konstanta u paketu, zaštita memorije). Pokrivenost 93,0 %; fuzz 60 s bez pada.
      - Fuzzer je našao dva pada (pomak unosa iznad MaxInt64; greške `encoding/xml` koje nisu sintaksne);
        popravljena su, unosi ostaju u `internal/convert/testdata/fuzz/` kao regresijski testovi.
      - Ograničenja: XML dio u kodiranju koje nije UTF-8 daje `ErrNotDocx` (namjerno, da gubitak ne prođe
        neopaženo); nema limita ukupnog rada preko svih dijelova, pa pozivatelj mora dati `ctx` s rokom.
      - `coder` nije učitao skillove `samber/cc-skills-golang` (nije naveo razlog); u sljedećim briefovima
        to izričito tražim.
- [x] 3. `internal/convert`: pretvorbe i golden testovi. `Converter` (`New(Options)`, `Runner` s radnim
      direktorijem): `DocxToMarkdown(ctx, src, dir) ([]Loss, error)`, `MarkdownToDocx(ctx, dir, reference, dst)`,
      `DocToDocx(ctx, src, dst)`, `MakeReference(ctx, src, dst)`; greške `ErrToolMissing`, `*ToolError`
      (stderr ograničen na 4 KiB). Pokrivenost paketa 91,5 %.
      - Čitanje: pipe tablice, pa grid ako prvi prolaz da `<table`; ako i grid ostavi `<table`, vraća se
        gubitak `tables` (dokument ide samo za čitanje). Pandoc 3.13 u grid tablicama podnosi i spojene
        ćelije, pa je to zaštita, ne očekivani slučaj. Mapa u kojoj `doc.md` već postoji se odbija.
      - Pisanje: čitač `markdown-smart` (sve vrste tablica); daje bajt-identičan `.docx` kao čitanje
        formatom kojim je Markdown zapisan. Odredište mora biti slobodno (ne postoji ili prazna datoteka).
      - `MakeReference`: uklanja slike na koje pokazuju samo slikovne veze tijela i briše te veze iz
        `document.xml.rels` (rez na razini bajtova, ostalo netaknuto); ostali unosi se kopiraju bez
        dekompresije. Slike zaglavlja, podnožja i drugih dijelova ostaju.
      - Golden: `testdata/docx/golden/*.md` za svih sedam dokumenata (`-update` ih regenerira); krug
        `.docx → .md → .docx → .md` za `basic`, `complex-table`, `header-image`. Ime slike je stabilno kroz
        tri generacije, drugo spremanje daje bajt-identičan `.docx`, zaglavlje i logo preživljavaju, nema
        visećih veza ni unosa u `[Content_Types].xml` bez dijela.
      - Novi probni dokument `header-image.docx` (slika u tijelu + logo u zaglavlju); skripta ima 31 provjeru.
      - Za `docs/known-losses.md`: nestaje `lang` iz metapodataka; numerirana lista postaje `1.  `;
        tablica se poravna; slika dobiva `{width=… height=…}`; širine stupaca grid tablice se mijenjaju;
        zaglavlje i podnožje nisu vidljivi u Markdownu (preživljavaju preko referentnog dokumenta);
        `.doc`: nema naslova, poveznice ni teksta fusnote, lista je citat s „•”, tablica zasebni odlomci.
      - `textutil` i od datoteke koja nije `.doc` (običan tekst) napravi `.docx` bez greške.
      - **Odstupanje od plana:** pandoc ne piše privremenu datoteku u korisnikov folder nego u Pisarovu
        radnu mapu; kopiranje u privremenu datoteku pored izvornika, `fsync` i `rename` radi `docsafe`
        (korak 4). Tako u korisnikovu mapu piše isključivo `docsafe`, kako traži `CLAUDE.md`.
- [x] 4. `internal/docsafe`: siguran zapis. `TakeSnapshot` / `Snapshot.Same` (veličina + SHA-256; mtime se
      ne uspoređuje), `CheckWritable` (`not-regular`, `hard-links`, `locked`, `no-permission`),
      `Ops.Replace(ctx, src, dst, expect) (ReplaceResult, error)`, greške `ErrConflict`, `ErrReadOnly`
      (`*ReadOnlyError` s razlogom). `Options` ima `BackupsDir`, `MaxBackups`, `Now`. Pokrivenost 83,2 %.
      - Redoslijed: provjera pisanja → konflikt → backup → privremena datoteka u istom folderu
        (`.<ime>.<slučajno>.tmp`, `O_EXCL`, 0600) → sadržaj → xattr-i → dozvole → `fsync` → zatvaranje →
        druga provjera konflikta → `rename` → `fsync` foldera.
      - Backup se radi kad god sadržaj izvornika nije ono što je Pisar sam zadnji zapisao (prvo
        prepisivanje u sesiji, ali i „Prepiši” nakon konflikta): `<backups>/<ime>-<hash putanje>/<UTC
        vrijeme>-<ime>`, 0600, provjeren hashom, rotacija na `MaxBackups`.
      - Greška ubačena u svakoj od 10 faza: izvornik bajt-identičan, iste dozvole i xattr-i, bez zaostalih
        datoteka (nakon `rename`-a je na mjestu novi, potpun sadržaj, a greška se svejedno prijavljuje
        zajedno s rezultatom — pozivatelj to mora tretirati kao „spremljeno, uz upozorenje”).
      - Izmjereno: `File.Sync` na macOS-u je `F_FULLFSYNC`. `com.apple.provenance` je jedini atribut koji
        se ne da prenijeti (kernel ga postavlja sam; jedina imenovana iznimka). `quarantine`,
        `FinderInfo`, `ResourceFork`, oznake, `macl` prenose se. Atribut koji sustav odbije (npr.
        `com.apple.rootless`) prekida spremanje, izvornik ostaje netaknut.
      - Poznata ograničenja (ne prenosi se): ACL, vlasnik i grupa, vrijeme stvaranja, inode (aliasi mogu
        puknuti), BSD zastavice osim zaključanosti. Između druge provjere konflikta i `rename`-a ostaje
        mali prozor koji bi zatvorilo samo zaključavanje.
      - `golang.org/x/sys` v0.48.0 je sada izravna ovisnost (odobreno planom); `go.sum` nepromijenjen.
- [x] 5. `internal/docsafe`: radne kopije i nova datoteka. `WorkMeta` (`Origin`, `Target`, `Snapshot`, `Dirty`,
      `Opened`), `WorkCopy`, `CreateWork`, `FindWork` (po `Origin` ili `Target`; prljava kopija ima
      prednost), `SaveWorkMeta` (atomski), `DiscardWork`, `SweepWork` (pri pokretanju briše čiste,
      vraća prljave), `CreateNew(ctx, src, dst)` (nova datoteka, nikad prepisivanje: privremena datoteka
      + `fsync` + tvrdi link na ciljno ime → `ErrExists` ako postoji). `Options.WorkDir`. Pokrivenost 85,1 %.
      - Mapa je `<work>/<16 hex znakova>`, slučajna (ne izvodi se iz putanje jer se dokument može
        preimenovati dok je otvoren). Brisati se smije samo prava mapa (ne link) koja je izravno dijete
        `work/` i ima takvo ime; sve drugo je `ErrNotWork`.
      - Mapa s oštećenim ili nedostajućim `meta.json` se nikad ne briše (može držati jedini primjerak
        teksta); samo se zapiše u log.
      - Ubacivanje greške: 6 faza za `meta.json` (stari ostaje čitljiv), 9 faza za `CreateNew` (ciljna
        datoteka ne postoji ili je potpuna; nema zaostalih datoteka).
      - Ograničenje: prljave radne kopije koje korisnik nikad ne oporavi ostaju u `work/` (nema čišćenja
        po starosti). Rezervni put bez tvrdih linkova testiran je samo zamjenom `os.Link`-a.
- **Korak 6 je podijeljen** (kao 6/6b u fazi 2), zbog veličine:
- [x] 6a. Logika Wordovih dokumenata bez Neovima (`worddocs.go`): `wordDocs` s `open`, `byMarkdown`, `byPath`,
      `markSaved`, `export`, `saveCopy`, `renamed`, `removed`, `sweep`, `close`; `wordView` (`Path`,
      `Markdown`, `ReadOnly`, `Imported`, `Dirty`). Ovisnosti kroz mala sučelja (`wordConverter`,
      `wordFiles`, `wordDialogs`). 56 testova s pravim pandocom i pravim `docsafe.Ops`.
      - Oporavak: prljava radna kopija → Oporavi / Odbaci izmjene / Odustani. Konflikt: Prepiši (backup
        tuđeg sadržaja) / Učitaj ponovno (korisnikov tekst ostaje kao `doc.prev.md`) / Odustani (zadano).
      - „Učitaj ponovno” stare `media/` i `reference.docx` prvo premješta u stranu i briše ih tek nakon
        uspjeha; ako ponovna pretvorba ne uspije, sve se vraća (odstupanje od briefa, na bolje).
      - Uvezeni `.doc`: prvo spremanje stvara `Ime.docx`; ako postoji, nativni dijalog za spremanje pita
        za ime. Wailsov dijalog na macOS-u nema naslov (ide kao poruka) i ignorira filtere; NSSavePanel
        sam pita za zamjenu, ali `CreateNew` nikad ne prepisuje pa se pita ponovno.
      - `doc.md` zapisan tijekom pretvorbe (veličina ili mtime različiti prije i poslije) → pretvorba se
        ponavlja, najviše 4 puta. Konstanta je zasad u `worddocs.go`; seli se u `config` u 6b.
      - Novi kodovi grešaka: `pandoc-missing`, `convert-failed`, `not-word`, `document-too-large`,
        `read-only`, `conflict`, `damaged-work` (Go + frontend + tekstovi u `strings.json`).
      - Nije provjereno: stvarni prikaz nativnih dijaloga (ide u ručnu provjeru).
- [ ] 6b. Uvezivanje u `EditorService`: bufferi samo za čitanje, preslikavanje radne kopije u izvornik,
      izvoz nakon `:w`, gašenje, preimenovanje i Smeće, dokument s naredbenog retka
- [ ] 7. Frontend: otvaranje iz stabla, stanja u alatnoj traci
- [ ] 8. `docs/known-losses.md`
- [ ] 9. Dimna provjera (`scripts/smoke.sh`)
- [ ] 10. Kraj faze: dokazi, popis **[R]**, `plan-reviewer`, sažetak

### Izvan opsega

Novi `.docx` iz aplikacije, uređivanje komentara i praćenih izmjena, pregled, tražilica, PDF izvoz,
postavke za prozu i pravopis, politika za Neovimovo swap pitanje, zaslon „nedostaje `pandoc`”,
pakiranje, automatsko pokretanje Worda.

### Kriteriji prihvaćanja

- [ ] otvaranje `.docx`-a kroz radnu kopiju; slike se izdvajaju i vraćaju
- [ ] spremanje prema toku 4.3, svaki korak pokriven testom
- [ ] golden round-trip prolazi; `docs/known-losses.md` napisan iz stvarnih rezultata
- [ ] `.docx` s komentarima ili praćenim izmjenama otvara se samo za čitanje, s razlogom
- [ ] „Spremi kao kopiju” stvara novu datoteku i ne dira izvornik
- [ ] `.doc` se uvozi i sprema kao novi `.docx`
- [ ] simulirani prekid usred zapisa ostavlja izvornik netaknut
- [ ] oporavak radne kopije nakon rušenja
- [ ] **[R]** spremljeni `.docx` otvara se u Wordu ili Pagesu i izgleda kao izvornik

## Faza 4 — Pregled i tražilica

### Kriteriji prihvaćanja

- [ ] pregled se pali i gasi (`␣p`, gumb), osvježava se nakon debouncea
- [ ] pregled je u sandboxed iframeu; skripta u dokumentu se ne izvršava (test)
- [ ] tražilica: `␣f` i `⌘P`, pretraga kroz sve projekte, `⌃p` sužava, `↵` otvara, `Esc` vraća fokus
- [ ] **[R]** izgled odgovara `docs/design/DESIGN.md`

## Faza 5 — Udobnost pisanja

### Kriteriji prihvaćanja

- [ ] `init.lua`: prelamanje po riječima, kretanje po prikazanim recima, bez brojeva redaka za prozu (prekidač)
- [ ] provjera pravopisa hr + en (provjeriti odakle dolazi hrvatski rječnik i može li se isporučiti s aplikacijom)
- [ ] prečaci za naslov, bold, italic, listu (umeću Markdown oznake)
- [ ] neobavezni `user.lua` učitava se nakon ugrađene konfiguracije

## Faza 6 — Pakiranje

### Kriteriji prihvaćanja

- [ ] `.app` se gradi i pokreće izvan razvojnog okruženja
- [ ] provjera `nvim` i `pandoc` pri pokretanju, s jasnom porukom
- [ ] ikona, naziv, verzija; potpisivanje za lokalnu upotrebu
