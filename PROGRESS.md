# PROGRESS

Plan: `docs/plans/2026-11-11-pisar-design.md` · Protokol: `PROMPT.md` · Izgled: `docs/design/DESIGN.md`

## Stanje

- Trenutna faza: 9 — Log datoteka i provjera verzija alata (u tijeku od 2026-11-13). Faze 6 (Pakiranje), 7 (Teme) i 8 (Prečaci i which-key) prihvaćene su 2026-11-13: vlasnik je javio da je ručna provjera svih triju provjerena. Nakon vlasnikove provjere faze 8 ispravljeni su: „Uredi user.lua” bez pokrenutog Neovima, treperenje poruke u tražilici i prazno stanje koje je nudilo `␣f` (vidi „Nalazi vlasnikove ručne provjere” u fazi 8). Faza 5 je prihvaćena 2026-11-12: vlasnik je prošao ručnu
  provjeru i potvrdio da sve radi (predložena granica oko `user.lua` postavljena je uz plan faze 6 i
  prihvaćena odobrenjem tog plana; vidi „Odluke vlasnika” u fazi 6). Nakon faze 6 planiraju se teme (npr. Dracula, Catppuccin) kao nova faza. Faza 4 je prihvaćena 2026-11-12: vlasnik je
  prošao ručnu provjeru i potvrdio da sve radi. Faza 3 je prihvaćena 2026-11-12: vlasnik je
  prošao ručnu provjeru i potvrdio da sve radi. Faza 2 je prihvaćena 2026-11-11:
  vlasnik je prošao ručnu provjeru i potvrdio da sve radi. Faza 1 je prihvaćena 2026-11-11: vlasnik je prošao
  ručni pogled i potvrdio da sve radi. Faza 0 je prihvaćena 2026-11-11:
  vlasnik je potvrdio svih sedam kriterija **[R]** i odlučio **idemo** (pravi Neovim u PTY-ju)
- Sljedeći korak: faza 9, korak 5 (odbijanje prestarog alata)
- Effort: vlasnik je 2026-11-11 odlučio da glavna sesija i `coder` rade na `high` u svim fazama; `PROMPT.md`,
  `.claude/agents/coder.md` i plan su usklađeni. (Faza 1 i korak 1 faze 2 još su išli s `coder`-om na `medium`.)
- Zadnja izmjena: 2026-11-13 (faza 9 započeta)

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
- 2026-11-12, nakon mjerenja izravnog oblikovanja (pandoc 3.13, krug `.docx → .md → .docx`):
  - Graf (`c:chart`), SmartArt i ugrađeni objekti nestaju bez traga → dokument se otvara **samo za
    čitanje**, kao onaj s tekstualnim okvirom.
  - Izravno oblikovanje se gubi (poravnanje, boja, veličina i vrsta fonta, prored, uvlaka → citat,
    prijelom stranice, tabulator → razmak, polja → običan tekst, stilovi odlomka izvan osnovnih,
    sjenčanje i obrubi tablica) → **upozorenje u alatnoj traci**, dokument se i dalje sprema. Ostaju:
    isticanje, podcrtano, precrtano, eksponent, kapitalke, formule, fusnote. `docx+styles` je odbačen
    (svaki odlomak omota u `::: {custom-style="…"}`).
  - Putanju do mape s pravim dokumentima vlasnik još treba upisati.

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
- [x] 6b. Uvezivanje u `EditorService` (`editorwords.go`): `OpenDocument` prima `.docx`/`.doc`; `doc.md` se u
      svim događajima zamjenjuje korisnikovom datotekom; `SaveCopy`; događaj `document:export`
      (`converting` / `saved` / `failed` + kod / `canceled` / `reloaded`); `DocumentActive` ima
      `readOnly` (uvijek niz), `imported`, `unexported`. 22 nova testa s pravim Neovimom i pandocom.
      - Radnik: jedna goroutina s redom zadataka (izvoz, početni dokument, zatvaranje nakon `:q`),
        pokrenuta s uslugom, zaustavljena u `shutdown`. Izvozi istog dokumenta koji još nisu počeli se
        spajaju. `:wq` → izvoz se izvrši prije zatvaranja Wordovih dokumenata.
      - „Nespremljeno” za Wordov dokument = izmijenjen buffer ILI neizvezena radna kopija ILI izvoz u
        redu. Gašenje sa „Spremi” čeka kraj izvoza; neuspjeh → aplikacija ostaje otvorena s hrvatskom
        porukom; Odustani / Učitaj ponovno u konfliktu → ostaje otvorena bez dijaloga greške.
        `running()` je istinit i dok radnik ima posla nakon što se Neovim ugasio.
      - `internal/nvim` + `term`: `SetReadOnly` (`modifiable=false`, `readonly=true`; izmjena kroz RPC i
        tipkama ne uspijeva, `:w` pada) i `Reload` (`:edit!` i za pozadinski buffer). Između `OpenFile`
        i `SetReadOnly` buffer je kratko upisiv; izvoz takvog dokumenta Go ionako odbija.
      - Početni `.docx`/`.doc` s naredbenog retka otvara radnik nakon `Start`-a (ne unutar njega).
      - `Limits.ExportAttempts` (4) je u `config`. Usluga bez `Words` ponaša se kao prije faze 3.
      - Izmijenjeni postojeći testovi (zbog novih polja `DocumentActive`, koji više nije usporediv s
        `!=`): `TestEdSwitch`, `TestEdLocate`, `TestEdRemoved` (usporedba kroz `reflect.DeepEqual`),
        `TestEdEventJSON` (očekuje i nova polja). Testovi o odbijanju `.docx`-a ostali su netaknuti.
      - Poznata ograničenja: prozor od nekoliko ms između `:w` i obrade događaja (gašenje baš tada ne
        vidi dokument kao neizvezen); „Spremi kao kopiju” uzima `doc.md` s diska (bez teksta koji je
        utipkan, a nije zapisan); ako dijalog drži radnika dulje od `CommandTimeout` pri gašenju, radne
        kopije ostaju neočišćene; Neovim prikazuje ime `doc.md`, ne ime dokumenta.
- [x] 7. Frontend: `.docx` i `.doc` se otvaraju iz stabla (`↵`, klik); grana „unsupported” i tekst
      `openUnsupportedNotice` su uklonjeni. Alatna traka (`documentStatus` u `lib/activeDocument.ts`), po
      prednosti: „Konverzija u tijeku…” → „Izmijenjeno” → greška izvoza (boja `danger`, tekst po kodu) →
      „Izmjene još nisu u dokumentu” → „Samo za čitanje: <razlozi>” + gumb „Spremi kao kopiju” →
      „Spremljeno”; uz to napomena za uvoz iz `.doc`-a. Dugi tekst se skraćuje s tri točke, cijeli je u
      `title` i u DOM-u za čitač zaslona. Vitest: 512 testova.
      - Stanje izvoza se prati po putanji; završni događaj briše sve zapise `converting` (slučaj
        `.doc` → `.docx`); `saved` za aktivnu putanju briše i `unexported`.
      - Izmijenjeni postojeći testovi (ponašanje promijenjeno planom): u `activeDocument.test.ts` „opens
        only md and txt in this phase” (sada i docx, doc), „does not open docx and doc” (sada „opens docx
        and doc like md”), „the unsupported notice text exists” (obrisan s tekstom); u `treeKeys.test.ts`
        dva retka za `Enter` na Wordovom dokumentu sada očekuju `open`.
      - Ograničenje: dokument samo za čitanje s izmijenjenim bufferom ne može nastati (buffer je
        nepromjenjiv), pa gumb „Spremi kao kopiju” u praksi uvijek stoji. Izgled trake nije provjeren
        okom (ide u ručnu provjeru).
- [x] 8a. Dorada po vlasnikovim odlukama od 2026-11-12. `convert.Examine(ctx, path, lim) (Report, error)` u
      jednom prolazu vraća `Losses` (novi `embedded-objects`: `c:chart`, `dgm:relIds`, `w:object`, i u
      strogim prostorima imena) i `Simplified` (9 kodova: `alignment`, `text-color`, `font`, `spacing`,
      `indentation`, `page-breaks`, `tabs`, `fields`, `table-formatting`; samo `word/document.xml`, s
      praćenjem roditelja elementa). `Inspect` je omotač. `wordView.Simplified` i
      `DocumentActive.simplified` (uvijek niz); prazni se nakon uspješnog izvoza. Traka: napomena
      „Spremanje pojednostavljuje oblikovanje: …” za dokument koji nije samo za čitanje (napomena o
      uvozu iz `.doc`-a ima prednost). Probni dokumenti `chart.docx`, `formatting.docx` (iz Markdowna sa
      sirovim OpenXML blokom) + golden. Vitest 537.
      - Testovi `TestFormattingIsLostInSave` i `TestChartIsLostInSave` dokazuju gubitak nakon kruga
        (i da isticanje, podcrtano, precrtano, eksponent i kapitalke ostaju).
      - `basic`, `complex-table`, `header-image` ne okidaju nijedno pojednostavljenje (pravila nisu
        preširoka za pandocov izlaz). Polje `HYPERLINK` se ne broji kao polje (pandoc ga čuva kao vezu).
      - Ugrađeni objekt s pregledom: pandoc izvuče pregled kao sliku, OLE podaci nestaju; i dalje samo
        za čitanje. Noviji `cx:chart` nije pokriven. Za oporavljenu radnu kopiju popis je prazan (ne
        pamti se u `meta.json`).
      - Izmijenjeni postojeći testovi: u `inspect_test.go` tri slučaja ranog prekida dobila su četvrti
        gubitak (`<w:object/>`) jer „svi gubici” sada znači četiri; popisi probnih dokumenata u
        `inspect_test.go`, `golden_test.go`, `worddocs_test.go`; `TestEdSwitch`, `TestEdLocate`,
        `TestEdRemoved`, `TestEdEventJSON` i literali u frontend testovima dobili su novo polje `simplified`.
      - Nestabilan test: `coder` je jednom vidio pad `task test` u glavnom paketu i nije stigao utvrditi
        koji test. Glavna sesija je zatim pokrenula `go test -count=8 -race .` (uz opterećenje drugim
        provjerama): 8 od 8 prolazi, 187 s. Uzrok nije nađen; ostaje otvoren rizik.
- [x] 8b. `docs/known-losses.md` napisan iz golden rezultata i mjerenja. Mjerenje na pet vlasnikovih
      dokumenata iz `/Users/odinwolf/Documents/test` (kopije u scratchpadu; izvornici samo čitani, nisu u
      repozitoriju): tekst nakon kruga isti riječ po riječ; podnožje s brojem stranice, fusnote, margine
      sačuvani; izgubljeno izravno oblikovanje kako je izmjereno. U uzorku nije bilo komentara, praćenih
      izmjena, okvira, tablica, slika ni `.doc`-a.
      - **Nalaz:** stavka Wordovog sadržaja (`w:hyperlink` s poljem `PAGEREF \h` unutra) izlazi kao
        poveznica u poveznici; nakon spremanja u dokumentu piše „Uvod [3](#uvod)”. Izmjereno rješenje:
        Lua filter pri čitanju koji ugniježđenu poveznicu svodi na njezin tekst (`--lua-filter`, pandocov
        ugrađeni Lua, bez nove ovisnosti) → „Uvod 3” s vezom na naslov, krug stabilan. Ide u korak 8c.
      - **Nalaz:** crtani oblici bez teksta (`wps:wsp`, četiri linije u jednom dokumentu) nestaju → novi
        kod pojednostavljenja `drawings` u koraku 8c.
      - Uvučeni citati: u Markdownu su citat, a u spremljenom dokumentu bez stila „Block Text” postaju
        običan odlomak (i pri drugom otvaranju više nisu citat). Zapisano kao poznati gubitak.
- [x] 8c. Lua filter i crtani oblici. `internal/convert/filters/flatten-links.lua` (ugrađen s `go:embed`):
      `DocxToMarkdown` ga zapiše u radnu mapu, doda `--lua-filter=` u oba prolaza i ukloni ga na svakom
      izlazu (neuspjeh uklanjanja se vraća kao greška, ne guta se). Stavka sadržaja: bez filtra
      `[Uvod [3](#uvod)](#uvod)`, s filtrom `[Uvod 3](#uvod)`. Probni dokument `toc.docx` (složen tako
      da pandoc bez filtra stvarno da ugniježđenu poveznicu; test to dokumentira). U krugu `toc.docx`
      prvi i drugi Markdown razlikuju se samo klasom `.TOC-Heading` na naslovu sadržaja; drugi, treći i
      četvrti su isti. Novi kod pojednostavljenja `drawings` (`wps:wsp`, `wpg:wgp`); `formatting.docx`
      ima crtanu liniju. Skripta je tada ispisivala 65 redaka `ok` (nakon kruga 1 pregleda: 69); vitest 538.
      - Izmijenjeni postojeći testovi: `TestDocxToMarkdownCalls` (argumenti imaju `--lua-filter=`);
        `TestRoundTrip` (dodan `toc` s jednom dopuštenom razlikom prvog kruga); `TestExamineSimplified`,
        `TestExamineKeepsReadingAfterTheLosses`, `TestExamineFixtures` (deseti kod); u glavnom paketu
        popis kodova u `worddocs_test.go` i očekivani JSON u `editorwords_test.go` dobili su `drawings`
        (izvan popisa datoteka iz briefa, ali nužno jer se `formatting.docx` promijenio).
      - Nije potvrđeno na stvarnom XML-u u testu: prostori imena `wps`/`wpg` (uzeti iz vlasnikova
        dokumenta koji je mjerila glavna sesija; stroga varijanta nije dodana).
      - **Nestabilan test je nađen i ispravljen u kodu:** `TestWdProject` je padao u oko 5 od 30
        pokretanja jer je usluga javljala `saved` dok je zadatak izvoza još bio označen kao „u tijeku”,
        pa je `unsaved` nekoliko ms nakon `saved` vraćao istinu (preimenovanje odmah nakon spremanja
        bilo bi odbijeno). Ispravak glavne sesije (12 redaka u `editorwords.go`): zadatak se označi
        dovršenim (`settle`) prije javljanja ishoda. Test nije mijenjan; 40 od 40 pokretanja prolazi.
- [x] 9. Dimna provjera (`scripts/smoke.sh`): 63 provjere (19 starih + 44 nove), oko 10 s. `basic.docx` s
      naredbenog retka → `doc.md` u `work/`, izmjena kroz Neovimov socket + `:w` → valjan `.docx` s novim
      tekstom i istom slikom, jedan backup jednak izvorniku, bez drugih datoteka pored dokumenta, iste
      dozvole (0640) i xattr; drugo spremanje bez novog backupa. `comments.docx` → `modifiable=0`,
      `readonly=1`, `:w!` ne mijenja datoteku, nema backupa. `legacy.doc` → nastaje `legacy.docx`, `.doc`
      bajt-identičan. Nakon gašenja `work/` je prazan. Čekanje: uvjet u logu (`document:export …
      phase=saved`) + promjena datoteke, rok 30 s; bez `sleep`-a. Bez `pandoc`-a provjere se preskaču.
      - Nalaz: `doc.md` završava definicijama fusnota; redak dopisan odmah iza zadnje (bez praznog
        retka) pandoc čita kao nastavak fusnote. Svojstvo Markdowna; ide u `docs/known-losses.md`.
- [x] 10. Kraj faze: dokazi po kriterijima, popis **[R]**, `plan-reviewer` (krug 1 FAIL, krug 2 FAIL samo
      zbog zapisa, krug 3 **PASS**), sažetak vlasniku

### Pregled plana — krug 1 (2026-11-12): FAIL, bez blokera; ispravljeno

`plan-reviewer` je sam pokrenuo sve provjere (sve prolaze) i našao pet stavki za ispravak:

1. Tekst koji „Učitaj ponovno” ostavlja kao `doc.prev.md` brisao se pri zatvaranju dokumenta (radna
   kopija nakon učitavanja nije `Dirty`), a dijalog je tvrdio da ostaje. **Ispravak u kodu:** novi
   `docsafe.BackupText` sprema korisnikov Markdown u mapu sigurnosnih kopija dokumenta
   (`<vrijeme>-<ime>.md`, vlastita rotacija, odvojena od `.docx` kopija) PRIJE učitavanja; ako to ne
   uspije, učitavanje se ne radi. Tekst dijaloga sada to i kaže. `doc.prev.md` ostaje u radnoj mapi
   do zatvaranja, kako piše u planu. Namjerno izmijenjene tvrdnje: `TestWdConflict` i podtest „reload”
   u `worddocs_test.go` su tvrdili da „Učitaj ponovno” ne radi nikakvu kopiju; sada traže točno jednu
   `.md` kopiju s korisnikovim tekstom.
2. Dijalog pri gašenju imao je hrvatski tekst samo za četiri koda Wordovih grešaka. Dodani su
   `damaged-work`, `document-too-large`, `not-word`. Greške Neovima i `internal` i dalje pokazuju
   tekst uzroka (kao u fazi 2); tvrdnja „s hrvatskom porukom” u koraku 6b vrijedi za kodove Wordovih
   grešaka, ne za sve.
3. `docs/known-losses.md` je za ugrađeni objekt tvrdio da nestaje; ispravljeno (slika pregleda
   ostaje, podaci nestaju), a za SmartArt i OLE piše da su mjereni na ručno složenom XML-u, bez
   probnog dokumenta.
4. Krug za uvezeni `.doc`: `legacy` je dodan u `TestRoundTrip` (Markdown stabilan).
5. Datoteke izvan tablice plana nisu bile zapisane kao odstupanje: zapisane su niže.

Uz to, iz napomena pregleda: `TestFormattingIsLostInSave` sada dokazuje i crtani oblik, indeks i
formulu (ostaju) te stilove `Title` i `Quote` (postaju `BodyText`); podtest u `TestRealToolFailures`
čije je ime tvrdilo da pokreće `textutil` preimenovan je u ono što stvarno provjerava; ispravljen je
broj provjera skripte i redak o BSD zastavicama.

### Pregled plana — krug 2 (2026-11-12): kod u redu, FAIL samo zbog zapisa; ispravljeno

Svi nalazi kruga 1 potvrđeni kao riješeni ili zabilježeni. Novo: brojevi u „Dokazi po kriterijima”
bili su od prije ispravaka (ispravljeni su prema svježem mjerenju); ograničenje „Učitaj ponovno” za
neupisani tekst i točka u ručnoj provjeri (dodani); uvod u `docs/known-losses.md` (ublažen); pogrešan
komentar u podtestu „reload” (ispravljen, tvrdnje iste).

### Pregled plana — krug 3 (2026-11-12): PASS

Reviewer je sam izmjerio brojeve iz „Dokazi po kriterijima” (isti su) i potvrdio N1–N4 iz kruga 2.
Otvoreno ostaje samo ono što je u „Otvoreno nakon faze 3” i kriterij **[R]**.

### Odstupanja od plana faze (datoteke i ponašanje)

Uz odstupanja zapisana uz pojedine korake:

- **Datoteke izvan tablice „Datoteke” u planu:** novi `worddocs.go`, `editorwords.go` (i testovi) u
  glavnom paketu (plan je naveo samo „uredi `editorservice.go`”; logika je izdvojena da datoteka
  usluge ne naraste i da se dio bez Neovima može testirati sam); `internal/term/session.go` (+ test)
  za `SetReadOnly` i `Reload`; generirani `frontend/bindings/**`; `internal/convert/filters/`.
- **`internal/nvim`:** plan je dopustio izmjene „samo za buffer samo-za-čitanje”. Dodan je i `Reload`
  (treba ga „Učitaj ponovno”), a traženje buffera po putanji izdvojeno je iz `Wipe` u zajednički
  `findLua` da ga koriste sve tri metode (mala izmjena postojećeg koda, bez promjene ponašanja).
- **Planirane, a nedirane datoteke:** `menu.go` (nema nove stavke menija; „Spremi kao kopiju” je gumb u
  traci), `Sidebar.svelte` (otvaranje je u kontrolerima), `nvim/init.lua` (stanje samo-za-čitanje
  postavlja Go kroz RPC).
- **Krug `.docx → .md → .docx → .md`** nije na „svim probnim dokumentima”, kako piše u koraku 3 plana,
  nego na onima koji se smiju spremati; dokumenti samo za čitanje (`comments`, `tracked-changes`,
  `textbox`, `chart`) nikad se ne spremaju, pa za njih postoji samo golden čitanja.
- **Konstante izvan `internal/config`:** tehničke granice u paketima (`maxXMLDepth`, `maxToolOutput`,
  `killGrace`, `maxFieldDepth`, `maxInstrBytes`, `maxLabelBytes`, `copyBufSize`, `maxWorkMetaBytes`,
  `xattrRetries`, deset pokušaja slučajnog imena) nisu u `config`-u; tamo su granice koje imaju
  smisla kao postavke (rokovi, broj backupa, veličine arhive, broj pokušaja izvoza). Isti je presedan
  u ranijim fazama (`maxNameBytes`). `CLAUDE.md` kaže „nema hardkodiranih limita”, pa je ovo za
  vlasnikovu odluku.
- **`CreateNew` bez tvrdih linkova:** na volumenu koji ih ne podržava ime se zauzima provjerom pa
  preimenovanjem; datoteka nastala između ta dva koraka bila bi prepisana (mali prozor, samo na takvom
  volumenu; isto vrijedi za `Rename` iz faze 1).
- **Preimenovanje `.docx` → `.doc`:** `docsafe.Rename` odbija promjenu vrste dokumenta, pa otvoreni
  `.docx` ne može dobiti nastavak `.doc` kroz Pisar; `wordDocs.renamed` zato nema vlastitu zaštitu.

### Dokazi po kriterijima (2026-11-12)

Naredbe iz korijena repozitorija, pokrenute nakon zadnje izmjene koda (nakon ispravaka iz kruga 1 pregleda).

- `go test -count=1 -race ./...` → svi paketi `ok`; `task test` → exit 0 (vitest `Tests 538 passed`);
  `task lint` → `0 issues.`, svelte-check `0 ERRORS 0 WARNINGS`; `task build` → exit 0;
  `bash scripts/make-fixtures.sh` → `all checks passed` (69 redaka `ok`, 61 poziv `check`; izlaz
  bajt-stabilan);
  `bash scripts/smoke.sh` → `smoke: all checks passed` (63 provjere); `bash scripts/shutdown.sh` →
  `shutdown: all checks passed`.
- Go testovi (`go test -list`): `Pisar` 127, `config` 28, `convert` 55, `docsafe` 92, `nvim` 55,
  `project` 36, `term` 34; preskočeni samo `TestRealTrash` i `TestScanNFCandNFD` (kao u fazi 2).
  Pokrivenost (`go test -count=1 -cover ./...`): `Pisar` 82,0 %, `config` 84,4 %, `convert` 92,8 %,
  `docsafe` 85,2 %, `nvim` 87,1 %, `project` 94,5 %, `term` 93,5 % (`nvim` se mijenja između
  pokretanja, kao u fazi 2).

1. Otvaranje kroz radnu kopiju, slike se izdvajaju i vraćaju: `TestWordDocsOpenBasic` (`doc.md`,
   `media/`, `reference.docx`, `meta.json`; izvornik bajt-identičan), `TestWdOpen` (pravi Neovim:
   buffer je `doc.md`, događaj nosi putanju `.docx`-a), `TestRoundTrip` u `internal/convert` (broj i
   bajtovi slike nakon kruga; `header-image`: logo u zaglavlju bajt-identičan, nema visećih veza),
   `smoke.sh` (slika u spremljenom `.docx`-u jednaka izvornoj).
2. Spremanje prema toku 4.3, svaki korak testom: konflikt `TestReplace_ConflictBeforeStart`,
   `TestReplace_ConflictDuringWork`, `TestWordDocsExportConflict`, `TestWdConflict`; backup
   `TestBackup_*` (12 testova); privremena datoteka `TestReplace_TempFile`; provjera izlaza pandoca
   u `convert` (`MarkdownToDocx` odbija prazan ili nevaljan izlaz); dozvole i xattr-i
   `TestReplace_Success`, `TestCopyXattrs`; redoslijed `TestReplace_StagesAreVisitedInOrder`; greška u
   svakoj fazi `TestReplace_FaultAtEveryStage`; cijeli tok s pravim alatima `TestWordDocsExport`,
   `TestWdSave`, `smoke.sh`.
3. Golden round-trip i `docs/known-losses.md`: `go test ./internal/convert/...` (golden za 10 probnih
   dokumenata, krug za `basic`, `complex-table`, `header-image`, `formatting`, `toc`);
   `TestFormattingIsLostInSave` i `TestChartIsLostInSave` dokazuju stavke koje imaju probni dokument
   (ostale su mjerene ručno i tako označene u dokumentu);
   `docs/known-losses.md` uz to nosi mjerenje na pet vlasnikovih dokumenata. Krug je i za uvezeni `legacy`.
4. Komentari i praćene izmjene samo za čitanje, s razlogom: `TestInspectFixtures`,
   `TestWordDocsOpenReadOnlyReasons`, `TestWdReadOnly` (buffer se ne da mijenjati, `Save` →
   `read-only`, `:w!` ne mijenja datoteku), `smoke.sh` (slučaj B); razlog u traci `documentStatus`
   (vitest).
5. „Spremi kao kopiju”: `TestWordDocsSaveCopy`, `TestWdSaveCopy`, `TestCreateNew_ExistingDestination`
   (postojeće ime se ne prepisuje, izvornik bajt-identičan).
6. `.doc` se uvozi i sprema kao novi `.docx`: `TestWordDocsOpenLegacyDoc`, `TestWordDocsImportedDoc`,
   `TestWdLegacy`, `smoke.sh` (slučaj C); neuspjeh `textutil`-a uz izlazni kod 0 prepoznaje se po
   izlaznoj datoteci (testovi `DocToDocx` s lažnim `Runner`-om).
7. Prekid usred zapisa ostavlja izvornik netaknut: `TestReplace_FaultAtEveryStage` (10 faza: bajtovi,
   dozvole i xattr-i isti, bez zaostalih datoteka), `TestCreateNew_FaultAtEveryStage`,
   `TestBackup_FailureLeavesTheDocumentAlone`, `TestReplace_Cancellation`.
8. Oporavak radne kopije nakon rušenja: `TestWordDocsRecovery` (Oporavi / Odbaci / Odustani, oštećena
   kopija), `TestFindWork_PrefersUnsavedThenNewest`, `TestSweepWork`, `TestWdShutdownQueued`
   (izvoz prekinut gašenjem ostavlja kopiju za oporavak).

### Ručna provjera [R] — za vlasnika (agent ovo ne može provjeriti)

Pripremi kopije dokumenata; izvornike ne diraj. Backup je u `~/Library/Application Support/Pisar/backups/`.

1. Otvori pravi `.docx` iz stabla (`↵` ili klik): traka kratko kaže „Konverzija u tijeku…”, zatim se
   vidi tekst; uz dokument s izravnim oblikovanjem stoji napomena „Spremanje pojednostavljuje
   oblikovanje: …”.
2. Izmijeni tekst, `⌘S`: traka prođe kroz „Konverzija u tijeku…” do „Spremljeno”. **Otvori spremljeni
   dokument u Wordu (i Pagesu):** otvara li se bez poruke o oštećenju ili popravku; je li tekst isti;
   jesu li zaglavlje, podnožje s brojem stranice i fusnote na mjestu; izgleda li sadržaj (TOC) kao
   „Uvod 3”. Oblikovanje će biti pojednostavljeno kako piše u `docs/known-losses.md`; presudi je li to
   prihvatljivo. Ovo je kriterij **[R]** faze.
3. U mapi backupa postoji kopija izvornika prije prvog spremanja.
4. Dokument s komentarima ili praćenim izmjenama (u testnoj mapi ga nije bilo): otvara se „Samo za
   čitanje: …”; tipkanje ne mijenja tekst; „Spremi kao kopiju” otvara dijalog, kopija se otvori i
   može se uređivati; izvornik je netaknut.
5. `.doc` (u testnoj mapi ga nije bilo): napomena o uvozu; `⌘S` stvara `Ime.docx` pored njega; ako
   `Ime.docx` već postoji, dijalog pita za ime; `.doc` ostaje isti.
6. Konflikt: dokument otvoren u Pisaru spremi iz Worda, pa `⌘S` u Pisaru → dijalog s tri gumba.
   Isprobaj „Odustani” (ništa se ne mijenja, traka kaže „Izmjene još nisu u dokumentu”), „Prepiši”
   (u backupu je Wordova verzija) i „Učitaj ponovno” (u Neovimu je Wordova verzija, a u mapi backupa
   tog dokumenta je `…-ime.docx.md` s tekstom iz Pisara).
7. Oporavak: nakon „Odustani” u konfliktu zatvori Pisar s „Odbaci”, pokreni ga i otvori isti
   dokument → pitanje Oporavi / Odbaci izmjene / Odustani.
8. Gašenje s nespremljenim Wordovim dokumentom: dijalog navodi ime `.docx`-a (ne `doc.md`); „Spremi”
   zatvara aplikaciju tek kad je datoteka zapisana.
9. Izgled trake na najmanjoj širini prozora (1000 px): dugi razlozi i napomena se skraćuju s tri
   točke, cijeli tekst je u oblačiću; greška je crvena i podebljana.
10. `Esc` u nativnim dijalozima znači „Odustani” (otvoreno i iz faze 2).

### Otvoreno nakon faze 3

- Wordovo oblikovanje se pri spremanju pojednostavljuje (odjeljak 2 u `docs/known-losses.md`). To je
  posljedica Markdowna kao radnog formata, ne greška; kriterij **[R]** „izgleda kao izvornik” vlasnik
  ocjenjuje uz to ograničenje.
- U uzorku stvarnih dokumenata nije bilo komentara, praćenih izmjena, tekstualnih okvira, tablica,
  slika ni `.doc`-a; za njih postoje samo mjerenja na generiranim dokumentima.
- Mjerenje na stvarnim dokumentima rađeno je pandocom s naredbenog retka, s istim argumentima koje
  koristi aplikacija, ne kroz samu aplikaciju.
- Neovim prikazuje ime `doc.md`, ne ime dokumenta. Prljave radne kopije koje nitko ne oporavi ostaju u
  `work/`. `SaveCopy` ne uzima tekst koji je utipkan, a nije zapisan. Noviji `cx:chart` nije pokriven.
- Nativni dijalozi (oporavak, konflikt, spremanje) provjereni su samo prevođenjem i lažnim dijalozima.
- „Učitaj ponovno” čuva ono što je zapisano u `doc.md`; tekst utipkan nakon zadnjeg `:w` (npr. dok je
  pretvorba trajala) odbacuje se s bufferom. Gdje je Markdown kopija, korisnik vidi samo u mapi
  backupa (aplikacija to ne prikazuje).
- Komentar uz `BackupsDir` i `WorkDir` u `docsafe.Options` kaže da ne smiju biti u korisnikovim
  dokumentima, ali kod to ne provjerava; obje putanje dolaze iz `internal/config`.

### Izvan opsega

Novi `.docx` iz aplikacije, uređivanje komentara i praćenih izmjena, pregled, tražilica, PDF izvoz,
postavke za prozu i pravopis, politika za Neovimovo swap pitanje, zaslon „nedostaje `pandoc`”,
pakiranje, automatsko pokretanje Worda.

### Kriteriji prihvaćanja

- [x] otvaranje `.docx`-a kroz radnu kopiju; slike se izdvajaju i vraćaju — dokaz 1
- [x] spremanje prema toku 4.3, svaki korak pokriven testom — dokaz 2
- [x] golden round-trip prolazi; `docs/known-losses.md` napisan iz stvarnih rezultata — dokaz 3
- [x] `.docx` s komentarima ili praćenim izmjenama otvara se samo za čitanje, s razlogom — dokaz 4
- [x] „Spremi kao kopiju” stvara novu datoteku i ne dira izvornik — dokaz 5
- [x] `.doc` se uvozi i sprema kao novi `.docx` — dokaz 6
- [x] simulirani prekid usred zapisa ostavlja izvornik netaknut — dokaz 7
- [x] oporavak radne kopije nakon rušenja — dokaz 8
- [x] **[R]** spremljeni `.docx` otvara se u Wordu ili Pagesu i izgleda kao izvornik — potvrdio vlasnik ručno
      2026-11-12 („testirano i radi sve”)

## Faza 4 — Pregled i tražilica

Effort high. Plan faze odobren 2026-11-12.

### Odluke vlasnika (2026-11-12)

- Okvir pregleda ima `sandbox="allow-same-origin"` i nema `allow-scripts`; aplikacija mijenja sadržaj
  okvira na mjestu (pregled ne skače na vrh pri osvježavanju).
- Font pregleda: Source Serif 4 (četiri woff2 datoteke iz službenog Adobeovog izdanja + OFL).
- Pregled je samo za Markdown i Wordove dokumente; za `.txt` ploča to kaže.

### Ovisnosti (odobrene)

- Nema novih Go ni npm ovisnosti. Novi resurs: font Source Serif 4. Nove pandocove zastavice:
  `--embed-resources`, `--math-method=mathml`, `--syntax-highlighting=none`.

### Izmjereno prije plana (pandoc 3.13)

- Čitač `markdown-smart-raw_html-raw_tex-raw_attribute`: `<script>` i `<img onerror=…>` iz Markdowna
  izlaze kao doslovan tekst.
- Lua filter zamjenjuje sliku s adresom (`http:`, `//`, apsolutna putanja, `..`, `data:`) njezinim
  opisom, a poveznicu pretvara u `span`; `--embed-resources` radi i bez `--standalone` (lokalna slika
  postaje `data:` URI). `--sandbox` onemogući i lokalne slike, pa se ne koristi.
- `--no-highlight` i `--mathml` su zastarjeli. Jedno osvježavanje malog dokumenta: oko 125 ms.

### Ugovor Go ↔ frontend

```go
EditorService.SetPreview(enabled bool) error
event "preview:content"  PreviewContent{ Path, State ("html" | "unsupported" | "error"), HTML, Code }
event "ui:toggle-preview" ""   // ␣p
ProjectService.SearchDocuments(query, projectID string) ([]SearchHit, error)   // "" = svi projekti
SearchHit{ ProjectID, ProjectName, Rel, Name, Kind string; Matches [][2]int }  // rasponi u Name, UTF-16
event "ui:open-search" ""      // ␣f
event "menu:search" ""         // ⌘P
```

### Koraci

- [x] 1. `internal/config` + `internal/preview`. `config`: `PreviewDebounce` (300 ms), `PreviewTimeout` (10 s),
      `MaxPreviewSource` (8 MiB), `MaxPreviewHTML` (64 MiB), `MaxSearchResults` (50), `PreviewDir()`.
      `preview.Renderer.Render(ctx, markdown, resourceDir)`: pandoc čita Markdown sa standardnog ulaza i
      vraća tijelo stranice; radni direktorij je Pisarova mapa; zadani runner prekida proces kad izlaz
      prijeđe granicu; greške `ErrToolMissing`, `ErrTooLarge`, `*ToolError`. Pokrivenost 92,2 %; fuzz
      20 s bez nalaza.
      - **Odstupanje od plana:** čitač je `markdown-smart-raw_tex-raw_attribute`, dakle sirovi HTML je
        UKLJUČEN u čitaču, a cijeloga ga uklanja filter. Razlog: s isključenim sirovim HTML-om komentar
        `<!-- -->`, koji pandoc sam upisuje između susjednih lista u radnim kopijama Wordovih
        dokumenata, vidio bi se u pregledu kao tekst. Svi neprijateljski ulazi ponovljeni su s novim
        čitačem i nijedan ne prolazi (`<script>`, `<iframe>`, `<svg onload>`, `<img onerror>`,
        `<a href="javascript:">`, `<meta refresh>`, `<form>`… nestaju; `<span onclick>` i `<div>`
        zadržavaju tekst bez atributa).
      - Filter (`internal/preview/filters/preview.lua`) uz polazni radi i ovo, sve nađeno mjerenjem:
        provjera putanje i na percent-dekodiranom obliku (`%2e%2e/x.png` i `%2fetc%2fhosts` su prije
        toga bili ugrađeni — stvarni bijeg iz foldera); dopušteni nastavci slike (`.mp4`, `.mp3`,
        `.pdf` su davali `<video>`, `<audio>`, `<embed>`); briše se svaki atribut osim `class` (i
        brojčanih `width`/`height` na slici) na svim elementima koji ih nose; slika koja nije
        čitljiva u folderu dokumenta zamjenjuje se opisom (pandoc je traži i u radnom direktoriju);
        `- [ ]` ne postaje `<input>`.
      - Neprijateljski SVG kao lokalna slika ugrađuje se kao `<img src="data:image/svg+xml…">`, ne kao
        umetnuti `<svg>`; test pada ako se to promijeni.
      - Poznata ograničenja: simbolička veza u folderu dokumenta koja pokazuje izvan njega se ugrađuje
        (pandoc je slijedi; iz Markdowna se ne vidi); folder s `:` u imenu dobiva pregled bez slika
        (pandoc dijeli `--resource-path` po `:`); slike su u pregledu `data:` URI. (Pandocova sidra
        fusnota, koja su ovdje prvo ostala, uklonjena su nakon kruga 1 pregleda — vidi niže.)
- [x] 2. `internal/project`: `Search(sources []SearchSource, query, limit) []Hit`. Traži samo dokumente, po imenu
      bez ekstenzije; svaka riječ upita mora biti podniz imena; bez razlike velikih i malih slova i
      bez dijakritika (kombinirajući znakovi U+0300–036F, `đ` → `d`, Latin-1 i Latin Extended-A po
      tablici); `Matches` su rasponi u izvornom imenu u UTF-16 jedinicama i ne zalaze u ekstenziju.
      Poredak: ime počinje upitom → riječ imena počinje upitom → ostalo; zatim kraće, abecedno,
      projekt, `Rel`. Pokrivenost paketa 96,0 %; fuzz 20 s (2,7 milijuna izvršavanja) bez nalaza.
      - Izmjereno: 100 000 čvorova u 5 projekata, upit od dvije riječi oko 11 ms; najgori slučaj
        (upit `a`, svih 100 000 pogodaka) 27,6 ms.
      - Odstupanje od briefa: tip izvora se zove `SearchSource`, jer `Source` u paketu već postoji
        (izvor promjena u `watch.go`).
      - Ograničenja: `dj` ne nalazi `đ` i obrnuto; predsloženi vijetnamski znakovi, grčki i ćirilica se
        samo pretvaraju u mala slova.
- [x] 3. Neovim. `init.lua`: `<leader>p` → `pisar_preview`, `<leader>f` → `pisar_search` (samo normal mod);
      `pisar_changed` bez sadržaja, samo dok je `g:pisar_preview` istinit, na `TextChanged`,
      `TextChangedI`, `TextChangedP`, pri prelasku na drugi buffer i na `BufReadPost` trenutačnog
      buffera. `internal/nvim` + `term`: `EventChanged`, `EventTogglePreview`, `EventOpenSearch`,
      `SetPreview(ctx, on)` (pri uključivanju Go sam daje jedan `EventChanged`), `BufferText(ctx,
      maxBytes)` (tekst i opis buffera iz istog Lua bloka; `ErrBufferTooLarge`). `Emitter` ima
      `Changed`, `TogglePreview`, `OpenSearch` (u glavnom paketu zasad samo `slog.Debug`).
      - Izmjereno (Neovim 0.12): `:edit!` i `Reload` ne okidaju `TextChanged*`, zato `BufReadPost`;
        `TextChangedP` je potreban dok je otvoren popup dovršavanja; u insert modu jedna obavijest po
        znaku pri sporom tipkanju, jedna za cijeli zalijepljeni tekst.
      - `BufferText` veličinu provjerava u Neovimu prije čitanja (`nvim_buf_get_offset`): preveliki
        tekst se ne prenosi (odbijanje 93 µs; prijenos 8 MiB 9,6 ms). Završni novi redak je tu točno
        kad bi ga `:w` zapisao. Poziv ne mijenja kursor, mod, registre ni poruke.
      - **Ispravak greške iz faze 3 (nađena u ovom koraku):** `Reload` pozadinskog buffera slao je
        lažni `pisar_entered` (unutar `nvim_buf_call` taj je buffer na trenutak „trenutačan”), pa bi
        nakon „Učitaj ponovno” za dokument koji nije na ekranu frontend dobio `document:active` za
        krivi dokument, a sljedeći pravi prelazak mogao je ostati neprijavljen. `Reload` sada oko
        poziva postavlja `g:pisar_reloading`, a `entered()` ga poštuje; zastavica se vraća i kad
        `:edit!` ne uspije. Četiri nova testa (`reload_test.go`); mutacije ih ruše.
      - Događaji iz Neovima idu kroz kanal od 256 mjesta s odbacivanjem najstarijih (postojeće
        ponašanje); petlja pregleda zato mora stalno čitati i sama tražiti tekst.
- [x] 4. Usluge. `editorpreview.go`: `EditorService.SetPreview`, događaji `preview:content` (`PreviewContent{Path,
      State, HTML, Code}`), `ui:toggle-preview`, `ui:open-search`; petlja pregleda je jedna goroutina
      (pokrenuta s uslugom, zaustavljena u `shutdown`): čeka `PreviewDebounce` tišine, uzima tekst
      buffera, odlučuje po bufferu (`.md` → folder datoteke; `doc.md` Wordovog dokumenta → radna mapa,
      `Path` je korisnikova datoteka; `.txt`, buffer bez datoteke i ostalo → `unsupported`), pretvara i
      šalje. Nova izmjena otkazuje pretvorbu u tijeku, a njezin rezultat se ne šalje. Isti sadržaj se
      ne šalje dvaput, osim nakon `SetPreview(true)`. `ProjectService.SearchDocuments(query,
      projectID)` s predmemorijom stabala; stavka menija „Traži dokument…” (`⌘P` → `menu:search`);
      novi kod `preview-too-large`. Vitest 540.
      - Vrijeme u testovima petlje je ubrizgani tajmer (bez `time.Sleep`); testovi s pravim Neovimom
        koriste debounce od 20 ms i provjeravaju konačno stanje. Novi testovi prolaze 10 puta zaredom
        s `-race` (i kod glavne sesije).
      - Predmemorija stabla vrijedi samo dok je projekt promatran i briše se na svaku obavijest o
        promjeni projekta, na dodavanje i uklanjanje projekta te pri svakoj promjeni promatranja;
        skeniranje istog projekta ne teče dvaput istodobno. Slijepa točka: izmjena izvana se ne vidi
        dok je promatrač ne javi (FSEvents + `WatchDebounce`).
      - Pregled preživljava gašenje i ponovno pokretanje Neovima (`Start` primjenjuje zapamćeno stanje).
        Usluga bez `Preview` u ovisnostima ponaša se kao da pregleda nema.
      - Prevelik `.txt` buffer daje `error` + `preview-too-large` s praznom putanjom (putanja se ne zna
        kad Neovim odbije tekst), a ne `unsupported`.
      - Izmijenjeni postojeći testovi: `TestEdCodes` i `TestCodeOf` dobili su retke za nove kodove;
        lažni izvor promjena `fakeSource` u `projectservice_test.go` je interno prepisan (zatvaranje
        kanalom, novi `die()` i `startErr`), ponašanje za stare testove isto.
      - Za frontend: `SetPreview(true)` uvijek daje jedan `preview:content`; nakon `SetPreview(false)`
        može stići jedan zakašnjeli događaj (ignorirati kad je ploča zatvorena); nakon ponovnog
        učitavanja stranice treba pozvati `SetPreview` s trenutačnim stanjem; na `nvim:exit` prikaz
        briše frontend.
- [x] 5. Frontend — pregled. `Preview.svelte` s okvirom `sandbox="allow-same-origin"` (jedina dozvola);
      `lib/previewFrame.ts` gradi dokument ljuske koji ide u `srcdoc` jednom: CSP `default-src 'none';
      img-src data:; style-src 'unsafe-inline'; font-src <podrijetlo>; form-action 'none'; base-uri
      'none'`, stil iz tokena, `@font-face` za četiri lica; `lib/previewWrite.ts` je jedino mjesto s
      `innerHTML` i piše u spremnik `#page` u dokumentu okvira; `lib/previewState.ts` (prekidač,
      ignoriranje sadržaja kad je ploča zatvorena, `nvim:exit`, gašenje ispod 1100 px bez
      samopaljenja); gumb „Pregled” (`aria-pressed`, ispunjen akcentom; onemogućen ispod 1100 px);
      raspored `Sidebar | EditorPane | Preview`. Font Source Serif 4 (4.005R, četiri woff2 + OFL) u
      `assets/fonts`. Vitest 620.
      - Pomak ostaje jer se okvir nikad ne učitava ponovno; dok se nove slike dekodiraju, spremnik
        drži staru visinu (najviše 1 s). Isti HTML se ne piše dvaput. Pozivi `SetPreview` idu serijski.
      - Podrijetlo za `font-src` je `protocol//host` (ne `location.origin`, koji je za prilagođenu
        shemu u nekim motorima „null”); sve što nije točno `scheme://host[:port]` se odbija, a tada
        okvir dobiva `font-src 'none'` i rezervni font.
      - Zaštitni test `previewGuard.test.ts` pada na `{@html`, na `innerHTML` / `outerHTML` / `DOMParser`
        / `document.write` izvan `previewWrite.ts` i na svaku `sandbox` dozvolu osim `allow-same-origin`.
      - Klik u pregled vraća fokus terminalu (pravilo „terminal dobiva tipke”); tekst se može označiti
        mišem, ali `⌘C` iz pregleda ne radi (meni „Kopiraj” poznaje samo terminal i polja za unos).
      - Odluke `coder`-a izvan briefa: zatvaranje ploče briše sadržaj; ploča se montira samo dok je
        otvorena (pri otvaranju pomak kreće od vrha); oko stranice je 16 px podloge, pa je stranica u
        ploči od 400 px široka oko 368 px.
      - Prošireni postojeći testovi (samo dodavanje): `cssVars.test.ts` (broj varijabli se računa iz
        broja obitelji fonta; nove varijable pregleda), `fonts.test.ts` (lica i datoteke Source Serif 4).
      - Nije provjereno okom: učitava li okvir font uz ovaj CSP u Wails prozoru, izgled stranice,
        ponašanje pri osvježavanju, fokus.
- [x] 6. Frontend — tražilica. `SearchPanel.svelte` na dnu stupca editora, iznad zadnja dva retka terminala
      (statusna traka i naredbeni redak; pomak javlja `Terminal.svelte` nakon svakog `fit()`);
      `lib/searchState.ts` (čisto stanje, prijelazi vraćaju stanje + učinke), `searchKeys.ts` (`↓` `↑`
      `↵` `Esc` `⌃P`; ništa s `⌘`), `searchHighlight.ts` (ime bez ekstenzije kao tekstualni segmenti,
      otporno na neispravne raspone), `searchController.svelte.ts` (događaji `ui:open-search` i
      `menu:search`, čekanje 80 ms, samo najnoviji odgovor vrijedi). Popis je `listbox`, broj rezultata
      se objavljuje kroz `aria-live`. Vitest 794.
      - „Samo najnoviji”: svaki upit ili promjena opsega dobiva token u stanju; odgovor s drugim
        tokenom se odbacuje, i nakon zatvaranja ploče (`lib/latest.ts` nije korišten jer je token u
        čistom stanju, što se testira bez Sveltea).
      - Fokus: otvaranje daje fokus polju; `Esc` i uspješno otvaranje vraćaju ga terminalu, a kad
        Neovim ne radi stablu; klik ili `Tab` izvan ploče je zatvara bez premještanja fokusa;
        neuspjelo otvaranje ostavlja ploču i vraća fokus u polje. `Terminal.svelte` ima `keepsFocus`
        da kraj pokretanja Neovima ne otme fokus polju.
      - Odluke `coder`-a izvan briefa: `↵` se ignorira dok se čeka odgovor (da se ne otvori rezultat
        starog upita); zatvaranje vraća opseg na „svi projekti”; tekstovi `searchCountOne/Few/Many`
        za hrvatsku množinu.
      - Pretpostavka: `laststatus=2` i `cmdheight=1` (Neovimove zadane vrijednosti; `init.lua` ih ne
        mijenja), pa su rezervirana dva retka (`tokens.search.reservedRows`).
      - Nije provjereno okom: poravnanje s statusnom trakom, pomicanje popisa, `⌘P` dok je fokus u
        ploči, `⌃P` u polju, zatvaranje klikom.
- [x] 7. Dimna provjera (`scripts/smoke.sh`): novi odjeljak „preview and search” (28 provjera; ukupno 91, oko
      16 s). `.md` s retkom `<script>` i slikom na `http://127.0.0.1:9` zadan na naredbenom retku;
      tipke `␣p` i `␣f` idu Neovimu kroz socket. Dokazano na sagrađenoj aplikaciji: frontend pri
      pokretanju javlja `SetPreview enabled=false`; nakon `␣p` redom `ui:toggle-preview` →
      `SetPreview enabled=true` (frontend je reagirao) → `preview:content state=html html_bytes>0`;
      izmjena buffera daje novi `preview:content`; nakon drugog `␣p` izmjena ne daje ništa (Neovim
      je ni ne javlja); `.txt` daje `state=unsupported`; `␣f` daje `ui:open-search`; tipke ne pišu u
      buffer; tekst dokumenta nije u logu; nakon gašenja nema procesa (ni pandoca), a u `preview/` je
      samo filter.
      - Da bi skripta imala što čekati, glavna sesija je dodala redak dijagnostike za događaj
        `preview:content` (ime datoteke, stanje, kod, veličina HTML-a; nikad sadržaj) — 5 redaka u
        `editorservice.go`, commit `8ec9832`. `coder` je prije toga ispravno stao i prijavio da
        aplikacija taj događaj ne bilježi.
      - Skripta ne može vidjeti webview: dokazuje da je Go poslao stranicu, ne da je nacrtana.
- [x] 8. Kraj faze: dokazi po kriterijima, popis **[R]**, `plan-reviewer` (krug 1 FAIL, krug 2 FAIL, krug 3
      **PASS**), sažetak vlasniku

### Pregled plana — krug 1 (2026-11-12): FAIL, bez blokera; ispravljeno

`plan-reviewer` je sam pokrenuo sve provjere (prolaze) i vlastitim neprijateljskim ulazima provjerio
filter (ništa izvršivo ni mrežno ne prolazi). Četiri stavke za ispravak:

1. U pregledu su ostajala sidra fusnota (`<a href="#fn1">`, `<a href="#fnref1">`), iako plan kaže da
   filter miče sve poveznice. U `srcdoc` okviru osnovna adresa je adresa roditelja, pa bi klik u
   WebKitu mogao odvesti okvir na stranicu aplikacije. **Ispravak u filtru:** bilješka u tekstu je
   brojčana oznaka (`<span class="footnote-ref"><sup>1</sup></span>`), a sadržaji bilješki idu na
   kraj kao obična numerirana lista (`<div class="footnotes">`), istim redoslijedom i s istim
   brojevima; sadržaj bilješke prolazi isto čišćenje. Test sigurnosti je pooštren: izlaz ne smije
   imati NIJEDNU oznaku `<a` ni atribut `href` (iznimka za `#…` je uklonjena). `TestRenderFootnotes`
   (16 slučajeva nakon kruga 1: tablica, lista, naslov, bilješka u bilješci, 12 bilješki…). Namjerno izmijenjeni
   testovi: slučaj `plain` u `TestUnsafeInFindsWhatItShould` i slučaj `footnote` u
   `TestRenderMarkup` (oba su očekivala pandocov oblik s poveznicama). Fuzz 20 s bez nalaza.
2. Tvrdnja u dokazima da izlaz „nema `href`-a” nije bila točna dok su sidra postojala; sada jest.
3. Datoteke izvan tablice plana nisu bile zapisane kao odstupanje: zapisane su niže.
4. Doslovne veličine u stilu okvira (`box-shadow 0 1px 3px`, `border-left 3px`): nove veličine u
   `tokens.ts` (`previewPageShadowOffset`, `previewPageShadowBlur`, `previewQuoteRuleWidth`), stil ih
   koristi kroz varijable; test pada na svaku duljinu osim 0 i 1 px izvan bloka `:root`.

Iz napomena: prazno stanje sada, kako traži `DESIGN.md`, uz „Otvori dokument” pokazuje tipke `␣f` /
`⌘P` (`EmptyState.svelte`, tekst `emptySearchHint`); broj provjera u dimnoj skripti ispravljen je na
28; ime tipa `Hit`, dodatno mjerilo poretka, preširoka tvrdnja o atributima i konstante izvan
`config`-a zapisani su niže.

### Pregled plana — krug 2 (2026-11-12): FAIL; nalazi kruga 1 riješeni, dva nova ispravljena

1. Bilješka u opisu slike brojila se dvaput (pandoc opis slike drži i kao `alt` i kao naslov
   figure), a broj je završio u `alt` tekstu. **Ispravak u filtru:** iz teksta koji postaje `alt`
   bilješke se uklanjaju; broji se samo ona u vidljivom naslovu. Slika u retku gubi bilješku iz opisa,
   kao i u čistom pandocu. Novi slučajevi u `TestRenderFootnotes` i svojstvo „broj oznaka u tekstu =
   broj stavki popisa” na svakom slučaju i u fuzzu.
2. `TestWdQuitDeclined/reload` je reviewer-u pao jednom pod `-race` (`saveAll = nil`). **Uzrok je
   stvarna greška iz faze 3**, potvrđena determinističnom reprodukcijom: obavijest o zapisu koji radi
   `SaveAll` stavi vlastiti zadatak izvoza, koji može krenuti prije izričitog zadatka gašenja. Tada
   je kod „Učitaj ponovno” izričiti zadatak zaključio da nema što izvesti, `saveAll` je vratio `nil` i
   aplikacija bi se ugasila; kod „Odustani” se dijalog konflikta pojavio dvaput. **Ispravak**
   (`editorwords.go`): `saveAllWords` prije `SaveAll` počinje pratiti ishode svih izvoza svojih
   dokumenata (`exportWatch`) i zbraja ih bez obzira na to koji ih je zadatak obavio; izričiti zadatak
   istog kruga ne pita ponovno nakon odbijanja; obavijest o zapisu za koji je izvoz već odbijen ne
   pita ponovno (novi zapis ima drugi otisak). Novi testovi `TestWdOrder` (oba redoslijeda × Odustani
   / Učitaj ponovno), `TestWdOrderGate`, `TestWorkQueueWatch`, `TestWorkQueueRound`; postojeći nisu
   mijenjani. `go test -count=30 -race` nad testovima gašenja i reda prolazi; četiri puna `go test
   -count=1 -race ./...` prolaze.

Napomene: `TestRenderFootnotes` ima više slučajeva nego što je pisalo (ispravljeno); točka 13 ručne
provjere kaže da u praznom stanju radi samo `⌘P`; commit `588c64a` je spojio četiri ispravka (od
kruga 2 svaki ispravak je zaseban commit).

### Pregled plana — krug 3 (2026-11-12): PASS

Reviewer je tri puta pokrenuo `task test:race` bez predmemorije i 30 puta testove gašenja i reda (sve
prolazi), ponovio vlastite neprijateljske ulaze za bilješke u opisu slike, i provjerio da ispravak
gašenja ne mijenja obično spremanje (otisak „odbijeno” preskače samo ponovljenu obavijest istog
zapisa; svaki novi `:w` daje novi otisak i ponovno pita). Otvoreno ostaje ono iz „Otvoreno nakon
faze 4” i kriterij **[R]**.

### Odstupanja od plana faze (datoteke i ponašanje)

Uz odstupanja zapisana uz pojedine korake:

- **Datoteke izvan tablice „Datoteke” u planu:** `frontend/src/components/Terminal.svelte` (javlja
  pomak iznad statusne trake nakon svakog `fit()` i ima `keepsFocus`, da kraj pokretanja Neovima ne
  otme fokus polju tražilice); `frontend/src/lib/apiError.ts` i `errorMessage.ts` (kod
  `preview-too-large`; popis kodova je tipiziran pa svaki Go kod mora imati par); `theme/cssVars.ts`
  (varijable fonta i tipografije pregleda); `components/EmptyState.svelte` (natuknice tipki, vidi
  krug 1 pregleda); `internal/nvim/reload_test.go` i ispravak `Reload`-a (greška iz faze 3);
  `editorwords.go` i `editorwords_test.go` (ispravak redoslijeda zadataka izvoza pri gašenju, greška
  iz faze 3 nađena u krugu 2 pregleda).
- **Ugovor:** tip rezultata pretrage je `project.Hit` (plan ga je zvao `SearchHit`); polja i JSON imena
  su ista. Poredak ima i mjerilo „kraće ime prije duljeg” ispred abecednog, koje plan nije naveo.
- **Čitač pregleda** prima sirovi HTML (plan je mjerio s isključenim); razlog je uz korak 1.
- **Konstante izvan `internal/config`:** `maxToolOutput` i `killGrace` u `internal/preview` (isti
  obrazac kao u `internal/convert` iz faze 3); `search.debounceMs` (80) i `search.reservedRows` (2) su
  ponašanje frontenda, a stoje u `theme/tokens.ts`, jer frontend nema drugo mjesto za konstante.
  `CLAUDE.md` kaže „nema hardkodiranih limita”; kao i u fazi 3, ovo je za vlasnikovu odluku.
- **Atributi u HTML-u pregleda:** filter briše svaki atribut koji dolazi IZ DOKUMENTA, osim `class` (i
  brojčanih `width` / `height` na slici). Atributi koje pandoc dodaje sam (`data-cites`, `role`,
  `aria-hidden`, `title` slike, `style="text-align"` u tablici) ostaju; nijedan ne nosi adresu ni kod.

### Dokazi po kriterijima (2026-11-12)

Naredbe iz korijena repozitorija, pokrenute nakon zadnje izmjene koda (ispravci iz kruga 2 pregleda).

- `go test -count=1 -race ./...` → svih 8 paketa `ok`; `task test` → exit 0 (vitest `Test Files 27
  passed`, `Tests 796 passed`); `task lint` → `0 issues.`, svelte-check `0 ERRORS 0 WARNINGS`;
  `task build` → exit 0; `bash scripts/smoke.sh` → `smoke: all checks passed` (91 provjera, dvaput
  zaredom); `bash scripts/shutdown.sh` → `shutdown: all checks passed`.
- Go testovi (`go test -list`): `Pisar` 170, `config` 30, `convert` 55, `docsafe` 92, `nvim` 68,
  `preview` 24, `project` 44, `term` 35. Pokrivenost (`go test -count=1 -cover ./...`, nakon
  ispravaka iz kruga 2): `Pisar` 84,3 %, `config` 84,5 %, `convert` 92,8 %, `docsafe` 85,2 %, `nvim`
  88,7 %, `preview` 92,2 %, `project` 96,0 %, `term` 93,9 % (`Pisar` i `nvim` se mijenjaju između
  pokretanja za nekoliko desetinki).

1. Pregled se pali i gasi (`␣p`, gumb), osvježava se nakon debouncea: `TestPvCoalesce` (više izmjena
   unutar debouncea → jedna pretvorba sa zadnjim tekstom), `TestPvOff` (isključen → nijedna),
   `TestPvEnableRepeats`, `TestPvSupersede` (nova izmjena otkazuje pretvorbu u tijeku, rezultat
   otkazane se ne šalje), `TestPvSameTextOnce`, `TestPvToggleSearch` i `TestPrevKeys` (`␣p` → događaj,
   pravi Neovim), `TestChanged`, `TestChangedIns`; vitest `previewState.test.ts` (prekidač, gašenje
   ispod 1100 px, bez samopaljenja); `smoke.sh` (cijeli lanac kroz pravi frontend).
2. Pregled je u sandboxed iframeu; skripta u dokumentu se ne izvršava: `TestRenderSafety` i `FuzzRender`
   (za svaki neprijateljski ulaz izlaz nema oznake skripte, obrađivača događaja, oznake `<a`, atributa
   `href` ni adrese), `TestRenderFootnotes` (fusnote bez poveznica; bilješka u opisu slike jednom),
   `TestRenderDoesNotFetch` (lokalni poslužitelj broji 0 zahtjeva), `TestRenderAttributes`,
   `TestRenderImages` (bijeg iz foldera, neprijateljski SVG), `TestPvPandoc` (pravi Neovim i pandoc:
   `.md` sa `<script>` daje stranicu bez oznake skripte); vitest `previewFrame.test.ts` (`sandbox` je
   točno `allow-same-origin`; CSP `default-src 'none'`, `form-action 'none'`, `base-uri 'none'`),
   `previewGuard.test.ts` (nigdje `{@html}`, `innerHTML` samo u `previewWrite.ts`, nijedna druga
   dozvola okvira).
3. Tražilica: `TestSearchFolding`, `TestSearchWords`, `TestSearchRanking`, `TestSearchMatches`
   (`internal/project`); `TestSearchDocuments` (dva projekta, sužavanje na jedan), `TestSearchCache`,
   `TestSearchThroughService`, `TestSearchConcurrent` (usluga); `TestPvToggleSearch` (`␣f` → događaj),
   `TestMenuSearch` (`⌘P` → `menu:search`); vitest `searchKeys.test.ts` (`↑` `↓` `↵` `Esc` `⌃P`),
   `searchState.test.ts` (opseg, otvaranje, zatvaranje i fokus, samo najnoviji odgovor),
   `searchHighlight.test.ts`; `smoke.sh` (`␣f` → `ui:open-search`).

### Ručna provjera [R] — za vlasnika (agent ovo ne može provjeriti)

1. Otvori `.md` i pritisni `␣p` (ili gumb „Pregled”): desno se pojavi stranica (bijela, na sivoj
   podlozi, blaga sjena); gumb je ispunjen akcentom; terminal se suzi i ostaje poravnat.
2. Font stranice je Source Serif 4 (ne sistemski serif); naslovi, tablica, citat, kod i slika iz
   foldera dokumenta izgledaju kako `DESIGN.md` traži. Fusnota je broj u tekstu i stavka u popisu na
   kraju stranice; nije poveznica.
3. Tipkaj u dugom dokumentu s pregledom pomaknutim dolje: nakon stanke se sadržaj osvježi, a pregled
   NE skače na vrh i ne treperi.
4. U dokument upiši `<script>alert(1)</script>` i `![x](https://example.com/x.png)`: ništa se ne
   izvrši, skripta se ne vidi, umjesto slike stoji njezin opis; poveznica izgleda kao poveznica, a
   klik ne vodi nikamo.
5. Klik u pregled, pa tipkaj: tipke idu u Neovim. Klik na gumb „Pregled”: isto.
6. Suzi prozor ispod 1100 px: pregled se ugasi, gumb je onemogućen s objašnjenjem; nakon proširenja
   se ne pali sam.
7. `.txt` dokument s uključenim pregledom: ploča kaže da je pregled za Markdown i Wordove dokumente.
8. Wordov dokument (`.docx`): pregled radi, slike iz dokumenta se vide.
9. `␣f` i `⌘P` otvaraju tražilicu na dnu stupca editora; donji rub ploče je točno iznad Neovimove
   statusne trake; ne prekriva stablo ni pregled.
10. Upiši dio imena bez dijakritika (npr. „cesalj” za „Češalj”): podudaranje je istaknuto (akcent,
    podebljano), projekt je desno; `↑` `↓` biraju (popis se pomiče iza osmog reda); `↵` otvara i
    vraća fokus u Neovim; `Esc` zatvara i vraća fokus.
11. `⌃P` u ploči mijenja opseg (svi projekti ↔ projekt aktivnog dokumenta) i ne miče kursor u polju;
    `⌘P` dok je ploča otvorena samo vraća fokus u polje.
12. `Esc` u normal modu Neovima (ploča zatvorena) ne zatvara ništa.
13. Bez otvorenog dokumenta stupac editora pokazuje „Otvori dokument” i ispod toga `␣f` / `⌘P` s
    natuknicom. Prazno stanje se vidi samo dok Neovim ne radi, pa tada stvarno radi samo `⌘P` (`␣f`
    je Neovimovo mapiranje); natuknica doslovno slijedi `DESIGN.md`. Presudi treba li je promijeniti.

### Otvoreno nakon faze 4

- Nije provjereno okom ništa od izgleda i fokusa (popis gore); posebno učitava li okvir font uz CSP
  `font-src` u Wails prozoru.
- `⌘C` iz pregleda ne radi (označavanje mišem radi); kopiranje iz pregleda nije bilo u planu.
- Simbolička veza u folderu dokumenta koja pokazuje izvan njega ugrađuje se u pregled; folder s `:` u
  imenu dobiva pregled bez slika.
- Tražilica ne vidi izmjenu napravljenu izvana dok je promatrač ne javi; `dj` ne nalazi `đ`.
- Položaj ploče tražilice pretpostavlja Neovimove zadane `laststatus=2` i `cmdheight=1`.
- Prevelik `.txt` buffer u pregledu daje grešku „prevelik”, a ne poruku „nije podržano”.
- Bilješka u opisu slike unutar retka ne vidi se u pregledu (kao ni u čistom pandocu).
- Prazno stanje nudi `␣f`, a dok Neovim ne radi stvarno radi samo `⌘P` (doslovno po `DESIGN.md`).
- Konstante izvan `internal/config` (popis u „Odstupanja od plana faze”) čekaju vlasnikovu odluku.

### Izvan opsega

Pretraga sadržaja, pregled za `.txt`, sinkronizirano pomicanje pregleda, bojanje koda, ispis i PDF,
uređivanje u pregledu, klikabilne poveznice u pregledu, fuzzy podudaranje, povijest pretraga,
postavke za prozu i pravopis, pakiranje, tamna tema.

### Kriteriji prihvaćanja

- [x] pregled se pali i gasi (`␣p`, gumb), osvježava se nakon debouncea — dokaz 1
- [x] pregled je u sandboxed iframeu; skripta u dokumentu se ne izvršava (test) — dokaz 2
- [x] tražilica: `␣f` i `⌘P`, pretraga kroz sve projekte, `⌃p` sužava, `↵` otvara, `Esc` vraća fokus — dokaz 3
      (tipke u prozoru i fokus su u ručnoj provjeri)
- [x] **[R]** izgled odgovara `docs/design/DESIGN.md` — potvrdio vlasnik ručno 2026-11-12 („testirano i radi
      sve”)

## Faza 5 — Udobnost pisanja

Effort high. Plan faze odobren 2026-11-12.

### Odluke vlasnika (2026-11-12)

- Hrvatski rječnik (Vimov službeni `hr.utf-8.spl`, LGPL) ulazi u repozitorij i u aplikaciju.
- Prečaci: u Neovimu `␣b`, `␣i`, `␣h`, `␣l`; uz to izbornik „Oblikovanje” s `⌘B` i `⌘I`.
- Zadano: pravopis uključen, brojevi redaka isključeni; `␣s` i `␣n` ih mijenjaju.
- **Tehničke konstante izvan `internal/config`** (otvoreno od faze 3: `maxXMLDepth`, `maxToolOutput`,
  `killGrace`, `maxFieldDepth`, `maxInstrBytes`, `maxLabelBytes`, `copyBufSize`, `maxWorkMetaBytes`,
  `xattrRetries`, broj pokušaja slučajnog imena, `search.debounceMs`, `search.reservedRows`) **ostaju
  u paketima kao prihvaćena iznimka**: to su unutarnje zaštitne granice, ne postavke. Pravilo iz
  `CLAUDE.md` i dalje vrijedi za sve što ima smisla kao postavka (rokovi, veličine, broj backupa).

### Ovisnosti (odobrene)

- Nema novih Go ni npm ovisnosti. Novi resurs: `hr.utf-8.spl` + `README_hr.txt` (LGPL).

### Izmjereno prije plana (Neovim 0.12)

- Neovim nosi samo engleski rječnik; za jezik kojeg nema pri pokretanju pita smije li ga preuzeti
  (to bi zamrznulo aplikaciju), pa se dodatak za preuzimanje isključuje.
- Vimov `hr.utf-8.spl` (1,26 MB; SHA-256 počinje s `36ea13e3561a4fac`): na probnom tekstu od 110
  riječi označene samo tri namjerno krive. LibreOfficeov `hr_HR` preveden s `:mkspell` označio je 58
  od 110 ispravnih (aliasi `AF` u `.aff` nisu podržani) — odbačen.
- `spelllang=hr,en` radi zajedno. Colorscheme iz faze 2 već ima `SpellBad` kao valovitu crtu.

### Ugovor Go ↔ frontend

```go
EditorService.Format(ctx, kind string) error   // "bold" | "italic" | "heading" | "list"; nepoznato → kod "invalid-format"
event "menu:format" string                      // "bold" | "italic" (izbornik „Oblikovanje”, ⌘B / ⌘I)
```

### Koraci

- [x] 1. Proza i pravopis. `init.lua`: `wrap`, `linebreak`, `breakindent`, bez brojeva redaka; `j` `k` `↓` `↑` po
      prikazanim recima (s brojem ispred po stvarnim), i u insert modu; `spell` s `spelllang=hr,en`;
      `␣s` i `␣n`; `laststatus=2` i `cmdheight=1` sada izričito. Rječnik `nvim/spell/hr.utf-8.spl`
      (+ `README_hr.txt`, LGPL) je ugrađen (`go:embed`) i `writeConfig` ga zapisuje u
      `<home>/nvim/config/pisar/spell/` samo kad se razlikuje. Vlastite riječi (`zg`):
      `<home>/spell/pisar.utf-8.add` (putanja stiže kroz `PISAR_SPELLFILE`; izvan mape koju aplikacija
      prepisuje; preživljava ponovno pokretanje).
      - **Odstupanje od plana:** `loaded_spellfile_plugin=1` NIJE dovoljan. Kad `spelllang` traži jezik
        bez datoteke, jezgra Neovima sama postavi upit „Download? [y/N]” i pokretanje se zamrzne
        (izmjereno). Zato `init.lua` u `spelllang` stavlja samo jezike čija datoteka postoji (bez
        hrvatskog rječnika ostaje `en`) i postavlja ga kroz `silent!` (oštećen rječnik inače daje
        hit-enter). `TestSpellBoot` to dokazuje.
      - `spellcapcheck=""`: zadani uzorak je označavao mala slova nakon „npr.”, „itd.”, „tj.” i svaku
        stavku „1. kupiti” kao grešku velikog slova.
      - **Valovita crta (odstupanje od plana, koji je predviđao ravnu crtu ako valovita ne radi):** Neovim
        pri pokretanju pita terminal (DECRQSS) podržava li je; xterm.js 6.0.0
        odgovara `0m` (taj mu je dio označen kao neispravan), pa je kriva riječ bila podcrtana ravnom
        crtom u boji teksta. Go sada na taj upit odgovara sam (`termAnswers`, kao za DSR i DA1) s
        `0;4:3m`; Neovim tada šalje `CSI 4:3 m` i boju `58:2::180:35:24` (`danger`), a DOM renderer
        xterm.js-a stil 3 crta kao valovitu crtu (provjereno u izvoru, ne okom). Prepoznavanje upita
        razlomljenog preko dva čitanja izdvojeno je u `queryScanner` i dobilo test.
      - **Ispravak greške iz faze 2 (nađena ovdje):** `Process.Redraw` je zvao `:redraw!`, koji ne
        šalje nijedan bajt kad se ekran nije promijenio, pa je nakon ponovnog učitavanja stranice
        prozor mogao ostati prazan. Sada zove `:mode` („Clears and redraws the screen”, `:help
        :mode`). `TestRedrawSends`; `TestEdInitial`, koji je zbog toga postao nestabilan (13 od 30
        padova), nije mijenjan i prolazi 30 od 30 (kod glavne sesije 20 od 20).
      - `scripts/smoke.sh`: popis sadržaja privremenog homea dobio je `spell` (jedan redak).
      - Krhkost u testovima: putanja Neovimovog socketa u testovima paketa `nvim` je `t.TempDir()` +
        `nvim-<pid>-<redni broj>.sock`; redni broj je zajednički za cijeli testni program, pa kasniji
        testovi s dugim imenom mogu prijeći 103 bajta. Novi `writing_test.go` se zato po imenu sortira
        zadnji. Nije ispravljeno (ispravak bi socket stavio izvan `t.TempDir()`).
- [x] 2. Oblikovanje u Neovimu. Sav Lua je u `init.lua` (sada oko 835 redaka; zaseban modul bi tražio izmjene
      `writeConfig`-a i testnog okruženja); ulazna točka `pisar_format(kind)`; mapiranja `␣b` `␣i` `␣h`
      `␣l` u normal i visual modu. `nvim.Process.Format(ctx, kind)` (+ `term.Session.Format`),
      `ErrUnknownFormat`. 12 testova s oko 170 slučajeva, svaki kroz mapiranje i kroz `Format`.
      - Podebljano i kurziv: visual omota označeno (rubni razmaci se režu), a već omotano odmota;
        normal djeluje na riječ pod kursorom (interpunkcija ostaje izvan oznaka); insert umeće par i
        stavlja kursor između, a ista radnja ispred zatvarajuće oznake je preskače („izađi”). `V`
        omata redak po redak. Blokovski odabir se odbija bez promjene.
      - Naslov: bez oznake → `#` → `##` → `###` → bez oznake (razine 4–6 → bez oznake); zamjenjuje
        oznaku liste (`- a` → `# a`), numerirana stavka zadržava broj. Lista: `- ` se dodaje ili skida
        (`*`, `+` se skidaju); numerirana stavka i naslov se ne diraju; uvlaka se čuva; u više redaka
        skida samo ako svi imaju oznaku.
      - Svaka radnja u normal i visual modu je jedan korak za `u`; u insert modu je dio tekućeg unosa
        (`u` nakon „tekst, ⌘B, tekst, ⌘B” vraća cijeli unos). Registri, pretraga, poruke netaknuti.
      - Mod koji čeka unos: `Format` vraća nil i ne mijenja ništa. Izmjereno: Lua poziv se NE vraća dok
        je otvoren hit-enter prompt, pa Go prvo pita `nvim_get_mode` (uvijek odgovara) i odustaje kad je
        `blocking`. Buffer s `modifiable=false`: ništa, bez greške.
      - Riječ je niz znakova razreda 2 (`charclass`) bez `_`: `\k` u regexu hvata i emoji.
      - Krhkost s duljinom putanje socketa u testovima paketa `nvim` ublažena je u koraku 3 (kraće ime).
- [x] 3. `user.lua`: `<home>/user.lua` (putanja kroz `PISAR_USER_LUA`; `NvimPaths.UserLua`) učitava se na kraju
      `init.lua`, ako postoji; aplikacija je nikad ne stvara ni ne mijenja. Veza na običnu datoteku se
      učitava; mapa, viseća veza i FIFO se preskaču. `nvim.Process.UserConfig()` nakon spajanja čita
      grešku (`g:pisar_user_error`) i napomenu (`g:pisar_user_note`); Neovim pokaže jedan skraćeni
      redak, Go zapiše putanju i prvi redak greške. 10 testova u paketu `nvim`, 3 u glavnom.
      - **Vraća se nakon `user.lua`:** `hidden`; zastavice `t` i `T` u `shortmess`; `laststatus=2`;
        `cmdheight=1`; skupine autocmdova `PisarNotify` i `PisarFormat` (grade se iznova, bez dvostrukih);
        `pisar_format`; `spelllang` bez jezika čiji rječnik ne postoji (uz napomenu).
      - **Smije se mijenjati:** sve ostalo, uključujući sva `<leader>` mapiranja (radnje aplikacije
        ostaju dostupne kroz gumbe i izbornik), opcije proze, boje, vlastita mapiranja i autocmdove.
        Promjena `mapleader` vrijedi samo za nova mapiranja.
      - Izmjereno i zaštićeno: ispis iz `user.lua` pri učitavanju (30 redaka, dugi redak) hvata se u
        `g:pisar_user_output` (`silent` bi ga obrisao iz `:messages`); ispis iz `vim.schedule` ili
        `VimEnter` je ZAMRZAVAO pokretanje, pa je za vrijeme pokretanja `messagesopt` bez `hit-enter` i
        vraća se nakon `VimEnter`; `spelllang` s jezikom bez rječnika iz `user.lua` zamrzavao je
        pokretanje i nakon provjere, jer jezgra doda vlastiti `VimEnter` autocmd koji zove
        `nvim.spellfile`, pa je taj modul zamijenjen praznim.
      - **I dalje može zamrznuti pokretanje:** `vim.fn.input()`, `confirm()`, `getchar()` ili beskrajna
        petlja u `user.lua`. `Start` tada istekne (`NvimStartTimeout`) i ne ostavlja proces
        (`TestUIn`). Višeredna poruka iz tajmera ili autocmda NAKON pokretanja može otvoriti hit-enter;
        Go pozivi se tada brane kao i dosad (`Format`, `Save`).
      - Ispravci glavne sesije uz ovaj korak: (1) ime socketa je skraćeno (`n<pid u bazi 36>-<redni broj
        u bazi 36>.sock` umjesto `nvim-<pid>-<redni broj>.sock`), jer su putanje u testovima paketa
        `nvim` bile na 100–103 od 103 dopuštena bajta (ovisno o slučajnom sufiksu `t.TempDir()`); (2)
        `TestTextState` (faza 4) je uspoređivao dva `vim.json.encode` niza, čiji redoslijed ključeva
        nije određen, pa je jednom pao pod `-race`; snimka sada koristi `vim.inspect` (sortira
        ključeve). Tvrdnje testa su iste.
- [x] 4. Usluga, izbornik, frontend. `EditorService.Format(kind)` (nepoznata radnja → kod `invalid-format`);
      izbornik „Oblikovanje” između „Uređivanje” i „Prozor”: „Podebljano” (`⌘B`), „Kurziv” (`⌘I`); klik
      samo emitira `menu:format`. Frontend: `lib/formatAction.ts` (`shouldFormat`: šalje samo kad
      terminal ima fokus i Neovim radi; polje za unos, stablo, gumb, pregled i bez fokusa → ne).
      Vitest 826.
      - **Redoslijed tipki i oblikovanja:** tipke terminala i `Format` idu kroz ISTI serijski red
        (`serialSender.run`; poziv je zapreka pa se podaci prije i poslije njega ne spajaju). Bez toga
        bi Wails, koji svaki poziv obrađuje u vlastitoj goroutini, mogao poslati `Format` prije tipki:
        „tekst” + `⌘B` dalo bi `**tekst`. Izbornik zato emitira bez vlastite goroutine (odstupanje od
        uzorka „Kopiraj”). Drugi dio puta (PTY naspram RPC-a) izmjeren je u `TestEdFmtOrder`: Neovim
        obradi već upisane tipke prije kasnijeg RPC poziva (40 od 40 pod `-race`).
      - Greška poziva i `not-running` se samo zapišu u konzolu; korisniku se ne prikazuju.
      - Datoteke izvan tablice plana: `frontend/src/lib/serialSender.ts`, `components/Terminal.svelte`
        i `EditorPane.svelte` (zajednički red), `lib/apiError.ts`, `errorMessage.ts` (novi kod),
        `projectservice.go` (kod greške).
      - Prošireni postojeći testovi (samo dodavanje): `TestEdCodes`, `TestCodeOf`, `apiError.test.ts`,
        `errorMessage.test.ts`, `serialSender.test.ts`.
      - Nije provjereno: izbornik u pravom Wails prozoru (testirana je radnja i konstante); tipke
        utipkane nakon `⌘B`, a prije nego događaj stigne u frontend, mogu proći prije oblikovanja
        (nekoliko ms).
- [x] 5. Dimna provjera (`scripts/smoke.sh`): novi odjeljak „writing” (55 provjera; ukupno 146, oko 27 s). Tri
      pokretanja sagrađene aplikacije s istim `PISAR_HOME`: (A) bez `user.lua` — rječnik u homeu je
      bajt-identičan onome u repozitoriju, `user.lua` nije stvoren, opcije proze i pravopisa, `j`/`k`
      po prikazanim recima, `␣n` i `␣s`, `␣b` `␣i` `␣h` `␣l` i `u`, `zg` piše u
      `<home>/spell/pisar.utf-8.add`; (B) s `user.lua` — njegova opcija vrijedi, `hidden` i
      `laststatus` su vraćeni, riječ dodana sa `zg` i dalje nije označena, datoteka netaknuta; (C)
      `user.lua` s greškom — aplikacija radi, prva naredba vrijedi, greška je u `g:pisar_user_error` i
      u logu. Tipke idu samo Neovimu kroz socket; nakon svake se čeka učinak s rokom.
      - Nije pokriveno skriptom: `⌘B` / `⌘I` iz izbornika (tražilo bi slanje tipki aplikaciji) i izgled
        valovite crte u prozoru.
- [x] 6. Kraj faze: dokazi po kriterijima, popis za ručnu provjeru, `plan-reviewer` (krug 1 FAIL, krug 2 FAIL,
      krug 3 **PASS**), sažetak vlasniku

### Pregled plana — krug 1 (2026-11-12): FAIL, bez blokera; ispravljeno

`plan-reviewer` je sam pokrenuo sve provjere (prolaze; testove glavnog paketa i `internal/nvim` tri
puta pod `-race`) i `init.lua` isprobao vlastitim ulazima u Neovimu bez sučelja. Nalazi:

1. `user.lua` s `eventignore = "all"` (ili s pojedinim događajima) utišao bi sve obavijesti `pisar_*`,
   pa `:w` na Wordovom dokumentu ne bi pokrenuo izvoz. **Ispravak:** nakon `user.lua` se iz
   `'eventignore'` i `'eventignorewin'` (globalno i po prozoru) uklanjaju `all` i događaji koje koriste
   skupine `PisarNotify` i `PisarFormat` (popis se čita iz samih skupina); ostali vlasnikovi unosi
   ostaju. Uz to se brišu `g:pisar_reloading`, `g:pisar_preview`, `g:pisar_channel` (Pisarove unutarnje
   varijable; Go ih postavlja sam), a `g:pisar_user_error` koji nije tekst više ne ruši `Start`
   (izmjereno: prije je padao na dekodiranju). Testovi `TestUEi`, `TestUOne`, `TestURel`, `TestUVar`;
   `TestWdIgnore` (Wordov dokument + `eventignore=all` → `:w` i dalje izvozi).
2. Prečaci `⌘B` / `⌘I` nisu imali test: stavke izbornika „Oblikovanje” i svi prečaci aplikacije sada su
   podaci (`formatItems`, `menuShortcuts`), s testovima `TestMenuFormatItems` i
   `TestMenuShortcutsUnique` (nema dvostrukih među šest prečaca). Položaj izbornika u prozoru ostaje u
   ručnoj provjeri.

Iz napomena, ispravljeno: podebljano / kurziv u normal modu na riječi UNUTAR već omotanog izraza
skida oznake cijelog izraza (`**jedna riječ**` → `jedna riječ`; prije je nastajalo
`**jedna **riječ****`); apostrof između dva slova je dio riječi (`don't`); visual grana poštuje
`'selection'` (`exclusive`); komentari u `format.go` i `init.lua`. Namjerno NIJE promijenjeno:
podvlaka i dalje završava riječ (`snake_case` → `**snake**_case`), jer to tvrdi postojeći test
`underscore ends a word` (podvlaka je u Markdownu oznaka naglaska); zahtjev glavne sesije za suprotnim
je povučen.

### Pregled plana — krug 2 (2026-11-12): FAIL, bez blokera; ispravljeno

Nalazi kruga 1 potvrđeni kao riješeni, osim jednog dijela prvog:

1. Vraćanje nakon `user.lua` vrijedilo je samo za ono što `user.lua` promijeni dok se učitava;
   `vim.schedule` ili `VimEnter` autocmd iz `user.lua` i dalje su mogli postaviti `eventignore`, ugasiti
   `hidden` ili postaviti `g:pisar_reloading`. **Ispravak:** vraćanje se ponavlja tri puta: sinkrono u
   Pisarovom `VimEnter` autocmdu (registriran nakon `user.lua`, pa ide iza vlasnikovih), zatim
   zakazano, i još jednom na zahtjev Go-a u istom RPC pozivu u kojem Go postavlja `g:pisar_channel` i
   čita napomenu (pa čita nakon svih prolaza, bez utrke). Izmjereni redoslijed: učitavanje → vlasnikov
   `VimEnter` → Pisarov `VimEnter` → zakazani pozivi iz `user.lua` i `vim.defer_fn` → ono što je
   zakazao vlasnikov `VimEnter` → Pisarov zakazani prolaz → spajanje Go-a. Kasniji prolazi ne diraju
   `g:pisar_channel` ni `g:pisar_preview`. Testovi `TestULate` (7 slučajeva), `TestULateAnn`.
   - **Predložena granica, čeka odluku vlasnika (zapisana i u komentaru u `init.lua`, tvrdi je
     `TestULimit`):** `user.lua` je
     vlasnikov kod. Zaštita pokriva učitavanje i kraj pokretanja, ne kasniji rad: autocmd koji
     postavku promijeni kasnije (npr. na `BufWinEnter`), `vim.defer_fn` s duljim kašnjenjem, dvostruko
     zakazan poziv koji stigne nakon spajanja Go-a, ili zamjena Neovimovih funkcija (`vim.rpcnotify`)
     i dalje mogu utišati obavijesti.

Iz napomena: poruka za buffer bez imena pri „Spremi sve” bila je engleska kopija Neovimove (E141) —
novi ključ `nvimNoFileName` („Dokument bez naziva (br. {buffer}) nije spremljen; …”; `coder` je
predložio „Međuspremnik”, što na hrvatskom znači clipboard, pa je glavna sesija tekst uskladila s
ostatkom sučelja); u `nvimUserLuaError` uputa je sada ispred razloga, jer se na 80 stupaca uvijek
odrezala; Lua i Go predložak poruke ispunjavaju jednako (sva pojavljivanja). Nedostižan engleski tekst
`"unknown format: "` u `init.lua` ostaje (korisnik ga ne može vidjeti).

### Pregled plana — krug 3 (2026-11-12): PASS

Reviewer je ponovio svoje pokuse s odgođenim izmjenama iz `user.lua` (obavijesti stižu, nisu
dvostruke, stanje se ne gubi), provjerio da uklanjanje zasebne registracije kanala nije ništa
pokvarilo, i sam izmjerio brojeve u zapisu. Jedini nalaz: preostalu granicu oko `user.lua` zapis je
zvao „prihvaćenom”, a vlasnik je nije prihvatio — ispravljeno u „predložena, čeka odluku vlasnika”
(i u komentarima u `init.lua` i testu). Napomena: u poruci o grešci u `user.lua` okvir ima 52 znaka,
pa na 80 stupaca za razlog ostaje oko 16 znakova; puni tekst je u `g:pisar_user_error` i u logu.

### Dokazi po kriterijima (2026-11-12)

Naredbe iz korijena repozitorija, pokrenute nakon zadnje izmjene koda.

- `go test -count=1 -race ./...` → svih 8 paketa `ok`; `task test` → exit 0 (vitest `Test Files 28
  passed`, `Tests 826 passed`); `task lint` → `0 issues.`, svelte-check `0 ERRORS 0 WARNINGS`;
  `task build` → exit 0; `bash scripts/smoke.sh` → exit 0 (146 provjera, dvaput zaredom);
  `bash scripts/shutdown.sh` → exit 0.
- Go testovi (`go test -list`, nakon ispravaka kruga 2): `Pisar` 188, `config` 33,
  `convert` 55, `docsafe` 92, `nvim` 121,
  `preview` 24, `project` 44, `term` 36. Pokrivenost (`go test -count=1 -cover ./...`): `Pisar`
  84,3 %, `config` 84,5 %, `convert` 92,8 %, `docsafe` 85,2 %, `nvim` 89,0 %, `preview` 92,2 %,
  `project` 96,3 %, `term` 92,6 % (mijenja se između pokretanja; `nvim` za do oko jedan postotni bod,
  jer grane ovise o vremenu pokretanja i gašenja Neovima).

1. `init.lua`: prelamanje po riječima, kretanje po prikazanim recima, bez brojeva redaka (prekidač):
   `TestProseOptions` (i za kasnije otvoren buffer), `TestRows` (`j` ostaje u istom stvarnom retku,
   `2j` ide dva retka), `TestRowsIns`, `TestRowsPum`, `TestProseSwitches` (`␣n`, `␣s`); `smoke.sh`.
2. Provjera pravopisa hr + en: `TestSpell` (kriva riječ označena, ispravne hrvatske i engleske nisu),
   `TestSpellBoot` i `TestSpellNoDl` (pokretanje bez upita i kad rječnika nema), `TestSpellZg`
   (vlastita riječ preživljava ponovno pokretanje), `TestUndercurl` (valovita crta u boji `danger`),
   `TestEdWriteConfigDictionary` (rječnik zapisan bajt-identično, drugi put se ne piše),
   `TestEdWriteConfigKeepsUserWords`, `TestEdStartSpell`; `smoke.sh`.
3. Prečaci za naslov, bold, italic, listu: `TestFmtBold`, `TestFmtItalic`, `TestFmtBoldIns`,
   `TestFmtHead`, `TestFmtList`, `TestFmtUndo`, `TestFmtRO`, `TestFmtWait`, `TestFmtSpan`, `TestFmtApos`,
   `TestFmtSel` (svaka radnja kroz mapiranje i kroz `Format`); `TestEdFmt`, `TestEdFmtOrder`,
   `TestWdFmtRO`; `TestMenuFormat` (radnja → `menu:format`), `TestMenuFormatItems` (prečaci
   `CmdOrCtrl+b` / `CmdOrCtrl+i`), `TestMenuShortcutsUnique`; vitest `formatAction.test.ts` (odluka o fokusu),
   `serialSender.test.ts` (redoslijed tipki i oblikovanja); `smoke.sh` (`␣b` `␣i` `␣h` `␣l`).
4. `user.lua` nakon ugrađene konfiguracije: `TestUOpts` (njegova opcija nadjačava ugrađenu), `TestUErr`
   (greška ne ruši, ugrađeno vrijedi), `TestUKeep` (vraćeno ono o čemu Go ovisi, događaji stižu i
   nisu dvostruki), `TestUEi`, `TestULate` i `TestWdIgnore` (`eventignore` postavljen pri učitavanju ili
   do kraja pokretanja ne može utišati obavijesti; kasnije može — `TestULimit`), `TestUFile` (bez datoteke, mapa, veze), `TestUNoWrite`, `TestUOut`, `TestUIn`,
   `TestUSpell`; `TestEdUserPath`, `TestEdUserLua`, `TestEdUserLuaLog`; `smoke.sh` (B i C).

### Ručna provjera — za vlasnika (agent ovo ne može provjeriti)

Faza nema kriterij **[R]** u planu; ovo su stvari koje se vide samo u prozoru.

1. Dugi odlomak se prelama po riječima; `j` / `k` i strelice idu po recima koje vidiš; nema brojeva
   redaka; `␣n` ih pali i gasi.
2. Kriva riječ ima VALOVITU crvenu crtu (ne ravnu); hrvatske i engleske riječi nisu podcrtane; `␣s`
   pali i gasi; `z=` nudi prijedloge; `zg` dodaje riječ i ona ostaje nakon ponovnog pokretanja.
3. `␣b`, `␣i` na riječi i na označenom tekstu; `␣h` kruži kroz razine naslova; `␣l` dodaje i skida
   oznaku liste; `u` vraća svaku radnju jednim pritiskom.
4. U insert modu utipkaj „tekst”, odmah `⌘B`, pa „još”, pa `⌘B`: očekuje se `tekst**još**` i kursor
   iza oznaka. Isto s `⌘I`.
5. `⌘B` s fokusom u tražilici, u polju za preimenovanje, na stablu i nakon klika u pregled: dokument
   se ne mijenja. Nakon `⌘B` u terminalu fokus ostaje u terminalu.
6. Izbornik „Oblikovanje” je između „Uređivanje” i „Prozor”, s `⌘B` i `⌘I`.
7. Na Wordovom dokumentu samo za čitanje `⌘B` i `␣b` ne rade ništa i ne javljaju grešku.
8. `~/Library/Application Support/Pisar/user.lua`: opcija iz njega vrijedi nakon ponovnog
   pokretanja Neovima; s namjernom greškom editor se pokreće i pokaže kratku poruku.
9. Nakon ponovnog učitavanja stranice (ako ga ikad izazoveš) terminal se ponovno iscrta.

### Otvoreno nakon faze 5

- Bez hrvatskog rječnika (ne bi se smjelo dogoditi: ugrađen je) pravopis radi samo za engleski.
- `user.lua` i dalje može zamrznuti pokretanje s `vim.fn.input()`, `confirm()`, `getchar()` ili
  beskrajnom petljom; `Start` tada istekne nakon `NvimStartTimeout`.
- `user.lua` koji postavke o kojima Go ovisi promijeni NAKON pokretanja (kasniji autocmd, tajmer) ili
  zamijeni Neovimove funkcije i dalje može utišati Pisarove obavijesti; tada `:w` na Wordovom
  dokumentu ne pokreće izvoz. **Predložena granica, čeka odluku vlasnika**: iznutra se ne može
  zatvoriti jer je `user.lua` proizvoljan kod, ali plan je obećao da `user.lua` „može mijenjati sve
  osim” onoga o čemu Go ovisi.
- Tipke utipkane nakon `⌘B`, a prije nego događaj izbornika stigne u frontend (nekoliko ms), mogu
  proći prije oblikovanja.
- U insert modu oblikovanje je dio tekućeg unosa: `u` vraća cijeli unos, ne samo oznake.
- Blokovski odabir se za podebljano i kurziv odbija; naslov zamjenjuje oznaku liste.
- Valovita crta i izbornik „Oblikovanje” nisu viđeni u prozoru (ručna provjera).
- `init.lua` ima 1525 redaka u jednoj datoteci (oblikovanje i `user.lua` nisu izdvojeni u module).
- `.md` bez dozvole pisanja (Neovimov `readonly`, ne `nomodifiable`): oblikovanje mijenja tekst uz
  Neovimovo upozorenje W10, isto kao tipkanje. Wordovi dokumenti samo za čitanje su `nomodifiable` i
  zaštićeni.
- Pismo bez razmaka između riječi (kineski, japanski): podebljano i kurziv u normal modu ne nalaze
  riječ pod kursorom; visual mod radi.
- **Odluke vlasnika tijekom pregleda (2026-11-12): „Poruke neka budu na hrvatskom” i „i opisi mapiranja
  neka budu na hrvatskom”.** Provedeno: 17 novih ključeva `nvim*` u `strings.json` (7 poruka, 10 opisa
  mapiranja). Jedan put do Neovima: Go složi tekstove iz `uiText` u JSON i preda ih kroz okolinu
  procesa (`PISAR_TEXT`, `nvim.Options.Text`); `init.lua` ih čita na vrhu i nema nijedne vlastite
  rečenice. Bez varijable ili s neispravnim JSON-om mapiranja nemaju opis, a poruka nosi samo
  činjenicu (nema engleskog rezervnog teksta). Poruke nastale pri pokretanju `init.lua` ostavlja kao
  strukturirane podatke (`g:pisar_user_note`), a Go nakon spajanja složi hrvatski tekst. Skraćivanje
  poruke na širinu ide po ćelijama i ne presijeca znak. `Save`, `SaveAll`, `Wipe`, `Reload` za
  otvoreni upit vraćaju `nvim.ErrPromptOpen`, a dijalog pri gašenju za njega pokazuje hrvatski tekst
  (time je zatvoreno ograničenje iz faze 2 „sirova poruka greške pri gašenju”).
  - Ostaje na engleskom: tekstovi samog Neovima i Lue (E-kodovi, tekst greške iz `user.lua`, razlog
    greške oblikovanja), tekstovi grešaka za programera i log.
  - Namjerno izmijenjeni testovi (uspoređivali su engleski tekst; tvrdnje o ponašanju iste): `TestUErr`,
    `TestUSpell`, `TestUEi`, `TestUOne`, `TestUOpts` (`writing_user_test.go`); `TestSwap`, `TestSaveAsk`
    (`nvim_prompt_test.go`: sada `errors.Is(err, ErrPromptOpen)`). Novi: `TestFmtMsg`, `TestUMsg` (80,
    40 i 20 stupaca), `TestUFail`, `TestUMap` (23 mapiranja), `TestUNoEn` (zaštita od povratka
    engleskih rečenica i opisa), `TestUITextNvim`, `TestEdText`.
  - `EditorService.Save` (`⌘S`) uz otvoren upit i dalje vraća opći kod greške (frontend ga samo zapiše).
- **Incident tijekom pregleda (2026-11-12, 14:33):** `plan-reviewer` je jednom pokrenuo
  `nvim --headless -u NONE` bez `-i NONE` i bez preusmjerenog homea (da nađe `VIMRUNTIME`), pa je
  Neovim prepisao vlasnikovu `~/.local/state/nvim/shada/main.shada` (povijest i oznake vlasnikovog
  Neovima; konfiguracija se nije učitala). Datoteka postoji (13 882 bajta); sadržaj nije provjeravan
  ni vraćan. Prijavljeno vlasniku. Svi kasniji pokusi išli su s preusmjerenim `XDG_*` i `-i NONE`; u
  briefove je dodano izričito pravilo.

### Izvan opsega

Drugi jezici i izbor jezika u sučelju, gramatika, automatsko ispravljanje, sinonimi, rastavljanje
riječi, prečaci za poveznicu, sliku, tablicu i citat, WYSIWYG, način bez ometanja, brojač riječi,
postavke u sučelju aplikacije, dodaci za Neovim, stvaranje ili uređivanje `user.lua` iz aplikacije,
pakiranje, tamna tema.

### Kriteriji prihvaćanja

- [x] `init.lua`: prelamanje po riječima, kretanje po prikazanim recima, bez brojeva redaka za prozu (prekidač) — dokaz 1
- [x] provjera pravopisa hr + en (hrvatski rječnik: Vimov službeni, LGPL, isporučen s aplikacijom) — dokaz 2
- [x] prečaci za naslov, bold, italic, listu (umeću Markdown oznake) — dokaz 3
- [x] neobavezni `user.lua` učitava se nakon ugrađene konfiguracije — dokaz 4

## Faza 6 — Pakiranje

Effort high. Plan faze odobren 2026-11-12 („kreni” na plan s četiri preporuke, bez izmjena).

### Odluke vlasnika (2026-11-12)

Vlasnik je plan odobrio jednom riječju, pa su sve četiri preporuke iz plana prihvaćene kako su napisane:

- Ikona: glavna sesija crta jednostavan znak iz boja tokena (list s recima teksta i blok-kursorom u boji
  akcenta); izgled vlasnik odobrava kao **[R]**.
- Potpis: stabilan identitet kroz `$SIGN_IDENTITY`, kao u Tragu („Trag Dev” može poslužiti); nikad ad-hoc
  za paket koji se koristi.
- **Granica oko `user.lua` iz faze 5 je prihvaćena:** zaštita pokriva učitavanje i kraj pokretanja; ono što
  `user.lua` promijeni kasnije (autocmd, tajmer, zamjena Neovimovih funkcija) vlasnikova je odgovornost.
- Greška pri pokretanju prikazuje nativni dijalog umjesto tihog izlaska (dodatak izvan kriterija plana).

### Ovisnosti (odobrene)

- Nema novih Go ni npm ovisnosti.

### Izmjereno prije plana (macOS 27.2)

- Grafičke aplikacije dobivaju PATH `/usr/bin:/bin:/usr/sbin:/sbin` (`launchctl getenv PATH` je prazan);
  `nvim` i `pandoc` su u `/opt/homebrew/bin`, pa ih aplikacija pokrenuta iz Findera ne bi našla
  (`env -i PATH=/usr/bin:/bin:/usr/sbin:/sbin sh -c 'command -v nvim pandoc'` → ništa, exit 1).
- U tom okruženju locale je `C`. `man pbcopy`: „pbcopy and pbpaste use locale environment variables to
  determine the encoding”; Neovimov `"+y` ide kroz njih (`init.lua` ne postavlja vlastiti provider).
  Ponašanje s č ć š ž đ NIJE mjereno (diralo bi sistemski clipboard) — ide u ručnu provjeru.
- pandoc 3.13 u golom okruženju: md → html, md → docx → md s imenom datoteke `čćšžđ.md` rade ispravno.
- Predložak za pakiranje je netaknut: `Info.plist` „My Product” / `com.example.pisar`, Wailsova ikona
  (`appicon.png`, `appicon.icon`, `icons.icns`, `Assets.car`), paket `bin/pisar.app`, potpis ad-hoc.
- Jedan valjan identitet za potpisivanje: „Trag Dev”. Trag: `codesign --force --deep --options runtime
  --entitlements … --sign "$SIGN_IDENTITY"`; entitlement mu treba samo zbog wazera (Pisar ga nema).
- `open` ima `--env`, `--stdout`, `--stderr`, `-n`, `-W`; postoje `actool`, `iconutil`, `sips`, `qlmanage`;
  `locale -a` ima `hr_HR.UTF-8` i `en_US.UTF-8`. Wailsov izbornik „About” zove standardni macOS prozor
  (`orderFrontStandardAboutPanel:`), koji naziv, verziju i ikonu čita iz paketa.

### Ugovor Go ↔ frontend

```go
type ToolStatus struct { Name string; Found bool } // Name: "nvim" | "pandoc"
EditorService.Tools(ctx) []ToolStatus               // pri svakom pozivu traži iznova; uvijek oba, tim redom
```

Okolina: `PISAR_TOOL_DIRS` (popis mapa odvojen dvotočkom) zamjenjuje ugrađeni popis dodatnih mapa za
alate; prazna vrijednost znači „bez dodatnih mapa”. Samo za provjere, kao `PISAR_HOME`.

### Koraci

- [x] 1. Okruženje procesa: `internal/config/environment.go` — `ToolDirs` (ugrađeno `/opt/homebrew/bin`,
      `/usr/local/bin`; `PISAR_TOOL_DIRS` ih zamjenjuje, prazno = bez dodatnih), `ExtendPath` (čista; dodaje
      na KRAJ, bez dvostrukih, postojeći unosi netaknuti), `PrepareEnvironment` (PATH + `LC_CTYPE=en_US.UTF-8`
      kad su `LC_ALL`, `LC_CTYPE` i `LANG` prazni); `main.go` je zove prvu. 5 testova, među njima
      `TestPrepareEnvironmentMakesToolFindable` (`LookPath` prije ne nalazi, poslije nalazi).
      - Aplikacija mijenja VLASTITU okolinu, pa to vrijedi za svaki `LookPath` i za Neovim; paketi
        `nvim`, `convert`, `preview` nisu dirani.
      - Izmjereno: `env -i X=1 locale charmap` → `US-ASCII`; s `LC_CTYPE=en_US.UTF-8` → `UTF-8`.
        Stvarno kodiranje u `pbcopy` nije mjereno (ručna provjera).
      - `gofmt -l .` ispisuje tri datoteke u `build/ios/` (Wailsov predložak, postojeće stanje, nije dirano).
- [x] 2. Provjera alata (Go): `editortools.go` — `EditorService.Tools` (`exec.LookPath` za oba alata pri svakom
      pozivu, ništa se ne pamti i ništa se ne pokreće; `Name` je stabilna oznaka, ne ime iz konfiguracije;
      uz `PISAR_LOG=debug` redak `editor: Tools nvim=… pandoc=…`). `main.go`: gradnja je izdvojena u
      `build(text)`, a greška prije `app.Run()` ide kroz JEDNO mjesto (`reportStartFailure` →
      `startFailureDialog` → `showFatal`); `ErrNewerVersion` zadržava svoj naslov i tekst, ostalo dobiva
      `startFailedTitle` / `startFailed` (`{reason}`). Testovi: `TestTools` (7 slučajeva), `TestToolsFolder`,
      `TestToolsLooksAgain`, `TestStartFailureDialog`.
      - Greška iz `loadUIText` i iz samog `app.Run()` ostaje bez dijaloga (nema teksta, odnosno aplikacija
        je već radila). Sam dijalog nije viđen (ručna provjera nije moguća bez izazivanja greške).
      - `coder` je jednom vidio pad `TestEdFmtOrder` (pravi Neovim, ovisi o vremenu); ponovljeno 3 od 3
        prolazi, kod glavne sesije `task test` i `go test -race .` prolaze. Nije istraženo.
- [x] 3. Zaslon u frontendu: `lib/toolsState.ts` (`missingTools`, `toolsNotice`; čisto, pod vitestom),
      `editorController` zove `Tools()` jednom pri pokretanju i na „Provjeri ponovno”; `EmptyState` u
      praznom stanju editora pokazuje naslov, redak i naredbu za svaki alat koji nedostaje i gumb
      (`role="alert"`, gumb stiliziran kao u `Toolbar.svelte`). Ne blokira: stablo radi, a bez pandoca se
      `.md` i `.txt` otvaraju (terminal zamijeni obavijest). Stanje `failed` ima prednost. Vitest 834.
      - Neuspio poziv `Tools()` se zapiše u konzolu i vrijedi kao „ništa ne nedostaje”.
      - Izgled obavijesti nije viđen u prozoru (ručna provjera).
- [x] 4. Naziv, identifikator, verzija: `build/config.yml`, `Info.plist`, `Info.dev.plist` uređeni ručno i usklađeni
      („Pisar”, `com.odinwolf.pisar`, 0.1.0). Paket je `bin/Pisar.app` (nova varijabla `BUNDLE_NAME` u
      korijenskom `Taskfile.yml`; koriste je `create:app:bundle` i `codesign:adhoc`); binarna datoteka ostaje
      `bin/pisar` i `Contents/MacOS/pisar`.
      - `wails3 task common:update:build-assets` namjerno NIJE pokrenut (prepisuje sredstva svih platformi).
      - Wailsovi `darwin:sign` i `darwin:sign:notarize` i DMG zadaci i dalje računaju na `pisar.app` (nisu dirani).
- [x] 5. Ikona: `build/appicon.svg` (bijeli list s recima teksta i blok-kursorom na podlozi u boji akcenta; crtala
      glavna sesija) → `task icon` (`sips` → `build/appicon.png` 1024×1024 → `common:generate:icons` →
      `build/darwin/icons.icns`, `build/windows/icon.ico`). Ponovljen `task icon` daje iste bajtove.
      - **Odstupanje od plana, odluka vlasnika 2026-11-12 („Samo icns”):** `Assets.car` se ne može izgraditi,
        jer `actool` traži puni Xcode, a na stroju su samo Command Line Tools (generator to prešuti i ostavi
        staru datoteku s Wailsovom ikonom). Zato su `build/darwin/Assets.car` i `build/appicon.icon/` obrisani,
        a `CFBundleIconName` maknut iz oba plista; paket koristi samo `icons.icns`, kao Trag. Nema Liquid
        Glass varijante ikone.
      - Boje u SVG-u su prepisane iz `tokens.ts` (`accent`, `bg.editor`, `text`, `text.muted`,
        `border.strong`); to je sredstvo za gradnju izvan `frontend/src`, pa ga čuvari boja ne gledaju.
      - Izgled ikone u Docku i Finderu nije viđen (ručna provjera).
- [x] 6. Potpisivanje: `task sign` u korijenskom `Taskfile.yml` (`deps: [package]`; `xattr -cr`, `codesign --force
      --deep --options runtime --sign "$SIGN_IDENTITY"`, `codesign --verify --deep --strict`). Ime identiteta
      nije u `Taskfile.yml` ni u skriptama (dolazi samo iz okoline; spominje ga ovaj zapis). Bez entitlements datoteke: potpisana aplikacija pod hardened runtimeom pokreće
      Neovim i pandoc (dokazuje `smoke-app.sh`).
      - Bez `SIGN_IDENTITY` zadatak pada s jasnom porukom, ali tek NAKON `package` (Task izvršava `deps`
        prije `preconditions`), pa ostane ad-hoc potpisan paket.
      - `man codesign` označava `--deep` zastarjelim za potpisivanje; ostavljen je radi istog recepta kao u Tragu.
- [x] 7. Dimna provjera paketa: `scripts/smoke-app.sh` (24 provjere). A: `.docx` s č ć š ž đ na naredbenom retku →
      `Tools nvim=true pandoc=true`, Neovim prikazuje pretvoreni tekst, PATH u Neovimu je točno
      `/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin:/usr/local/bin`, `LC_CTYPE=en_US.UTF-8`, radna mapa `/`,
      nakon SIGTERM nema procesa ni socketa, `.docx` bajt-identičan. B (`PISAR_TOOL_DIRS=`): `Tools
      nvim=false pandoc=false`, aplikacija živa i nakon 3 s, bez Neovima.
      - **Izmjereno, odstupanje od plana:** `open` aplikaciji PRENOSI okolinu procesa koji ga je pozvao, pa
        sam `open` ne daje Finderovo okruženje. Skripta zato zove `/usr/bin/open` pod `env -i` (PATH sustava,
        `HOME`, `USER`, `LOGNAME`, `TMPDIR`). To je emulacija; pravi dvoklik ostaje u ručnoj provjeri.
      - `--env IME=` stiže kao postavljena-a-prazna; `--stderr` radi; PID se nalazi usporedbom popisa prije i poslije.
      - Mutacijska provjera (`coder`): A s praznim `PISAR_TOOL_DIRS` → 2 pada, exit 1.
      - Prolazi i nad ad-hoc i nad potpisanim paketom (potpisani: 4 pokretanja kod `coder`-a, 2 kod glavne sesije).
      - **Incident (2026-11-12, 18:41):** `coder` je u pokusu jednom pokrenuo `nvim --server ''` (prazan socket);
        vlasnikova `~/.local/state/nvim/shada/main.shada` ima vrijeme izmjene 18:41 i 14 111 bajtova (prije
        13 882), pa je vjerojatno prepisana i drugi put. Sadržaj nije provjeravan ni vraćan. I mapa
        `~/.local/state/nvim/swap` ima vrijeme izmjene 18:41 (swap datoteka je nastala i nestala; ostataka nema). `remote()` u obje dimne skripte od kruga 1 pregleda odbija prazan socket i
        klijenta pokreće s `-i NONE` i `XDG_*` u privremenoj mapi.
- [x] 8. Kraj faze: dokazi po kriterijima, popis za ručnu provjeru, `plan-reviewer` (krug 1 FAIL, krug 2 **PASS**),
      sažetak vlasniku

### Pregled plana — krug 1 (2026-11-12): FAIL, jedan bloker; ispravljeno

Prvi pokušaj pregleda prekinut je bez nalaza (sesija je završila); ponovljen je ispočetka. Reviewer je sam
pokrenuo sve provjere (prolaze, nad ad-hoc i nad potpisanim paketom) i potvrdio brojeve u zapisu.

1. **Bloker:** zapis je tvrdio da `smoke-app.sh` odbija prazan socket, a `remote()` to nije radio (prazan
   socket se nije događao samo zato što ga pozivatelji prije provjere). Klijent `nvim --server` uz to nije
   imao `-i NONE` ni preusmjerene `XDG_*`, pa mu je `nvim.log` mogao završiti u vlasnikovom
   `~/.local/state/nvim/`. **Ispravak (glavna sesija):** `remote()` odbija prazan socket i klijenta pokreće
   s `-i NONE` i sva četiri `XDG_*` u privremenoj mapi skripte — u `smoke-app.sh` i, izvan diffa faze, u
   `smoke.sh` (isti rizik). Nakon obje skripte vlasnikovi `main.shada` i `nvim.log` imaju staro vrijeme izmjene.
2. Tvrdnja „ime identiteta nije u repozitoriju” nije bila točna (bilo je u komentaru `smoke-app.sh`):
   komentar sada ima `<identity>`, a tvrdnja je sužena.
3. Zastario redak „Zadnja izmjena” — ispravljen.
4. Obavijest o alatima mogla je ostati zastarjela: alat instaliran bez klika na „Provjeri ponovno”, dokument
   otvoren, pa nakon `:q` opet „Neovim nije pronađen”. **Ispravak (glavna sesija, 6 redaka u
   `EditorPane.svelte`):** provjera se ponavlja svaki put kad se vrati prazno stanje.

Napomene bez izmjene: u pokretanju B dimne skripte dokument je na naredbenom retku, pa frontend pokazuje
stanje `failed` s `errorNvimMissing` (i ono nosi naredbu za instalaciju), a ne obavijest s gumbom — obavijest
ostaje u ručnoj provjeri; dijalog o neuspjelom pokretanju umeće engleski tekst greške u hrvatsku rečenicu
(isti obrazac kao `nvimUserLuaError`); `task package` traži `wails3` na PATH-u (`~/go/bin`; postojeće stanje).

### Pregled plana — krug 2 (2026-11-12): PASS

Reviewer je potvrdio sva četiri ispravka (prazan socket i `XDG_*` pokusom s lažnim `nvim`-om; novi efekt u
`EditorPane` bez petlje: 1 poziv `Tools` pri pokretanju, 2 nakon `:qa!`), sam ponovio sve provjere i pratio
vlasnikove Neovim datoteke kroz sva pokretanja (nepromijenjene). Dvije napomene, obje unesene u zapis: mapa
`swap` u incidentu od 18:41 i potpunija naredba u ručnoj provjeri 6.

### Dokazi po kriterijima (2026-11-12)

Naredbe iz korijena repozitorija, pokrenute nakon zadnje izmjene (s `~/go/bin` na PATH-u i `SIGN_IDENTITY` u okolini).

- `task test` → exit 0, svih 8 Go paketa `ok`, vitest `Tests 834 passed`; `go test -count=1 -race ./...` → svih
  8 `ok` (prije ispravaka kruga 1; nakon njih Go kod nije mijenjan); `task lint` → `0 issues.`, svelte-check
  `239 FILES 0 ERRORS 0 WARNINGS`; `bash scripts/smoke.sh` → `smoke: all checks passed`;
  `bash scripts/shutdown.sh` → `shutdown: all checks passed`.

1. `.app` se gradi i pokreće izvan razvojnog okruženja: `task package` → exit 0, `bin/Pisar.app` s
   `Contents/MacOS/pisar`; `bash scripts/smoke-app.sh` → `smoke-app: all checks passed` (24 provjere; nad
   ad-hoc i nad potpisanim paketom). Pokretanje je kroz `open` pod `env -i` (PATH sustava, bez locale
   varijabli, radna mapa `/`) — emulacija Findera; pravi dvoklik je u ručnoj provjeri.
2. Provjera `nvim` i `pandoc` pri pokretanju, s jasnom porukom: `TestTools`, `TestToolsFolder`,
   `TestToolsLooksAgain`; `TestPrepareEnvironmentMakesToolFindable`; vitest `toolsState.test.ts`;
   `smoke-app.sh` A (`Tools nvim=true pandoc=true` iz golog okruženja, `.docx` pretvoren) i B (`Tools
   nvim=false pandoc=false`, aplikacija živa). Izgled obavijesti je u ručnoj provjeri.
3. Ikona, naziv, verzija; potpisivanje: `plutil -lint bin/Pisar.app/Contents/Info.plist` → `OK`; `PlistBuddy` →
   `CFBundleName = Pisar`, `CFBundleIdentifier = com.odinwolf.pisar`, `CFBundleShortVersionString = 0.1.0`,
   `CFBundleVersion = 0.1.0`, `CFBundleIconFile = icons`; `Resources` sadrži `icons.icns` (iz
   `build/appicon.svg`, 1024×1024); `task sign` → exit 0; `codesign --verify --deep --strict bin/Pisar.app` →
   exit 0; `codesign -dv --verbose=2` → `Authority=Trag Dev`, `flags=0x10000(runtime)`, bez `Signature=adhoc`.

### Ručna provjera — za vlasnika (agent ovo ne može provjeriti)

Paket: `SIGN_IDENTITY="…" task sign` → `bin/Pisar.app` (po želji ga povuci u `/Applications`).

1. Dvoklik u Finderu: aplikacija se otvara, stablo i projekti su tu, dokument se otvara u Neovimu.
2. Ikona u Finderu i Docku je Pisarova (bijeli list s recima i plavim kursorom na plavoj podlozi), ne Wailsova;
   sviđa li ti se — **[R]** odluka o izgledu.
3. Izbornik aplikacije → „About Pisar”: naziv „Pisar”, verzija 0.1.0, ista ikona.
4. `"+y` u Neovimu na tekstu s č ć š ž đ pa `⌘V` u drugoj aplikaciji; obrnuto: kopiraj takav tekst drugdje pa
   `"+p`. Znakovi moraju ostati ispravni (ovo ovisi o `LC_CTYPE`, koji nije mjeren sa stvarnim clipboardom).
5. `.docx` iz mape u Documents: macOS pita za pristup mapi JEDNOM; nakon ponovnog `task sign` i pokretanja ne
   pita opet.
6. Obavijest o alatu koji nedostaje: `open --env PISAR_TOOL_DIRS= bin/Pisar.app` iz terminala s golim PATH-om
   (iz korijena repozitorija: `env -i HOME="$HOME" USER="$USER" TMPDIR="$TMPDIR" PATH=/usr/bin:/bin:/usr/sbin:/sbin
   /usr/bin/open -n --env PISAR_TOOL_DIRS= bin/Pisar.app`):
   u praznom editoru naslov, dva retka s naredbama `brew install …` i gumb „Provjeri ponovno”; stablo radi.

### Otvoreno nakon faze 6

- Vlasnikova `~/.local/state/nvim/shada/main.shada` vjerojatno je prepisana i drugi put (18:41); nije vraćana.
- `LC_CTYPE=en_US.UTF-8` je postavljen prema `man pbcopy` i `locale charmap`; učinak na clipboard nije mjeren.
- Dijalog o neuspjelom pokretanju nije viđen; tekst greške u njemu je engleski.
- Nema `Assets.car` (Liquid Glass ikone) dok nema punog Xcodea.
- Wailsovi `darwin:sign`, `darwin:sign:notarize` i DMG zadaci računaju na `pisar.app` i ne rade s `Pisar.app`.
- `task sign` bez `SIGN_IDENTITY` ostavlja ad-hoc potpisan paket (pada tek nakon gradnje).
- Najmanja verzija Neovima i pandoca se ne provjerava; alat u mapi izvan PATH-a i dviju Homebrewovih nije nađen.

### Izvan opsega

DMG, notarizacija, Developer ID, univerzalna binarna datoteka, kopiranje u `/Applications`, otvaranje
dokumenata dvoklikom iz Findera i pridruživanje vrsta datoteka, provjera najmanje verzije Neovima i
pandoca, hrvatske stavke Wailsovog izbornika aplikacije, postavka putanje alata u `config.json`, brisanje
nekorištenih platformi iz `build/`, teme.

### Kriteriji prihvaćanja

Automatski dio je dokazan (odjeljak „Dokazi po kriterijima”); kućice ostaju prazne dok vlasnik ne prođe ručnu
provjeru (dvoklik iz Findera, izgled ikone i obavijesti, clipboard).

- [x] `.app` se gradi i pokreće izvan razvojnog okruženja — dokaz 1; dvoklik iz Findera čeka vlasnika — vlasnik potvrdio ručnu provjeru 2026-11-13
- [x] provjera `nvim` i `pandoc` pri pokretanju, s jasnom porukom — dokaz 2; izgled obavijesti čeka vlasnika — vlasnik potvrdio ručnu provjeru 2026-11-13
- [x] ikona, naziv, verzija; potpisivanje za lokalnu upotrebu — dokaz 3; izgled ikone čeka vlasnika — vlasnik potvrdio ručnu provjeru 2026-11-13

## Faza 7 — Teme

Effort high. Plan faza 7 i 8 odobren 2026-11-12 (jedan plan, dvije faze; STOP nakon faze 7). Faza 6 i
dalje čeka vlasnikovu ručnu provjeru; faze 7 i 8 o njoj ne ovise.

### Odluke vlasnika (2026-11-12)

- Tri teme: `pisar` (postojeća svijetla), `catppuccin-mocha`, `dracula`. Biraju se u meniju „Prikaz → Tema”,
  promjena vrijedi odmah (sučelje, xterm.js, Neovim, pregled), izbor se pamti u `config.json`.
- Pregled dokumenta prati temu (nema stalno bijelog papira).
- Dracula odstupa od službene palete na četiri mjesta zbog kontrasta 4,5:1 iz `DESIGN.md`: `text.muted`
  `#A1AAC8`, `text.linenr` `#8995BB`, `bg.cursorline` `#21222C`, `bg.selected` `#343746`. Catppuccin Mocha
  ostaje unutar službene palete (akcent lavender). Testovi kontrasta se ne slabe; vrte se nad svim temama.
- `config.json` ide na verziju 2; starija verzija Pisara takvu datoteku odbija i ne dira.
- Rad ide redom, bez paralelnih agenata.

### Ovisnosti (odobrene)

- Nema novih Go ni npm ovisnosti.

### Izmjereno prije plana

- Wails v3.1.0 (`go doc`, izvor u module cacheu): `(*Menu).AddRadio(label, checked)`, susjedne radio stavke
  čine grupu (`processRadioGroups`), `(*MenuItem).SetChecked` gasi ostale u grupi;
  `(*WebviewWindow).SetBackgroundColour(RGBA)` ide kroz `InvokeSync` i boji samo NSWindow; `parseKey`
  prihvaća `CmdOrCtrl+/`.
- xterm.js 6.0.0 (`typings/xterm.d.ts`): `terminal.options.theme = {...}` mijenja temu u radu; mora biti
  novi objekt.
- Neovim 0.12.6 (`nvim --clean --headless`): ponovno izvršen tekst colorschemea s `background = "dark"`
  mijenja `&background`, zadržava `g:colors_name = "pisar"` i zamjenjuje `Normal`.
- Kontrast (ista formula kao `theme/contrast.ts`): Dracula komentar `#6272A4` na `#282A36` = 3,03; palete iz
  plana prolaze sve parove iz postojećih testova (najmanje: Mocha 5,10, Dracula 4,53).
- NIJE provjereno: službene heksadecimalne vrijednosti obiju paleta (provjeriti prije koraka 3), sve
  vizualno (bljesak pri pokretanju, kvačice u meniju), `:colorscheme pisar` nad prepisanom datotekom u
  pokrenutom Neovimu (pokriva test koraka 5).

### Ugovor Go ↔ frontend

```go
// config.json v2: { "version": 2, "theme": "dracula", "projects": [...] }
config.Themes() []string; config.DefaultTheme; config.ErrUnknownTheme
(*config.Store).Theme() string
(*config.Store).SetTheme(id string) error
config.ThemeWindowBackground(id string)            // boja bg.app za nativni prozor

SettingsService.Theme(ctx) string                  // novi vezani servis
EditorService.SetColorscheme(ctx, colorscheme string) error
(*nvim.Process).Colorscheme(ctx) error             // i ista metoda na term.Session
```

Događaj `theme:changed` (payload: ID teme). URL prozora `/?theme=<id>`. `colorscheme.go` dobiva jedan novi
fiksni redak zaglavlja: `vim.o.background = "dark"`.

v1 se učitava s temom `pisar` i ne prepisuje pri otvaranju; prvi zapis piše v2. Nepoznat ID teme u v2 →
`pisar` uz upozorenje u logu, datoteka ostaje. v1 s poljem `theme` je oštećena datoteka, kao i dosad.

### Koraci

- [x] 1. Konfiguracija v2 s temom (`internal/config`): `fileVersion` 2, ID-evi tema i `Themes()`, `Store.Theme` /
      `SetTheme`; v1 ima vlastitu strogu strukturu (`fileFormatV1`), pa je v1 s poljem `theme` i dalje
      oštećena; 12 novih testova (`TestTheme*`), `TestNewerVersion` sada koristi verziju 3 (tvrdnje iste).
      - Nepoznata tema u v2 ne seli datoteku, ali je prvi sljedeći zapis prepisuje s `pisar`.
      - Prazan `"theme": ""` vrijedi kao izostavljeno polje (bez upozorenja). Tekst upozorenja u logu
        nema test.
      - `ThemeWindowBackground` dolazi u koraku 7, zajedno s testom koji ga veže uz `tokens.ts`.
- [x] 2. Registar tema s jednom temom: `tokens.ts` (`themeIds`, `themes`, `Palette`, `Theme`, `defaultTheme`; izvoz
      `colors` je uklonjen), `cssVariables(palette)`, `applyTheme(root, palette)`, `terminalTheme(palette)`,
      `nvimColorscheme(theme)`, `previewFrameDocument({…, palette})`, novi `theme/active.ts`; `main.ts` čita
      `?theme=`. Testovi vezani uz paletu idu kroz `describe.each(themeIds)` s istim tvrdnjama; vitest 863.
      - `pisar` mora ostati prva paleta u `tokens.ts` (Go `tokenColor` i `smoke.sh` uzimaju prvi pogodak);
        čuva to novi test. `testdata/colorscheme/pisar.lua` je bajt-identičan.
      - Izvan popisa datoteka iz plana dirnut je `theme/usedVariables.test.ts` (samo argument palete u
        pozivu `cssVariables`; greška u briefu glavne sesije).
      - Komponente (`Terminal`, `Preview`) i `main.ts` provjereni su samo tipovima; aplikacija nije pokretana.
- [x] 3. Palete Catppuccin Mocha i Dracula u `tokens.ts` (`themeIds` ima tri ID-a), `DESIGN.md` ima po jednu
      tablicu od 17 redaka za svaku temu (`### Tema: … (`id`, …)`), `tokens.test.ts` ih čita po temi; nove
      snimke `testdata/colorscheme/catppuccin-mocha.lua` i `dracula.lua` (razlikuju se od `pisar.lua` samo
      bojama i retkom `background`); vitest 1257. Nijedan postojeći test kontrasta nije pao ni mijenjan.
      - Službene vrijednosti provjerene 2026-11-12 (catppuccin/palette `palette.json`, draculatheme.com/spec).
        Kod Dracule su `#21222C`, `#191A21` i `#343746` službene nijanse sučelja (Background Dark / Darker /
        Light); izvan specifikacije su samo `text.muted` i `text.linenr` (posvijetljeni Comment), a
        `bg.cursorline` i `bg.selected` koriste službenu nijansu na drugom mjestu od službenog.
      - Novi eksplicitni par u `contrast.test.ts`: `text.muted` na `bg.cursorline` (prolazi u sve tri teme).
      - Go validator nove snimke još ne prihvaća (korak 4).
- [x] 4. Validator colorschemea prihvaća `dark`: jedan novi fiksni redak u `headerLines`; `TestEdSampleAccepted`
      ide kroz sve `testdata/colorscheme/*.lua` (najmanje 3), `TestEdBadScheme` ima 5 novih odbijenih
      oblika retka `background`.
      - Zapažanje (postojeće stanje, nije mijenjano): validator ne broji ni ne redâ retke zaglavlja, pa bi
        datoteka s oba retka (`light` i `dark`) prošla. Oba su retka fiksna i bezopasna.
- [x] 5. `nvim.Process.Colorscheme(ctx)` (novi `internal/nvim/colorscheme.go`) i `term.Session.Colorscheme(ctx)`:
      `vim.cmd.colorscheme(<ime kao argument>)` pa `:mode` kao `Redraw`. Testovi s pravim Neovimom:
      `TestColorschemeSwap` (pisar → dracula → pisar → mocha), `TestColorschemePrompt`, `TestColorschemeNone`,
      `TestColorschemeEnd`; u `internal/term` `TestScheme` i `TestNoProc`.
      - Izmjereno (Neovim 0.12.6): `:colorscheme pisar` ponovno učitava datoteku i kad je `g:colors_name`
        već `pisar`. U hit-enter promptu poziv se vraća s `context.DeadlineExceeded`, ostaje u redu u
        Neovimu i primijeni se kad se prompt zatvori. E185 za datoteku koje nema; Neovim radi dalje.
      - Nuspojava: `:mode` briše tekst u retku za poruke (kao i `Redraw`).
      - Nije provjereno: prompt „-- More --” (dugi izlaz); njega `Enter` ne zatvara, treba `q` ili `Esc`.
      - `internal/nvim` sada uvozi `internal/config` (zbog `NvimColorscheme`).
      - Odstupanje od plana: plan navodi izmjenu `internal/nvim/nvim.go`; metoda je u novoj datoteci
        `internal/nvim/colorscheme.go` (kao `format.go` i `preview.go`), a `nvim.go` nije diran.
- [x] 6. `EditorService.SetColorscheme(ctx, colorscheme)`: provjera kao u `Start`, atomski zapis pod `startMu`,
      zatim `session.Colorscheme` ako Neovim radi. `DeadlineExceeded` (prompt) i `ErrNotRunning` vraćaju nil
      uz zapis u logu. Testovi `TestEdSetScheme*` (6), među njima prompt (`CommandTimeout` 400 ms) i utrka
      sa `Start`; nova pomoćna `tokenColorIn(t, tema, token)` čita blok palete iz `tokens.ts`.
      - Odstupanje od briefa (prihvaćeno): `startMu` se drži samo za zapis datoteke, ne i za RPC, da prompt
        ne zaključa `Start` do `CommandTimeout`.
      - Neispravan colorscheme stiže frontendu s kodom `internal` (kao i kod `Start`; `codeOf` nema granu).
      - Prompt test prepoznaje prompt po tekstu „ENTER” u izlazu PTY-ja (10 od 10 uz `-race`).
      - Odstupanje od plana: plan kaže „`tokenColor()` dobiva neobavezan opseg teme”; `tokenColor` je ostao
        isti, a opseg teme čita zasebna `tokenColorIn` (postojeći pozivi nisu dirani).
- [x] 7. `settingsservice.go` (`SettingsService.Theme`, neizvezeni `setTheme`: store → boja prozora → događaj),
      `menu.go` (`themeItems`, izbornik „Prikaz” između „Oblikovanje” i Window, podizbornik „Tema” s tri
      radio stavke), `main.go` (`theme:changed`, `BackgroundColour` i `URL` `/?theme=<id>` iz zapamćene
      teme), `config.ThemeWindowBackground`, pet novih tekstova. Testovi `TestSettings*` (8),
      `TestMenuThemeItems*` (2), `TestThemeWindowBackground`.
      - Boja `bg.app` je namjerno ponovljena u Go-u; `TestSettingsTokens` pada ako se ID-evi tema ili te
        tri boje raziđu s `tokens.ts`.
      - Potvrđeno u izvoru Wailsa v3.1.0: radio grupe rade i u podizborniku (`processRadioGroups`
        rekurzira), `handleClick` pomiče kvačicu prije callbacka, `SetChecked` i `SetBackgroundColour` smiju
        se zvati iz bilo koje goroutine, poslužitelj datoteka gleda samo putanju (query ne smeta;
        `TestSettingsWindowURL` s `httptest`).
      - Klikovi na temu idu jedan po jedan (mutex); nakon svakog se kvačica postavlja na zadnju stvarno
        primijenjenu temu, pa neuspjeli zapis vraća kvačicu.
      - Dimna provjera agenta bez klikanja: aplikacija se diže s praznim homeom i s v2 `"theme":"dracula"`.
        Klik, boja prozora i kvačica NISU viđeni (ručna provjera).
- [x] 8. Promjena uživo: `lib/themeController.svelte.ts` (na `theme:changed`: `setActiveTheme`, `applyTheme`,
      reaktivni `id`, slanje colorschemea), `lib/themeSender.ts` (jedan `SetColorscheme` u letu, pamti se
      samo najnoviji zahtjev; 8 testova), `Terminal.svelte` (`term.options.theme` = novi objekt; `resend`
      ako se tema promijenila dok se Neovim pokretao), `Preview.svelte` (`applyTheme` na `<html>` okvira,
      `srcdoc` ostaje), `main.ts` (tema iz URL-a sinkrono, potvrda kroz `SettingsService.Theme()` usporedno
      s fontovima; `startTheme`); vitest 1276.
      - Dimna provjera agenta bez klikanja: home s `"theme":"dracula"` → log `method=Theme theme=dracula`,
        zatim `Start`; Neovim preko socketa javlja `dark pisar`.
      - Neuspjelo slanje colorschemea se samo logira (sučelje je tada u novoj temi, Neovim u staroj).
      - Odstupanje od plana: plan za „vrijedi zadnji” navodi `lib/latest.ts`; on samo odbacuje zakašnjele
        ODGOVORE i ne može poredati slanja (Wails svaki poziv obrađuje u vlastitoj goroutini), pa je uveden
        novi `lib/themeSender.ts`, a `latest.ts` se ovdje ne koristi.
      - Odstupanje od plana: izvan popisa datoteka dirnut je `components/EditorPane.svelte` (samo
        prosljeđuje `theme` u `Terminal`). `theme/active.ts` je dobio i `startTheme`.
      - Ispravak glavne sesije nakon kruga 1 pregleda (oko 15 redaka, bez testa jer kontroler nema
        testove): `ThemeController.start()` nakon pretplate još jednom pita `SettingsService.Theme()`, da se
        ne izgubi tema izabrana između potvrde u `main.ts` i pretplate na `theme:changed`. Nakon kruga 2:
        taj se odgovor odbacuje ako je u međuvremenu stigao događaj ili je pretplata uklonjena (događaj je
        uvijek barem jednako nov kao ono što je poziv mogao pročitati).
      - NIJE viđeno (ručna provjera): promjena na klik u meniju, boje terminala i pregleda nakon promjene,
        skrol pregleda, promjena teme usred pokretanja Neovima. Komponente i kontroler nemaju testove.
- [x] 9. Dimna provjera i dokumenti: `scripts/smoke.sh` ima još jedno pokretanje (v2 `config.json` s
      `"theme": "dracula"`; pomoćna `theme_token <paleta> <token>`), prvi prolaz dodatno provjerava
      `&background = light`; komentar uz `vim.o.background` u `nvim/init.lua`; `DESIGN.md` (tokeni se
      primjenjuju pri pokretanju i pri promjeni teme, stranica pregleda je `bg.editor`).
      - Negativna proba (`coder`): s `"theme": "pisar"` u novom odjeljku skripta pada na tri provjere.
      - Promjena teme klikom u meniju se u skripti ne može dokazati (bez tipki i klikova).
      - `PROMPT.md` i dalje u sažetku dizajna kaže „Svijetla tema”; vlasnikova datoteka, nije dirana.
      - `plan-reviewer`: vidi „Pregled plana” dolje.

### Izvan opsega

Praćenje izgleda macOS-a, vlastite teme korisnika, prozor postavki, zasebne ANSI palete po temi, nove
sintaksne boje izvan 17 tokena, ponovna primjena highlighta iz `user.lua` nakon promjene teme, sve iz
faze 8 (which-key, pregled prečaca, otvaranje `user.lua`).

### Dokazi po kriterijima (2026-11-12)

Naredbe iz korijena repozitorija, sve ponovno pokrenute nakon zadnje izmjene koda (ispravak iz kruga 2
pregleda, commit `6a00607`).

1. `task test` → svi Go paketi `ok`; vitest `Test Files 31 passed`, `Tests 1276 passed`. `task test:race` → svi
   paketi `ok`, bez `DATA RACE`. `task lint` → `0 issues.`, svelte-check `245 FILES 0 ERRORS 0 WARNINGS`.
   `task build` → exit 0. Uz to `go test -race -count=1 ./...` (bez cachea) → svih osam paketa `ok`.
2. `cd frontend && npx vitest run src/theme` → `Test Files 9 passed`, `Tests 597 passed` (kontrast, tablice iz
   `DESIGN.md`, xterm tema i colorscheme za sve tri teme).
3. `go test ./internal/config -run 'Theme|Version' -count=1` → `ok`;
   `go test . -run 'TestEd(SampleAccepted|BadScheme|SetScheme)|TestMenu|TestSettings' -count=1` → `ok`.
4. `bash scripts/smoke.sh` → `smoke: all checks passed` (157 `ok`, nijedan `FAIL` ni `skip`): svijetli prolaz
   `Normal uses the tokens text and bg.editor (1D2125 FFFFFF)`, `&background is light`; tamni prolaz
   `the frontend asked the service for the theme and got dracula`, `&background is dark and the colorscheme
   is pisar`, `Normal in Neovim has exactly those colours` (`F8F8F2 282A36`), `config.json is byte for byte
   what it was`.
5. `grep -rnE '#[0-9a-fA-F]{6}' frontend/src --include='*.svelte' --include='*.css'` → bez pogodaka (exit 1).

### Pregled plana (`plan-reviewer`)

- Krug 1: FAIL bez blokera. Kod radi sve što plan traži, sve naredbe prolaze (reviewer ih je sam
  ponovio, uz `go test -race -count=1 ./...` i `smoke.sh`), nijedan test nije oslabljen, nema novih
  ovisnosti ni rada iz faze 8. Šest manjih nalaza, svi u ovom dokumentu: četiri nezabilježena odstupanja
  od plana (`themeSender.ts` umjesto `latest.ts`, `colorscheme.go` umjesto `nvim.go`, `tokenColorIn` uz
  `tokenColor`, dirnut `EditorPane.svelte`), `config.Themes` zapisan kao varijabla umjesto funkcije i
  nepostojeći odjeljak na koji je korak 9 upućivao. Sve je ispravljeno ovdje. Uz to zapažanje o
  izgubljenom događaju `theme:changed` tijekom učitavanja stranice: ispravljeno u kodu (korak 8).
- Krug 2: FAIL bez blokera. Svih šest nalaza iz kruga 1 je riješeno i tekst odgovara kodu. Dva nova manja
  nalaza: ispravak iz kruga 1 otvorio je simetričnu utrku (zakašnjeli odgovor `Theme()` mogao je pregaziti
  noviji događaj) — ispravljeno zastavicom u `ThemeController.start()`; rečenica o tome kada su dokazi
  pokrenuti bila je zastarjela — dokazi su ponovljeni nakon ispravka i rečenica je ispravljena.
- Krug 3: PASS. Nema preostalih odstupanja; reviewer je ponovio `task test`, `task lint`, `task build`,
  vitest tema i `smoke.sh` (157 `ok`) i prošao slučajeve utrke u `ThemeController.start()`. Otvoren je samo
  ručni kriterij **[R]**.

### Ručna provjera — za vlasnika (agent ovo ne može provjeriti)

Pokretanje bez diranja prave konfiguracije: `PISAR_HOME=/tmp/pisar-teme bin/pisar` (ili bez varijable; tada
se prava `config.json` pri prvoj promjeni teme prepisuje u verziju 2).

1. Prikaz → Tema ima tri stavke (Pisar, Catppuccin Mocha, Dracula) s kvačicom na aktivnoj.
2. Izbor teme odmah boji alatnu traku, stablo, terminal, Neovim (statusna traka, redak kursora, naslovi)
   i pregled, bez ponovnog učitavanja; pregled zadržava položaj skrola.
3. Nakon ponovnog pokretanja aplikacija kreće u izabranoj temi, kvačica je na njoj, i nema bijelog bljeska
   prije prvog prikaza (ako ga ima, javi: rješenje je zasebna odluka, vidi rizike u planu).
4. Promjena teme dok u Neovimu stoji hit-enter prompt (npr. nakon `:echo "a\nb"`): sučelje se promijeni
   odmah, Neovim nakon Entera.
5. Čitljivost u obje tamne teme: prigušeni tekst u stablu, brojevi redaka, odabrani red u tražilici,
   poruke o grešci, tekst na primarnom gumbu.
6. Promjena teme prije nego je ijedan dokument otvoren, pa otvaranje dokumenta: Neovim kreće u novoj temi.

### Nije provjereno / poznata ograničenja

- Sve iz ručne provjere gore: klik u meniju, kvačice, boja prozora i bljesak pri pokretanju nisu viđeni.
- Promjena teme usred pokretanja Neovima (`resend` u `Terminal.svelte`) provjerena je samo čitanjem koda.
- Ako slanje colorschemea Neovimu ne uspije, sučelje je u novoj temi, a Neovim u staroj, do sljedećeg
  pokretanja ili sljedeće promjene teme; greška je samo u konzoli.
- Highlight koji korisnik postavi u `user.lua` nestaje nakon promjene teme (`highlight clear`); treba ga
  vezati uz `ColorScheme` autocmd.
- 16 ANSI boja terminala izvedeno je iz tokena; u tamnim temama „white” je tamna podloga (tiče se samo
  programa unutar `:terminal`).
- Prompt „-- More --” ne zatvara se Enterom; ponašanje promjene teme u njemu nije mjereno.

### Kriteriji prihvaćanja

Automatski dio je dokazan (odjeljak „Dokazi po kriterijima”); zadnja kućica čeka vlasnikovu ručnu provjeru.

- [x] `task test`, `task test:race`, `task lint`, `task build` prolaze — dokaz 1
- [x] `cd frontend && npx vitest run src/theme` — kontrast i tablice iz `DESIGN.md` za tri teme — dokaz 2
- [x] `go test ./internal/config -run 'Theme|Version'` i
      `go test . -run 'TestEd(SampleAccepted|BadScheme|SetScheme)|TestMenu|TestSettings'` prolaze — dokaz 3
- [x] `bash scripts/smoke.sh` — svijetli prolaz i prolaz s Draculom (`Normal` = tokeni, `&background` = `dark`) — dokaz 4
- [x] `grep -rnE '#[0-9a-fA-F]{6}' frontend/src --include='*.svelte' --include='*.css'` → bez pogodaka — dokaz 5
- [x] **[R]** promjena teme boji traku, stablo, terminal, Neovim i pregled bez ponovnog učitavanja; pregled
      zadržava skrol; pokretanje s tamnom temom bez bijelog bljeska; kvačica u meniju točna i nakon
      pokretanja; promjena dok stoji hit-enter prompt primijeni se nakon Entera — vlasnik potvrdio ručnu provjeru 2026-11-13

## Faza 8 — Prečaci i which-key

Effort high. Plan je odobren 2026-11-12 (isti plan kao faza 7); vlasnik je 2026-11-13 rekao „kreni”.

### Odluke vlasnika (2026-11-12)

- Vlastiti which-key u `nvim/init.lua` (bez dodatka): okidač je goli `<Space>` u `n` i `x`, tipke hvata
  `vim.on_key` (Neovim nikad ne blokira); `timeoutlen = 500`.
- Vlastiti prečaci i imena grupa (`vim.g.pisar_groups`) u `user.lua`; gašenje s `vim.g.pisar_whichkey = false`.
- Pregled svih prečaca kao Svelte panel na `⌘/` i `␣?`.
- Stavka menija „Uredi user.lua”: Go datoteku ne piše; ako je nema, Neovim otvori nespremljen buffer s
  predloškom, a `CLAUDE.md` dobiva rečenicu da `<home>/user.lua` zapisuje sam Neovim na `:w`.
- Raspored tipkovnice je US; `⌥` i dalje slaže znakove (`macOptionIsMeta: false`), terminal koristi `ev.key`.

### Ovisnosti

Nema novih.

### Izmjereno prije koraka 1 (Neovim 0.12.6, `--headless`, tipke kroz `--remote-send`)

- Goli `<Space>` uz dulja `<Space>…` mapiranja okida se tek nakon `timeoutlen`; brzo utipkan `␣e` ide ravno
  na `<leader>e` i prozor se ne otvara.
- `vim.on_key(fn, ns)`: `fn(key, typed)`; povrat `""` odbacuje tipku. Float s `focusable = false`,
  `noautocmd = true` ne mijenja mod ni trenutni prozor (`mode = n`, `blocking = false`, dva prozora).
- **Tipka koja je sama mapirana curi.** `j` je u `init.lua` mapiran na `gj`: `on_key` dobije `key = g`,
  `typed = j`, a zatim `key = j`, `typed = ""`. Ako se nakon prve odbaci samo `g` i skine hvatač, `j` se
  izvrši (kursor se pomakne). Rješenje: nakon odluke hvatač ostaje u stanju „drain” i odbacuje tipke s
  `typed == ""` dok ne stigne prva utipkana tipka ili dok ne odradi `vim.schedule` (izmjereno: zakazani
  poziv dolazi tek kad je ostatak mapiranja potrošen).
- Izvršavanje: `nvim_feedkeys(brojač .. lhs, "mt", false)` odmah u hvataču. Ubačene tipke stižu s nepraznim
  `typed` (za Lua mapiranje `key = <t_…>`, `typed = "<Space>j"`), pa prva od njih skida hvatač. Brojač se
  prenosi (`3␣e` → `vim.v.count == 3` u mapiranju). U vizualnom modu mapiranje se izvrši u modu `v`.
- `<Down>` iza `␣` stiže kao jedna tipka (`typed` je interni zapis, isti kao `lhsraw`).
- **Tipka koja je početak nekog mapiranja normalnog moda kasni za `timeoutlen`.** Neovim ima zadana
  mapiranja `gc`, `gx`, `gr…`, pa `g` utipkan sam stigne hvataču tek nakon 500 ms; `gs` utipkan brzo stiže
  odmah. Vrijedi za grupe pod `g`, `[`, `]`. Ograničenje se ne zaobilazi (promjena `timeoutlen` dok je
  prozor otvoren bila bi opasna ako zatvaranje ne uspije).
- Dvije tipke u istom čitanju (`ej` u jednom slanju): druga se izvrši prije ubačenog mapiranja. U stvarnom
  radu svaka tipka je zaseban upis u PTY; ograničenje se bilježi, ne rješava.
- Klik mišem (`<LeftMouse>`) i `:` dok je prozor otvoren stižu kao nepoznata tipka: zatvaraju i gutaju se.

### Koraci

- [x] 1. Which-key i `timeoutlen`: blok `install_whichkey` u `nvim/init.lua` (float bez fokusa, `vim.on_key` sa
      stanjima `open` i `drain`, autocmd grupa `PisarWhichKey`), `timeoutlen = 500`, tekstovi `nvimMapKeys` i
      `nvimKeysTitle`, `internal/nvim/whichkey_test.go` (7 testova `TestWhichKey*`, 30-ak podtestova), dva
      retka u `pisarMaps`.
      - Prikazuju se samo mapiranja s `desc`; kretanje i izvršavanje vrijede za sva (dopuna plana, da
        korisnikovo mapiranje bez opisa ne prestane raditi kad se prozor otvori).
      - Grupa bez ičega za prikaz (ili bez mjesta): hvatač ostaje bez prozora i sljedeća tipka se obrađuje
        isto. Prvi diff je tu vraćao prefiks u red tipki, što je Neovim ostavljalo bez odgovora (izmjereno u
        pregledu, ispravljeno, čuvaju podtestovi `blind` i `blesc`; bez ispravka padaju).
      - Tipka pod kojom su i točno mapiranje i dulja vodi u grupu; točno je dostupno brzim tipkanjem.
      - Neimenovana grupa pokazuje `+N`, gdje N broji sva mapiranja ispod nje, i ona bez `desc`.
      - Float ostavlja jedan redak razmaka iznad statusne linije (`max_h = row - 3`).
      - `uitext_test.go`: `TestUITextNvim` sada očekuje 19 tekstova (dva nova ključa dodana u popis).
      - `PisarKey` i `PisarKeyGroup` su dotad nedefinirane grupe; boje stižu u koraku 2.
      - `bash scripts/smoke.sh` nakon koraka: `smoke: all checks passed` (157 `ok`).
- [x] 2. Boje floata: `PisarKey` (`accent` na `bg.chrome`, podebljano) i `PisarKeyGroup` (`syntax.code` na
      `bg.chrome`) u `theme/nvimColorscheme.ts`; tri snimke u `testdata/colorscheme/` dobile su po dva retka;
      nova tvrdnja u „required groups”; izračunati test kontrasta mjeri obje grupe u tri teme (vitest 1283).
      - `PisarKeyGroup` nije `syntax.list`: na `bg.chrome` u temi Pisar daje samo 4,32. Omjeri izabranih
        boja: `accent` 4,99 / 9,81 / 6,55, `syntax.code` 5,66 / 11,81 / 11,51 (Pisar / Mocha / Dracula).
      - Izgled u aplikaciji nije viđen (ručna provjera).
- [x] 3. Skupljač mapiranja prema Go-u: `_G.pisar_keys(max_maps, max_text)` u `init.lua` (vraća se nakon
      `user.lua`), `internal/nvim/keymaps.go` (`Keymap`, `Keymaps`, `Process.Keymaps`), `Session.Keymaps`,
      `EditorService.Keymaps` (JSON `maps`, `groups`, nikad `null`), `Limits.MaxKeymaps` (500) i
      `Limits.MaxKeymapText` (200); bindings regenerirani. Testovi `TestKeymaps*` (4), `TestKeys` (term),
      `TestEdKeymaps`, `TestEdKeymapsOff`, `TestLimitsKeymaps`.
      - Vraćaju se GLOBALNA mapiranja modova `n`, `x`, `i` s `desc`. Zadana Neovimova (`gc`, `gx`, `gr…`,
        `]d`, s engleskim opisima) izostavljaju se: `init.lua` ih snimi na samom vrhu, a mapiranje koje ih
        pregazi vraća se. Buffer-lokalna (ftplugin) se ne vraćaju. Plan to nije određivao.
      - `MaxKeymapText` je dodan uz planirani `MaxKeymaps`: bez njega bi popis bio ograničen brojem, a ne
        veličinom. Rez je u znakovima (`strcharpart`), nikad kroz višebajtni znak.
      - Dok Neovim čeka u promptu, `Keymaps` odmah vraća `ErrPromptOpen`; frontendu to stiže kao kod
        `internal` (kao i kod `Save`), pa se u logu bilježi kao greška. Novi kod nije dodavan.
      - `vim.g.pisar_groups` s miješanim ključevima (`{ g = "Git", [1] = "x" }`) Neovim odbija već pri
        postavljanju (E5100); to je greška u `user.lua`, a ne u skupljaču.
- [x] 4. `␣?`, događaji i stavka menija: `<leader>?` u `init.lua` šalje `pisar_keys` → `EventOpenShortcuts` →
      `Emitter.OpenShortcuts()` → `ui:open-shortcuts`; meni Prikaz → „Prečaci tipkovnice” (`CmdOrCtrl+/`, ispod
      Teme, iza razdjelnika) šalje `menu:shortcuts`; `SettingsService.MenuShortcuts(ctx)` vraća isti popis kao
      `menuShortcuts()` (`{label, accel}`, nikad nil); oba događaja registrirana u `main.go`; bindings
      regenerirani. `TestMenuShortcutsUnique` očekuje 7; `TestUITextNvim` 20 tekstova.
      - Wails v3.1.0 `pkg/application/keys.go` (`parseKey`) prima bilo koji jedan ispisiv znak, pa je `/`
        valjan akcelerator. Okida li `⌘/` stavku u pokrenutoj aplikaciji nije viđeno (ručna provjera).
      - Generirani tip povrata je `MenuShortcut[] | null`, iako Go nikad ne vraća nil; frontend s tim računa.
      - Frontend događaje sluša tek od koraka 6.
- [x] 5. Otvaranje `user.lua`: `Process.OpenUserLua(ctx, path, template)` i `ErrNotRegularFile`
      (`internal/nvim/userlua.go`), `Session.OpenUserLua`, `EditorService.OpenUserLua(ctx)`, stavka menija
      Datoteka → „Uredi user.lua” (na kraju, iza razdjelnika, bez prečaca; `menuActions.editUserLua`), tekstovi
      `menuEditUserLua`, `nvimUserLuaNotFile`, `userLuaTemplate`; rečenica u `CLAUDE.md` („`.md`, `.txt` i
      `<home>/user.lua` zapisuje sam Neovim na `:w`”). Testovi `TestUserLuaOpen*`, `TestEdOpenUserLua`, menija.
      - Go datoteku ne stvara, ne piše i ne briše. Bez datoteke Neovim dobije buffer s predloškom
        (`modified`); datoteka nastaje na `:w` / `⌘S`. Postojeća se otvara bajt-identična, bez predloška.
      - Predložak je u `strings.json` (`userLuaTemplate`, ne ide u `PISAR_TEXT`); svi primjeri su
        zakomentirani. Test ga učitava kao `user.lua` kakav jest i s odkomentiranim primjerima.
      - Folder, FIFO i slično: ništa se ne otvara, jedan redak na liniji poruka, `ErrNotRegularFile`. Viseći
        link se računa kao da datoteke nema (spremanje tada stvara cilj linka, kako Neovim inače radi).
      - Hit-enter prompt: Lua poziv tamo ne dobiva odgovor, pa se prvo pita `nv.Mode()` (kao `Keymaps`);
        upit o swap datoteci hvata `refuseDuringPrompt`. Oba vraćaju `ErrPromptOpen`.
      - Kad je `user.lua` već trenutni buffer, `:edit` se preskače (s izmjenama bi dao E37).
      - Prije koraka provjereno: `describePath` i frontend (`activeRowKey`, `followActive`) već podržavaju
        dokument izvan svih projekata; traka prikazuje ime, stablo ga preskače. Izmjena nije trebala.
      - Posljedica: buffer s predloškom je `modified`, pa dijalog pri izlasku pita za `user.lua` ako ga
        korisnik nije spremio ni zatvorio. Uključen pregled prikazuje Lua izvor kao Markdown.
      - `ErrNotRegularFile` i `ErrPromptOpen` stižu frontendu kao kod `internal` (novi kod nije dodavan).
      - `TestUITextNvim` sada očekuje 21 tekst (`nvimUserLuaNotFile`).
- [x] 6. Pregled prečaca u frontendu: `lib/shortcuts.ts` (`formatAccel`, `formatNvimKeys`, statični redci
      stabla i tražilice, `buildSections`), `lib/shortcutsState.ts` (čisti stroj stanja), `lib/shortcutsKeys.ts`,
      `lib/shortcutsController.svelte.ts`, `components/ShortcutsPanel.svelte`; `App.svelte` i `EditorPane.svelte`
      (kontroler, panel, `keepsFocus` uključuje otvoren pregled); 22 nova teksta `shortcuts*`. Vitest: 109
      novih testova u tri datoteke (ukupno 34 datoteke, 1392 testa).
      - `⌘/` i `␣?` otvaraju i zatvaraju; `Esc` zatvara i vraća fokus u terminal (bez Neovima u stablo);
        klik izvan panela i odlazak fokusa zatvaraju bez vraćanja fokusa, kao kod tražilice. Zato `⌘P`
        zatvara pregled, a `⌘/` tražilicu.
      - Odjeljak Neovim: isti prečac u više modova je jedan redak s oznakama modova; prefiks „Pisar: ” se
        ne prikazuje; ime grupe po najduljem ključu iz `groups` koji je prefiks tipki iza leadera.
      - Test čuva da svaka tipka navedena za stablo i tražilicu stvarno daje radnju u `keyToAction` /
        `keyToSearchAction`.
      - Ako `MenuShortcuts` ne uspije, odjeljak Aplikacija ostaje prazan bez napomene (greška u konzoli).
      - Kontroler i komponenta nemaju testove (kao ni kod tražilice); izgled, fokus i tipke u pokrenutoj
        aplikaciji nisu viđeni (ručna provjera).
- [x] 7. Dimna provjera i dokumenti: `scripts/smoke.sh` dobio je blok „D. Which-key and the shortcut overview”
      na kraju odjeljka pregleda i tražilice (osam novih `ok` redaka: polazno stanje, `<Space>` otvara drugi
      prozor, mod `n` bez blokade i isti trenutni prozor, `e` daje točno jedan `ui:focus-tree`, prozor se
      zatvori, `<Space>?` daje `ui:open-shortcuts`, na kraju jedan prozor u normalnom modu); `DESIGN.md`:
      četiri nova retka u tablici tipkovnice i odlomci „Prozor s prečacima (which-key)” i „Pregled prečaca”.
      - Mutacijska provjera: s očekivanjem tri prozora skripta pada točno na toj provjeri.
      - Tipke se šalju odvojeno (`<Space>`, čekanje prozora, pa `e`), jer bi slanje unutar `timeoutlen`
        izvršilo izravno mapiranje bez prozora.

### Dokazi po kriterijima (2026-11-13, nakon zadnjeg commita koda)

1. `task test` → exit 0: svi Go paketi `ok`, vitest `Test Files 34 passed`, `Tests 1392 passed`.
   `go test -race -count=1 ./...` → svih osam paketa `ok` (`internal/nvim` 97 s). `task lint` → `0 issues.`,
   svelte-check `253 FILES 0 ERRORS 0 WARNINGS`. `task build` → exit 0.
2. `go test -count=1 ./internal/nvim -run 'TestWhichKey|TestUMap|TestKeymaps|TestUserLuaOpen'` → `ok` (62
   testa i podtesta). `go test -count=1 . -run 'TestMenuShortcutsUnique|TestEdKeymaps|TestEdOpenUserLua'` →
   `PASS` (`TestEdOpenUserLua`, `TestEdOpenUserLuaStopped`, `TestEdKeymaps`, `TestEdKeymapsOff`,
   `TestMenuShortcutsUnique`).
3. `cd frontend && npx vitest run src/lib/shortcuts` → `Test Files 3 passed`, `Tests 109 passed`.
   `bash scripts/smoke.sh` → `smoke: all checks passed` (165 `ok`, 0 `FAIL`), s novim provjerama iz koraka 7.

### Pregled plana (`plan-reviewer`) — krug 1 (2026-11-13): PASS

Bez blokera; reviewer je sam pokrenuo sve automatske kriterije i `scripts/smoke.sh` (165 `ok`). Nalazi:

1. Manji, zabilježen kao ograničenje: uska utrka u stanju `drain` (vidi „Nije provjereno / poznata
   ograničenja”). Predloženi popravak (držati `drain` samo kad je `key ~= typed`) nije primijenjen: mapiranje
   koje počinje istom tipkom (`j` → `jzz`) ima `key == typed`, a ostatak bi ipak procurio.
2. Manji, ispravljen: zastarjeli broj tekstova u napomeni koraka 5 (`TestUITextNvim` 21).
3. Zapažanje, dodan test: grupa na dvije razine (podtest `deep` u `TestWhichKeyUser`: `+3`, naslov `t`,
   `+Druga`, naslov `Druga`, izvršeno mapiranje, hvatač skinut).
4. Zapažanje, dodan test: statični redci stabla i tražilice sada nose očekivane vrste radnje (`actions`),
   a test provjerava koju radnju tipka daje i smjer kretanja; zamjena `r` i `d` u `treeKeys.ts` ga obara.
5. Zapažanje, zabilježeno kao ograničenje: „Uredi user.lua” bez pokrenutog Neovima tiho ne radi ništa.

Nakon dodanih testova (bez izmjene produkcijskog ponašanja): `task test` exit 0 (vitest 1394), `task test:race`
exit 0 (`internal/nvim` 97 s), `task lint` `0 issues.` i svelte-check 0 grešaka, `task build` exit 0.

### Ručna provjera — za vlasnika (agent ovo ne može provjeriti)

Pokretanje bez diranja prave konfiguracije: `PISAR_HOME=/tmp/pisar-precaci bin/pisar`.
**Prije svega dodaj projekt i otvori dokument:** Neovim se pokreće tek s prvim otvorenim dokumentom, a prazan
privremeni home nema projekata, pa bez toga `␣` nema kome stići (vlasnik je na to naišao 2026-11-13).

1. `␣` pa pričekaj: prozor s prečacima se pojavi nakon otprilike pola sekunde, dolje desno iznad statusne
   trake; čitljiv u sve tri teme (tipke u boji akcenta, imena grupa zeleno).
2. Brzo utipkan `␣f` otvara tražilicu bez treptaja prozora.
3. Dok je prozor otvoren: tipka s popisa izvrši radnju; `Esc`, `␣` i nepoznata tipka ga zatvore i ne
   učine ništa drugo; `⌘S` i `⌘B` rade.
4. Klik mišem u terminal i lijepljenje (`⌘V`) dok je prozor otvoren ne čine ništa neočekivano.
5. `⌘/` otvara i zatvara pregled prečaca iz terminala, iz stabla i iz tražilice; `␣?` ga otvara iz
   Neovima; `Esc` vraća fokus u Neovim; `j`/`k` i strelice pomiču popis.
6. Pregled prečaca: četiri odjeljka, tipke u obliku `⌘S`, `␣ f`; čitljivo u sve tri teme.
7. Datoteka → „Uredi user.lua” u praznom homeu pokaže predložak; `<home>/user.lua` nastane tek nakon `⌘S`;
   traka pokazuje `user.lua`.
8. Vlastiti prečac i grupa iz `user.lua` (odkomentiraj primjere iz predloška, ponovno pokreni): vide se u
   prozoru iza `␣` i u pregledu prečaca.

### Nalazi vlasnikove ručne provjere (2026-11-13)

- **Ispravljeno (vlasnik odobrio 2026-11-13):** „Uredi user.lua” bez pokrenutog Neovima nije radio ništa, a
  u praznom homeu Neovim nikad ne radi, pa kriterij „u praznom homeu pokaže predložak” nije bio ispunjen.
  Sada stavka menija samo šalje događaj `menu:edit-user-lua`; `EditorController` po potrebi pokrene Neovim
  (isti put kao otvaranje dokumenta: `OpenRequest.userLua`, zahtjev čeka u `pendingOpen`) i zatim pozove
  `EditorService.OpenUserLua()`, pa da fokus terminalu. **Promjena ugovora iz plana:** meni više ne zove Go
  izravno, `OpenUserLua` je uklonjen iz `menuEditor`. Tri testa menija iz koraka 5 (koji su tvrdili staro
  ponašanje) zamijenjena su jednim tabličnim za novo; šest novih testova u `editorState.test.ts`.
  `task test` (vitest 1409), `go test -race -count=1 .`, `task lint`, `task build`, `scripts/smoke.sh`
  (165 `ok`): exit 0. Klik na stavku i cijeli put u pokrenutoj aplikaciji nisu viđeni (kontroler nema
  testove).
- **Ispravljeno:** u tražilici je poruka „Nema dokumenata za taj upit.” nestajala pri svakom utipkanom znaku
  i vraćala se s odgovorom, pa je panel skakao (greška iz faze 4). `searchState.ts` sada pamti da je zadnji
  odgovor bio prazan (`emptyAnswer`) i poruka ostaje dok se čeka sljedeći; briše je odgovor s pogocima,
  greška, ispražnjen upit i zatvaranje. Prvi upit nakon otvaranja i dalje ne pokazuje poruku dok se čeka.
  Devet novih testova; bez ispravka dva padaju. `task test` (vitest 1403), `task lint`, `task build`: exit 0.
  Izgled u aplikaciji nije viđen.
- **Ispravljeno (vlasnik zatražio 2026-11-13):** prazno stanje editora više ne nudi `␣f`, samo `⌘P`. To se
  stanje prikazuje dok Neovim ne radi, pa njegovo mapiranje nema kome stići (`EmptyState.svelte`, jedan
  redak; `DESIGN.md`, odlomak „Prazno stanje”). Izmjenu je napravila glavna sesija (ispravak nakon pregleda,
  dva retka). `task test` (vitest 1409), `task lint`, `task build`: exit 0. Izgled nije viđen.

### Nije provjereno / poznata ograničenja

- Sve iz ručne provjere gore: izgled prozora i panela, fokus, `⌘/` kao akcelerator, klik na stavke menija.
- Ponašanje `vim.on_key` s mišem i lijepljenjem kroz pravi PTY (pokusi i testovi idu kroz PTY upis odnosno
  `--remote-send`; klik i bracketed paste nisu poslani).
- Tipka koja je početak nekog mapiranja normalnog moda (`g`, `[`, `]`; zbog zadanih `gc`, `gx`, `gr…`)
  stiže which-keyu tek nakon `timeoutlen`, osim ako se sljedeća utipka brzo. Grupa pod `g` zato kasni
  pola sekunde.
- Dvije tipke u istom čitanju mogu se izvršiti obrnutim redom (mapiranje iz prozora ide u red iza već
  utipkanog).
- `user.lua` koji zamijeni `vim.on_key` kvari prozor. `timeoutlen = 500` mijenja osjećaj svih
  višetipkovnih mapiranja.
- Pregled prečaca ne navodi sistemske stavke menija (Zalijepi `⌘V`, Zatvori…): popis je `menuShortcuts()`,
  tj. vlastite stavke s prečacem. Ne navodi ni buffer-lokalna ni zadana Neovimova mapiranja.
- Dok Neovim čeka u promptu, odjeljak Neovim pokazuje napomenu, a u logu ostaje greška (kod `internal`).
- Nespremljen predložak `user.lua` je izmijenjen buffer: dijalog pri izlasku pita za njega.
- Uska utrka: ako zahtjev za oblikovanje (`⌘B` u vizualnom modu) stigne u istoj obradi događaja u kojoj
  tipka zatvara which-key prozor, stanje `drain` može progutati `<Esc>` kojim `pisar_format` izlazi iz
  vizualnog moda. Nije pokriveno testom.

### Izvan opsega

Which-key za prefikse osim leadera (`g`, `z`, `[`), operator-pending i insert mod; prozor za leader koji
korisnik promijeni; ponovno učitavanje `user.lua` bez pokretanja; uređivanje prečaca u sučelju; pretraga u
pregledu; Neovim dodaci; `⌥` kao Meta.

### Kriteriji prihvaćanja

- [x] `task test`, `task test:race`, `task lint`, `task build` prolaze — dokaz 1
- [x] `go test ./internal/nvim -run 'TestWhichKey|TestUMap|TestKeymaps|TestUserLuaOpen'` i
      `go test . -run 'TestMenuShortcutsUnique|TestEdKeymaps|TestEdOpenUserLua'` prolaze — dokaz 2
- [x] `cd frontend && npx vitest run src/lib/shortcuts` prolazi; `bash scripts/smoke.sh` s novim provjerama — dokaz 3
- [x] **[R]** prozor se pojavi oko pola sekunde nakon `␣`, čitljiv u sve tri teme; brzi `␣f` bez treptaja;
      `⌘S` i `⌘B` rade dok je otvoren; `⌘/` otvara i zatvara pregled iz terminala, stabla i tražilice; klik
      mišem ili lijepljenje dok je prozor otvoren ne čini ništa neočekivano; „Uredi user.lua” u praznom
      homeu pokaže predložak, a datoteka nastane tek nakon `⌘S` — vlasnik potvrdio ručnu provjeru 2026-11-13

## Faza 9 — Log datoteka i provjera verzija alata

Effort high. Plan odobren 2026-11-13 (`~/.claude/plans/sad-ajmo-napraviti-plan-prancy-plum.md`).

### Odluke vlasnika (2026-11-13)

- Log ide u Pisarov home: `<home>/logs/pisar.log` (uz `PISAR_HOME` u taj home).
- Prestar alat se odbija, kao da ga nema: prestar Neovim ne pokreće editor, prestar pandoc gasi Wordove
  dokumente i pregled; `.md` i `.txt` rade.
- Meni dobiva Datoteka → „Prikaži log u Finderu”.
- Odobreno planom: najmanje verzije Neovim 0.12.0 i pandoc 3.13; verzija koja se ne da pročitati ne
  blokira (upozorenje u logu); log bilježi isto što ide na stderr (Info, uz `PISAR_LOG=debug` Debug),
  dozvole 0600 / 0700; rotacija samo pri pokretanju iznad 1 MiB (`pisar.log.1`).

### Ovisnosti

Nema novih.

### Izmjereno prije plana

- `main.go` postavlja `slog` samo uz `PISAR_LOG=debug`, i to na stderr; bez varijable vrijedi Goov zadani
  handler (Info, stderr). Aplikacija iz Findera nema stderr.
- `scripts/smoke.sh` čita log iz stderr-a (`PISAR_LOG=debug … >"$LOG" 2>&1`), pa stderr mora ostati.
- Pandocov popis izdanja: `--math-method` (koristi ga `internal/preview`) dodan je u 3.11; 3.12.0 je
  pokvario `--embed-resources` (ispravljeno u 3.12.1). Instalirano: `pandoc 3.13`, `NVIM v0.12.6`.
- U Go kodu nema konstante s verzijom aplikacije (verzija je samo u `build/config.yml` / `Info.plist`).

### Koraci

- [x] 1. Log datoteka: `config.LogFile()` (`<home>/logs/pisar.log`, preko `DefaultHome`), `DefaultMaxLogBytes`
      (1 MiB); novi `logging.go` (`openLog`, `setupLogging`, `fileWriter`, `logStart`); `main.go` zove
      `setupLogging` odmah nakon `PrepareEnvironment`. Testovi `TestLogFile` i `logging_test.go`.
      - Log ide u datoteku I na stderr (Info; uz `PISAR_LOG=debug` Debug). Mapa 0700, datoteka 0600,
        dopisivanje; iznad 1 MiB pri pokretanju postaje `pisar.log.1`.
      - `io.MultiWriter` staje na prvoj grešci, pa je datoteka umotana u `fileWriter`: prvu grešku pisanja
        prijavi na stderr i dalje datoteku preskače. Aplikacija se zbog loga nikad ne zaustavlja.
      - Odstupanje od plana: prvi zapis nema verziju aplikacije (u Go kodu je nema); nosi home, putanju
        loga, izvršnu datoteku i PID. Dodan je i zapis `pisar: stopped with an error` kad `run()` vrati
        grešku, da se neuspjelo pokretanje iz Findera vidi u logu.
      - Pregled svih `slog` poziva: nijedan ne bilježi tekst dokumenta, izlaz terminala ni clipboard.
        Bilježe se putanje, duljine, kodovi i greške; `editorservice.go` bilježi i prvi redak greške iz
        `user.lua`.
      - `scripts/smoke.sh`: provjera sadržaja privremenog homea sada očekuje i mapu `logs`
        (`config.json logs nvim spell`). Nakon izmjene `smoke: all checks passed` (165 `ok`); prava
        `~/Library/Application Support/Pisar` nema mapu `logs`.
- [x] 2. Stavka menija Datoteka → „Prikaži log u Finderu” (ispod „Uredi user.lua”, bez prečaca):
      `menuActions.revealLog` (ubrizgani `reveal` i `logFile`), `revealCmd` (`/usr/bin/open -R <putanja>`,
      argumenti zasebno, rok `CommandTimeout`), `config.DefaultOpenBin`, tekst `menuRevealLog`.
      - Ako datoteke još nema, pokaže se mapa `logs`; ako nema ni nje, samo upozorenje u logu. Ništa se ne
        stvara i ništa se ne šalje frontendu.
      - Testovi ne pokreću `open`: provjeravaju argumente naredbe i pozive lažnog `reveal`.
        `TestMenuShortcutsUnique` ostaje 7.
      - `man open`: „-R Reveals the file(s) in the Finder instead of opening them.” Stvarno otvaranje
        Findera nije viđeno (ručna provjera).
      - Putanju naredbe je `coder` ostavio u `menu.go`; glavna sesija ju je premjestila u `internal/config`.
- [x] 3. Čitanje verzije alata: `config.MinNvimVersion` (0.12.0), `config.MinPandocVersion` (3.13),
      `config.MaxVersionOutput` (16 KiB); novi `toolversion.go` (`toolVersion`, `less`, `parseToolVersion`,
      `parseVersionNumber`, `probeVersion`, `versionCache`, `nvimXDGEnv`, `nvimProbeEnv`). Još ih ništa ne
      koristi za odluke. Testovi `TestToolVersion*` i `TestToolMinimumVersionsShape`.
      - Izmjereno: `nvim --version` traje oko 10 ms i stvori `<XDG_STATE_HOME>/<NVIM_APPNAME>/nvim.log`;
        `pandoc --version` oko 30 ms. Neovim se zato pita samo s Pisarovom preusmjerenom okolinom; test s
        pravim `nvim` potvrđuje da izvan privremenog homea ne nastaje ništa.
      - Proba ima vlastitu procesnu grupu i ubija je po isteku roka (dijete `sleep` ne ostaje); izlaz veći
        od 16 KiB je greška. Predmemorija ključa po razriješenoj putanji, veličini i vremenu izmjene;
        greške se ne pamte.
      - `v0.12.0-rc1` i `v0.13.0-dev-…` čitaju se kao 0.12.0 odnosno 0.13.0. Usporedba je brojčana
        (3.9 < 3.13).
      - Iz `editorservice.go` su četiri retka `XDG_*` izdvojena u `nvimXDGEnv` (isti sadržaj i redoslijed).
      - `coder` je tijekom rada jednom pokušao pokrenuti `nvim --version` bez preusmjerene okoline; prema
        njegovom izvještaju naredba je odbijena. Provjereno: u `~/.local/state/nvim/` nema `nvim.log`, a
        mapa nosi datum 10. 8.
- [x] 4. `Tools()` s verzijama i obavijest: `ToolStatus` ima `version`, `minimum`, `tooOld`; `toolState`,
      `toolsProbe`, `logTools` u `editortools.go`; frontend `toolProblems`, `toolsNotice` s naslovom
      (`toolsState.ts`), `EditorController.toolProblems`, `EditorPane.svelte`, `EmptyState.svelte`; tekstovi
      `toolsNotReadyTitle`, `tools{Nvim,Pandoc}TooOld`, `tools{Nvim,Pandoc}Upgrade`; bindings regenerirani.
      Vitest 1423. U ovom koraku se još ništa ne odbija.
      - Oba alata se pitaju istodobno, svaki do `CommandTimeout`, pa alat koji visi ne košta drugi
        odgovor. Nečitljiva verzija: `found`, `version ""`, nije prestar, upozorenje u logu.
      - U log (Info) ide stanje oba alata prvi put i pri svakoj promjeni; upozorenje o nečitljivoj
        verziji također samo pri promjeni (frontend zove `Tools` pri pokretanju).
      - Dva starija testa prilagođena novom ugovoru, uz pune usporedbe: `TestTools` (7 podslučajeva) sada
        očekuje i `Minimum` iz configa (lažni alati tog testa ne ispisuju verziju, pa `Version ""`);
        `describe('toolsNotice')` (4 testa) zove funkciju s `ToolProblem[]` i očekuje `{title, lines}`.
        Nijedan test nije obrisan.
      - Izgled obavijesti nije viđen (ručna provjera).
- [ ] 5. Odbijanje prestarog alata (`nvim-too-old`, `pandoc-too-old`)
- [ ] 6. Dimna provjera i dokumenti, zatim `plan-reviewer`

### Izvan opsega

Preglednik loga u aplikaciji, rotacija tijekom sesije, slanje loga ikamo, automatska nadogradnja alata,
provjera `textutil`-a, izmjena dijaloga o neuspjelom pokretanju, README, DMG i notarizacija, git remote i
sigurnosne kopije dokumenata.

### Kriteriji prihvaćanja

- [ ] `task test`, `task test:race`, `task lint`, `task build` prolaze
- [ ] `go test . -run 'TestLog|TestToolVersion|TestEdTools|TestMenu'` i
      `go test ./internal/config -run 'Log|MinVersion'` prolaze
- [ ] `cd frontend && npx vitest run src/lib/toolsState src/lib/errorMessage` prolazi
- [ ] `bash scripts/smoke.sh` → `smoke: all checks passed`, s novim provjerama loga i prestarog Neovima
- [ ] `grep -rnE 'os\.(Create|WriteFile|Remove)' logging.go toolversion.go` pokazuje pisanje samo u log
- [ ] **[R]** nakon pokretanja iz Findera `~/Library/Application Support/Pisar/logs/pisar.log` raste;
      „Prikaži log u Finderu” ga označi u Finderu; s lažnim starim `nvim` u PATH-u prazno stanje pokaže
      poruku o prestaroj verziji i naredbu, a „Provjeri ponovno” je makne kad se lažni alat ukloni

