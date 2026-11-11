# Pisar — izvještaj spikea (faza 0)

Datum: 2026-11-11 · Kod: `spike/` (za bacanje) · Stanje po koracima: `PROGRESS.md`

## Preporuka

**Idemo s pristupom A** (pravi Neovim u PTY-ju, prikazan kroz xterm.js u Wails prozoru), uz uvjet da
ručna provjera tipkovnice u odjeljku 3 prođe. Sve što se dalo provjeriti naredbom prolazi, a mjerenje
ne pokazuje ni zastoje ni razbijen prikaz. Odluku donosi vlasnik.

Što još nije dokazano: sedam kriterija **[R]** (tipkovnica, clipboard, izgled). Njih agent ne može
provjeriti bez slanja tipki u korisnikovu sesiju, pa nisu označeni kao prošli.

## 1. Što radi (dokazano naredbom)

Okruženje: macOS 27.2 (arm64), Neovim 0.12.0, Wails v3.1.0, xterm.js 6.0.0 (+ fit 0.11.0, WebGL
0.19.0), go 1.27.2, node 22.22.0.

| Kriterij iz plana | Dokaz |
|---|---|
| Go otvara datoteku u pokrenutom Neovimu preko RPC socketa | `TestOpen` (7 imena: razmaci, dijakritici, `%`, `#`, `\|`, navodnici) u `task -d spike test`; `bash spike/scripts/smoke.sh`: Neovim preko socketa javlja `141 33 sample.md` |
| Go prima obavijest kad Neovim spremi buffer | `TestSave` (RPC), `TestColonW` (`:w` utipkan kroz PTY), `TestWQ` (događaj se ne gubi ni kod `:wq`) |
| nakon gašenja aplikacije nema živog `nvim` procesa | `bash spike/scripts/shutdown.sh`: SIGTERM, SIGINT, SIGHUP, SIGKILL — oba Neovim procesa nestaju unutar 2 s; `TestClose`, `TestHung` (zaglavljeni server); `task -d spike bench`: aplikacija se sama gasi (`Quit`) i ne ostaje ništa |
| `docs/spike-report.md` | ovaj dokument |

Dodatno, izvan kriterija, a provjereno automatski:

- `task -d spike test`, `task -d spike test:race`, `task -d spike lint`: sve prolazi (Go testovi u tri
  paketa, 13 frontend testova, golangci-lint 0 nalaza, svelte-check 0 grešaka).
- Font JetBrains Mono učitan je prije otvaranja terminala; ćelija je 7.80×21 px (DOM) i 7.50×21 px
  (WebGL); mreža koju izračuna xterm.js jednaka je onoj koju Neovim prijavi (`smoke.sh`).
- Tipke stižu redoslijedom kojim su poslane (serijsko slanje, vidi 4.3).
- Izlaz PTY-ja nikad se ne reže usred UTF-8 znaka (testovi rezanja na svakoj granici bajta).

## 2. Mjerenje

`task -d spike bench` — bez tipkovnice: Go sam šalje `Ctrl-d` kroz PTY kroz datoteku od 5000 redaka
(`task -d spike sample`). Prozor 1100×700. Tablica je iz zadnjeg pokretanja (`spike/bin/bench-*.json`,
izvan gita); dva ranija pokretanja u koraku 6 dala su gotovo iste brojke, ali njihove datoteke nisu
sačuvane.

- `repeat`: `Ctrl-d` svaka 33 ms do dna (345 pritisaka, 11,4 s) — približno držana tipka.
- `flood`: `Ctrl-d` do dna pa `Ctrl-u` do vrha bez ikakve stanke (720 pritisaka, oko 5 s).

| Renderer | Faza | Događaja/s | MB/s | Emit → obrađeno p50 / p95 / max (ms) | Kadar p95 / max (ms) | Kadrova > 33 ms | > 100 ms |
|---|---|---|---|---|---|---|---|
| DOM | repeat | 42 | 0,04 | 0,1 / 7,3 / 9,9 | 18 / 19 | 0 | 0 |
| DOM | flood | 12 | 0,16 | 1,1 / 1,9 / 9,1 | 18 / 19 | 0 | 0 |
| WebGL | repeat | 44 | 0,04 | < 1 / < 1 / 3,5 | 18 / 20 | 0 | 0 |
| WebGL | flood | 12 | 0,16 | < 1 / 0,9 / 1,3 | 18 / 19 | 0 | 0 |

Što to znači:

- **Nema zastoja u iscrtavanju:** nijedan kadar nije kasnio više od jednog osvježenja zaslona (60 Hz).
- **Nema gubitka ni preslagivanja:** u svim fazama broj i duljina događaja jednaki su na Go i JS strani.
- **Nema razbijenog prikaza:** tekst redaka u xterm.js-u na kraju faze jednak je recima koje Neovim
  prikazuje.
- **Prijenos Wails događajima nije usko grlo.** Propusnost ograničava Neovim (0,16 MB/s u najgorem
  slučaju); najveći događaj je 16 KiB. Otvoreno pitanje iz plana §9 je time zatvoreno.
- **DOM renderer je dovoljan.** WebGL je brži u obradi (p95 ispod 1 ms naspram 7 ms), ali razlika se
  ne vidi u kadrovima. WebGL usto zaokružuje širinu ćelije na fizičke piksele (7.50 umjesto 7.80 px).

Ograničenja mjerenja:

- **Ulazni put nije mjeren.** `Ctrl-d` u PTY upisuje Go, pa put prave tipke (keydown → xterm.js →
  serijsko slanje → Wails poziv `Write` → PTY) nije vremenski izmjeren. Izmjereno je samo od
  emitiranja izlaza u Go-u do kraja obrade u xterm.js-u. Zaključak „nema zastoja” zato vrijedi za
  izlaz i iscrtavanje; kašnjenje same tipke pokazuje tek ručna provjera 7 u odjeljku 3.
- Sat u WKWebViewu zaokružen je na oko 1 ms, pa su vrijednosti ispod 1 ms na granici razlučivosti
  (pojavljuju se i blago negativne razlike).
- Mjereno je na jednom stroju, s praznom Neovim konfiguracijom i običnim tekstom; treesitter,
  provjera pravopisa i dugi prelomljeni reci nisu bili uključeni.

## 3. Ručna provjera [R] — za vlasnika

Priprema:

```sh
task -d spike build && task -d spike sample
spike/bin/spike spike/bin/sample-5000.md
```

| # | Kriterij | Što napraviti | Što treba vidjeti |
|---|---|---|---|
| 1 | `Esc`, `Ctrl-d/u/v/w/r` stižu do Neovima | `i`, tekst, `Esc`; `Ctrl-d`, `Ctrl-u`; `Ctrl-v`; `:vsplit` pa `Ctrl-w w`; `u` pa `Ctrl-r` | izlazak iz inserta; skrolanje; `-- VISUAL BLOCK --`; kursor mijenja prozor; redo vraća izmjenu |
| 2 | č ć š ž đ i znakovi s `⌥` | u insert modu upiši č ć š ž đ Č Ć Š Ž Đ i sve znakove koje inače dobivaš s `⌥` | svaki znak se upiše jednom, bez `^[` i bez izlaska iz inserta |
| 3 | `⌘C` / `⌘V`, `"+y` / `"+p` | kopiraj tekst u drugom programu → `⌘V` u insert modu (i višeredni); `V` pa `⌘C` → zalijepi drugdje; `"+yy` → zalijepi drugdje; kopiraj drugdje → `"+p` | tekst je isti u oba smjera; višeredni tekst se ne uvlači stepenasto |
| 4 | `⌘S` sprema preko RPC-a | izmijeni tekst, `⌘S` u normal, insert i visual modu | datoteka na disku je promijenjena; mod ostaje isti; u bufferu nema upisanog `s` |
| 5 | promjena veličine prozora | povlači rub prozora, i na vrlo malu veličinu | Neovim se preslaže; statusna linija je uvijek na dnu; nema odsječenih redaka |
| 6 | font i poravnanje | pogledaj tekst i razdjelnik nakon `:vsplit` | JetBrains Mono; okomita crta je ravna; kursor leži točno na znaku |
| 7 | držanje `Ctrl-d` kroz 5000 redaka | drži `Ctrl-d` do dna, `Ctrl-u` do vrha; ponovi nakon Prikaz → Renderer: WebGL | bez zastoja, bez zaostalih ili pomaknutih redaka |

Napomena uz 4: `⌘S` namjerno ide kroz meni aplikacije (stavka „Spremi” → Go → RPC), bez JavaScripta.
Kriterij „ne presreće ga Wails meni” ovdje znači da ga ne proguta neka druga stavka menija.

Vrijedi usput pogledati: `⌘Z`, `⌘X`, `⌘A` ne rade ništa; nakon `:q` pojavi se poruka i Enter ponovno
pokreće Neovim; `⌘S` na bufferu bez imena pokaže E32 u Neovimu.

Ako želiš zapis onoga što aplikacija prima: `PISAR_SPIKE_TRACE=/tmp/trag.log spike/bin/spike …`.
**Trag bilježi svaku tipku**, pa ga ne uključuj dok tipkaš išta osjetljivo.

## 4. Nalazi i zaobilaznice

1. **Neovim 0.12 su dva procesa.** TUI radi u PTY-ju; server (iza RPC socketa) je njegovo dijete, ali u
   vlastitoj sesiji. Signal grupi procesa pogađa samo TUI. `Close` zato pamti PID servera i gasi ga
   zasebno (RPC izlaz → SIGTERM → SIGKILL). Kad aplikacija padne, oba procesa nestaju sama jer se PTY
   zatvori.
2. **Neovim pri pokretanju čeka odgovor terminala.** Šalje DSR i čeka 100 ms; bez odgovora ispiše
   upozorenje i stane na hit-enter promptu. Pri izlasku šalje DA1 i čeka do 1 s. Na ta dva upita zato
   odgovara Go, neovisno o tome je li xterm.js već priključen. xterm.js odgovori još jednom; ti
   dvostruki odgovori ne ulaze u buffer (`TestLateReplies`, sa stvarnim odgovorima xterm.js-a 6.0.0).
3. **Wails obrađuje svaki poziv iz frontenda u vlastitoj goroutini**, pa redoslijed poziva nije
   zajamčen. Tipke se zato šalju serijski: jedan `Write` u letu, sve što stigne u međuvremenu spaja
   se u sljedeći.
4. **xterm.js `lineHeight` množi izmjerenu visinu znaka, ne veličinu fonta.** Omjer 21/13 daje redak od
   27 px. Rješenje: izmjeriti prirodnu visinu i izračunati faktor za točno 21 px.
5. **Prave poruke o grešci zamrzavaju tipkovnicu.** U insert i visual modu Neovim nakon poruke o
   grešci ne prima tipke 1 s (nakon obične poruke u visualu 3 s). `Save` zato grešku ispisuje kao
   obojenu poruku i zove `:redraw`; poruka ostaje u `:messages`.
6. **`⌘C` ovisi o modu.** S vizualnim odabirom Go kroz RPC izvrši `"+y`; bez njega frontend kopira
   odabir iz xterm.js-a. `⌘V` je sistemska uloga menija, pa xterm.js dobije pravi `paste` događaj i
   pošalje ga kao bracketed paste.
7. **Putanja RPC socketa mora stati u 103 bajta** (`sun_path` na macOS-u). `Start` to provjerava
   unaprijed. Privremena mapa sustava već troši oko 50.
8. **Stavke iz Wailsovih uloga `AppMenu` i `WindowMenu` su na engleskom**; za hrvatski meni treba
   vlastite stavke.

## 5. Što ne radi ili nije pokriveno

- **Zaglavljeni server uz pad aplikacije.** Ako je Neovim server u beskonačnoj petlji, a aplikacija
  bude ubijena sa SIGKILL, server ostaje živ: nema tko ga ugasiti. Uredno gašenje taj slučaj rješava
  (`TestHung`).
- **Ostaci nakon pada.** Nakon SIGKILL ili SIGHUP ostaje privremena mapa `pisar-spike-*` s `init.lua`.
- **Neovimovo stanje živi izvan `spike/`.** Uz `NVIM_APPNAME=pisar-spike` Neovim piše u
  `~/.local/state/pisar-spike/` (`nvim.log`, `shada/`, `swap/`) i stvara `~/.local/share/pisar-spike/`.
  Te mape ostaju i nakon urednog gašenja, a u `swap/` završavaju swap datoteke dokumenata koje
  otvoriš. Pretpostavka iz plana „brisanjem `spike/` nestaje sve” zato ne vrijedi: uz `spike/` treba
  obrisati i te dvije mape. Trenutačno sadrže samo `nvim.log` i `shada/main.shada` (28 KB); nisam ih
  brisao. Testovi ih ne diraju (rade pod `pisar-test` unutar `t.TempDir()`).
- **`⌥` kao Meta nije dostupan** (namjerno): `<M-…>` mapiranja u Neovimu ne rade.
- **Tipke s `⌘` ne mogu doći do Neovima**; sve što treba `⌘` ide kroz meni aplikacije.
- **Miš izvan SGR načina** (`onBinary`) išao bi kroz string i bio bi pokvaren. Neovim traži SGR, pa se
  to ne događa; trag bi zabilježio `onBinary fired`.
- **Nije mjereno:** treesitter, provjera pravopisa, vrlo dugi reci, široki znakovi (emoji, CJK), drugi
  monitor s drugom gustoćom piksela, rad na bateriji.
- **`npm audit`:** dva niska nalaza (`deepmerge` kroz `@sveltejs/vite-plugin-svelte`, alat za build),
  bez dostupnog popravka.

## 6. Odstupanja od pravila i plana

- Neovim se pokreće s `exec.Command`, ne `exec.CommandContext`: istek konteksta pokretanja inače bi
  kasnije ubio dugoživući proces. Pravilo iz `CLAUDE.md` treba iznimku za takve procese.
- Korake 1 i 2 radio je general-purpose agent na Sonnetu s uputama iz `coder.md`, jer `coder` još nije
  bio registriran (odobrio vlasnik). Od koraka 3 radi `coder`.
- Skriptu `spike/scripts/shutdown.sh` i test `TestLateReplies` napisala je glavna sesija (provjere, ne
  produkcijski kod).
- Tekst sučelja je na dva mjesta, a `CLAUDE.md` traži jedno: poruke terminala u
  `spike/frontend/src/strings.ts`, a stavke menija i naslov prozora u Go-u (`spike/menu.go`,
  `spike/main.go`), jer meni gradi Go i ne može čitati TypeScript datoteku. U fazi 1 treba odlučiti
  gdje je jedini izvor (npr. Go, a frontend ga dobiva preko servisa).
- Bijela boja je zapisana na tri mjesta (`terminalTheme.ts`, `app.css`, `main.go`); nestaje s
  `theme/tokens.ts`.
- Trag tipki (`PISAR_SPIKE_TRACE`) i bench način nisu bili u planu; dodani su da se spike može
  provjeriti bez tipkovnice.
- U koraku 4 agent je bez naloga slao tipke aplikaciji preko `osascript`-a. Od koraka 5 to je u
  briefovima izričito zabranjeno, kao i svaki dodir sistemskog clipboarda.

## 7. Za fazu 1

Prenijeti (prepisati u `internal/`, ne kopirati slijepo):

- `spike/internal/nvim` — pokretanje, RPC, gašenje, odgovori na upite terminala; testovi su vrijedni.
- `spike/internal/term` — grupiranje izlaza i sesija.
- `serialSender.ts`, učitavanje fonta prije terminala, kalibracija visine retka.
- `scripts/smoke.sh` i `scripts/shutdown.sh` kao osnova provjere pri pakiranju.

Promijeniti:

- `init.lua` i socket držati u `~/Library/Application Support/Pisar/` (ili počistiti stare privremene
  mape pri pokretanju), da pad ne ostavlja tragove.
- Neovimu postaviti `XDG_STATE_HOME`, `XDG_DATA_HOME`, `XDG_CONFIG_HOME` i `XDG_CACHE_HOME` unutar
  `~/Library/Application Support/Pisar/`, da swap, shada i log ne završavaju u `~/.local/`.
- Boje terminala i font izvesti iz `theme/tokens.ts`.
- Vlastiti meni na hrvatskom umjesto Wailsovih uloga.
- Prije gašenja pitati za nespremljene izmjene (spike gasi s `qa!`).
- Zadani renderer: DOM; WebGL ostaviti kao mogućnost dok se ne pokaže potreba.

Baciti: trag tipki (`PISAR_SPIKE_TRACE`), bench način, generirane Wails mape za Android, iOS, Windows
i Linux.

## 8. Kako ponoviti

```sh
task -d spike test          # Go i frontend testovi
task -d spike test:race     # s detektorom utrka
task -d spike lint          # golangci-lint + svelte-check
bash spike/scripts/smoke.sh     # prava aplikacija, oba renderera (otvara prozor)
bash spike/scripts/shutdown.sh  # četiri signala (otvara prozor četiri puta)
task -d spike bench         # mjerenje; prozor mora ostati vidljiv
```
