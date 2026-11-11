# PROGRESS

Plan: `docs/plans/2026-11-11-pisar-design.md` · Protokol: `PROMPT.md` · Izgled: `docs/design/DESIGN.md`

## Stanje

- Trenutna faza: 2 — `.md` i `.txt` (plan odobren 2026-11-11). Faza 1 je prihvaćena 2026-11-11: vlasnik je prošao
  ručni pogled i potvrdio da sve radi. Faza 0 je prihvaćena 2026-11-11:
  vlasnik je potvrdio svih sedam kriterija **[R]** i odlučio **idemo** (pravi Neovim u PTY-ju)
- Sljedeći korak: faza 2, korak 6 (otvaranje iz stabla i praćenje)
- Effort: vlasnik je 2026-11-11 odlučio da glavna sesija i `coder` rade na `high` u svim fazama; `PROMPT.md`,
  `.claude/agents/coder.md` i plan su usklađeni. (Faza 1 i korak 1 faze 2 još su išli s `coder`-om na `medium`.)
- Zadnja izmjena: 2026-11-11 (faza 2, korak 5 gotov)

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
- [ ] 6. Otvaranje i praćenje: `↵` / klik, poruka za `.docx`, aktivni dokument u stablu, stanje u traci
- [ ] 7. Meni, prečaci i fokus: Spremi, Kopiraj, Zalijepi, `␣e`, `Esc` u stablu
- [ ] 8. Gašenje: dijalog Spremi / Odbaci / Odustani
- [ ] 9. Dimne provjere prave aplikacije: `scripts/smoke.sh`, `scripts/shutdown.sh`
- [ ] 10. Kraj faze: `git rm -r spike`, kriteriji s dokazima, popis **[R]**, `plan-reviewer`, sažetak, STOP

### Izvan opsega

Konverzija `.docx` / `.doc`, „Samo za čitanje”, oporavak radne kopije, pregled, tražilica, postavke za
prozu i pravopis, `user.lua`, WebGL, zaslon „nedostaje `nvim`”, hrvatski nazivi u Wailsovim ulogama
menija, tamna tema, pakiranje, brisanje `~/.local/state/pisar-spike/` i `~/.local/share/pisar-spike/`.

### Kriteriji prihvaćanja

- [ ] klik ili `↵` u stablu otvara dokument u Neovimu
- [ ] aktivni dokument je označen u stablu i kad se buffer promijeni iz Neovima
- [ ] alatna traka prikazuje „Izmijenjeno” / „Spremljeno” prema stanju buffera
- [ ] gašenje s nespremljenim izmjenama traži potvrdu
- [ ] xterm.js tema i Neovim colorscheme generirani su iz tokena
- [ ] **[R]** fokus: tipke iz terminala ne aktiviraju stablo, i obratno

## Faza 3 — `.docx` i `.doc`

Effort high. Preduvjet: `pandoc` instaliran.

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
