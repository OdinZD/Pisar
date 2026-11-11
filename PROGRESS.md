# PROGRESS

Plan: `docs/plans/2026-11-11-pisar-design.md` · Protokol: `PROMPT.md` · Izgled: `docs/design/DESIGN.md`

## Stanje

- Trenutna faza: 0 — Spike (u tijeku od 2026-11-11)
- Sljedeći korak: faza 0, korak 3 (most PTY ↔ frontend)
- Zadnja izmjena: 2026-11-11 (faza 0, korak 2 gotov)

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
- [ ] 3. Most PTY ↔ frontend: Wails servis (`Write`, `Resize`), događaji s izlazom grupirani po
      vremenu i veličini, bez rezanja usred UTF-8 znaka; brojači propusnosti
- [ ] 4. Terminal (Svelte): xterm.js + fit, prekidač DOM / WebGL, JetBrains Mono učitan prije
      otvaranja terminala, promjena veličine → fit → `Resize`
- [ ] 5. Tipke i clipboard: `⌘S` → Go → RPC, `⌘C` / `⌘V`, `"+y` / `"+p`, `Ctrl-` tipke i `Esc`,
      `⌥` slaže znakove
- [ ] 6. Mjerenje: datoteka od 5000 redaka, propusnost i kašnjenje pri držanju `Ctrl-d`, oba renderera
- [ ] 7. Gašenje aplikacije: hook zove `Close`; skripta gradi, pokreće, gasi i provjerava `pgrep`
- [ ] 8. Popis za ručnu provjeru **[R]** predan vlasniku
- [ ] 9. `docs/spike-report.md`, kriteriji s dokazima, `plan-reviewer`, sažetak, STOP

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
- [ ] Go otvara datoteku u pokrenutom Neovimu preko RPC socketa
- [ ] Go prima obavijest kad Neovim spremi buffer
- [ ] nakon gašenja aplikacije nema živog `nvim` procesa
- [ ] `docs/spike-report.md`: što radi, što ne, zaobilaznice, preporuka

Odluku idemo / ne idemo donosi vlasnik.

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
