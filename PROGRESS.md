# PROGRESS

Plan: `docs/plans/2026-11-11-pisar-design.md` · Protokol: `PROMPT.md` · Izgled: `docs/design/DESIGN.md`

## Stanje

- Trenutna faza: 0 — Spike (koraci gotovi 2026-11-11; čeka ručnu provjeru **[R]** i odluku vlasnika)
- Sljedeći korak: vlasnik prolazi popis **[R]** iz `docs/spike-report.md` (odjeljak 3) i odlučuje idemo / ne idemo
- Zadnja izmjena: 2026-11-11 (faza 0, koraci 1–9 gotovi)

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
- [ ] 9. `docs/spike-report.md` (napisan), kriteriji s dokazima (dolje), `plan-reviewer`: krug 1 FAIL
      (5 odstupanja u dokumentaciji, ispravljena), krug 2 u tijeku; zatim sažetak i STOP

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
- Mjerenje (`task -d spike bench`, 2026-11-11, prozor 1100×700, Neovim 0.12.0, tri pokretanja s
  gotovo istim brojkama). Faza `repeat`: `Ctrl-d` svaka 33 ms do dna (345 pritisaka, 11,4 s). Faza
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

- [ ] **[R]** `Esc`, `Ctrl-d`, `Ctrl-u`, `Ctrl-v`, `Ctrl-w`, `Ctrl-r` stižu do Neovima
- [ ] **[R]** č ć š ž đ i znakovi koji na mojoj tipkovnici traže `⌥` upisuju se ispravno
- [ ] **[R]** `⌘C` / `⌘V` rade sa sistemskim clipboardom; `"+y` i `"+p` također
- [ ] **[R]** `⌘S` sprema preko RPC-a i ne presreće ga Wails meni
- [ ] **[R]** promjena veličine prozora: Neovim se preslaže, bez odsječenih redaka
- [ ] **[R]** font je učitan prije otvaranja terminala; ćelije su poravnate
- [ ] **[R]** držanje `Ctrl-d` kroz datoteku od 5000 redaka: bez vidljivog zastoja i razbijenog prikaza
- [x] Go otvara datoteku u pokrenutom Neovimu preko RPC socketa — dokaz: `go test -v -run TestOpen ./internal/nvim/`
      → `--- PASS: TestOpen` (7 podtestova); `bash spike/scripts/smoke.sh` → `neovim says: '141 33 sample.md'`
- [x] Go prima obavijest kad Neovim spremi buffer — dokaz: `--- PASS: TestSave`, `--- PASS: TestColonW`,
      `--- PASS: TestWQ` (`task -d spike test` → `ok Pisar/spike/internal/nvim`)
- [x] nakon gašenja aplikacije nema živog `nvim` procesa — dokaz: `bash spike/scripts/shutdown.sh` →
      `shutdown: all checks passed` (četiri signala); `--- PASS: TestClose`, `--- PASS: TestHung`;
      `task -d spike bench` → `no Neovim process of ours is left` nakon `Quit`; `pgrep -fl nvim` prazan
- [x] `docs/spike-report.md`: što radi, što ne, zaobilaznice, preporuka — dokaz: `test -s docs/spike-report.md`

Sedam kriterija **[R]** gore ostaje neoznačeno dok ih vlasnik ne provjeri ručno (popis s radnjama:
`docs/spike-report.md`, odjeljak 3). Odluku idemo / ne idemo donosi vlasnik.

## Faza 1 — Kostur, projekti i stablo

### Kriteriji prihvaćanja

- [ ] struktura repozitorija iz `CLAUDE.md`, `task dev/build/test/lint`
- [ ] dodavanje i uklanjanje projekta (odabir foldera); konfiguracija preživljava ponovno pokretanje
- [ ] stablo prikazuje samo podržane vrste, s oznakom vrste; skrivene datoteke i folderi su izostavljeni
- [ ] nova datoteka, preimenovanje, brisanje u Trash
- [ ] stablo se osvježava kad se datoteke promijene izvana
- [ ] `theme/tokens.ts` postoji i iz njega izlaze CSS varijable

## Faza 2 — `.md` i `.txt`

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
