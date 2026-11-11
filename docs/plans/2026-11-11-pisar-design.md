# Pisar — dizajn i plan

Datum: 2026-11-11 · Status: odobren pristup A (pravi Neovim u PTY-ju), dizajn „Radni stol + tražilica”

Stavke označene s **(provjeriti)** su pretpostavke koje treba potvrditi u alatu prije oslanjanja na njih.

## 1. Cilj

Osobni macOS editor za pisanje i uređivanje `.md`, `.txt` i `.docx` dokumenata (te uvoz `.doc`),
s pravim Neovimom kao editorom i pregledom dokumenata po projektima (folderima).

Uspjeh: dokument iz Worda mogu otvoriti, urediti u Neovimu, spremiti i ponovno otvoriti u Wordu bez
gubitka sadržaja koji nije na popisu poznatih gubitaka.

## 2. Odluke

1. **Markdown je radni format.** `.docx` se ne uređuje izravno, nego kroz radnu `.md` kopiju.
2. **Pravi Neovim**, kao vanjski proces u PTY-ju, prikazan kroz xterm.js. Nema vlastitog renderera mreže.
3. **Dva kanala prema Neovimu:** PTY za prikaz i tipke, RPC socket za upravljanje.
4. **Izvorni `.docx` je referentni dokument** pri konverziji natrag, da se zadrže stilovi.
5. **Zapis `.docx`-a je siguran:** backup, atomski zapis, provjera konflikta.
6. **`.docx` s onim što konverzija gubi** otvara se samo za čitanje, uz „Spremi kao kopiju”.
7. **`.doc` se samo uvozi** i sprema kao novi `.docx`.
8. **Projekt je registrirani folder.** Nema baze ni indeksa sadržaja (to radi Trag).
9. **Izolirana Neovim konfiguracija** (`NVIM_APPNAME=pisar`), uz neobavezni korisnikov `user.lua`.
10. **Tražilica je Svelte panel**, ne Neovim plugin: Go već ima popis datoteka, a nema ovisnosti o
    plugin manageru ni mreži.
11. **Pregled koristi pandoc** (markdown → HTML), isti alat kao izvoz, da pregled ne laže o konverziji.

## 3. Arhitektura

```
┌──────────────────────── Wails prozor (WKWebView) ────────────────────────┐
│  Svelte: Toolbar · Sidebar · Terminal (xterm.js) · Picker · Preview      │
└───────▲──────────────▲─────────────────▲───────────────────▲────────────┘
        │ bindings     │ PTY bajtovi     │ događaji          │ HTML
┌───────┴──────────────┴─────────────────┴───────────────────┴────────────┐
│ Go:  project   nvim (PTY + RPC)   convert   docsafe   preview   config   │
└──────────────────────┬──────────────────────┬───────────────────────────┘
                       │ PTY + RPC socket     │ os/exec
                  ┌────┴────┐          ┌──────┴───────────┐
                  │ Neovim  │          │ pandoc, textutil │
                  └─────────┘          └──────────────────┘
```

| Komponenta | Odgovornost |
|---|---|
| `internal/config` | JSON konfiguracija, popis projekata, putanje radnih kopija i backupa |
| `internal/project` | stablo dokumenata po projektu (samo `.md`, `.txt`, `.docx`, `.doc`), tražilica po imenu, praćenje promjena na disku |
| `internal/nvim` | pokretanje Neovima u PTY-ju, promjena veličine, RPC veza, prijem događaja (spremljeno, promijenjen buffer, promijenjen aktivni buffer), gašenje bez procesa siročadi |
| `internal/convert` | docx → md, md → docx, doc → docx, detekcija gubitaka; svaki poziv s vremenskim ograničenjem |
| `internal/docsafe` | backup, atomski zapis, provjera konflikta, oporavak radne kopije |
| `internal/preview` | markdown → HTML, s debounceom |
| Svelte | prikaz i fokus; nema poslovne logike |
| `nvim/init.lua` | postavke za prozu, mapiranja, autocmd-i koji javljaju Go-u, učitavanje teme |

## 4. Tokovi

### 4.1 Otvaranje `.md` / `.txt`
Sidebar ili tražilica → Go → RPC naredba Neovimu da otvori datoteku. Neovim čita i piše sam.

### 4.2 Otvaranje `.docx`
1. `convert` pregleda arhivu i traži ono što se gubi: komentare, praćene izmjene, tekstualne okvire
   **(provjeriti** točne XML elemente na fixtureima**)**.
2. pandoc pretvara docx → markdown u `~/Library/Application Support/Pisar/work/<id>/`, a slike
   izdvaja u podfolder iste mape.
3. Go pamti veličinu, mtime i hash izvornika.
4. Neovim otvara radnu `.md` kopiju. Ako su nađeni gubici, buffer je samo za čitanje i alatna traka
   prikazuje razlog i gumb „Spremi kao kopiju”.

Format markdowna je pandocov `markdown` (najbogatiji round-trip) **(provjeriti** na golden testovima;
ako se pokaže lošim, alternativa je `gfm`**)**.

### 4.3 Spremanje `.docx`
1. `:w` zapisuje radnu `.md` kopiju (običan zapis).
2. Autocmd nakon zapisa javlja Go-u preko RPC-a.
3. `docsafe` provjerava konflikt: ako se izvornik promijenio od otvaranja, pita
   Prepiši / Učitaj ponovno / Odustani.
4. Prvi put u sesiji radi backup izvornika u `…/Pisar/backups/` (čuva zadnjih N).
5. pandoc pretvara md → docx u privremenu datoteku u istom folderu, s izvornikom kao referentnim
   dokumentom.
6. Provjera rezultata (postoji, nije prazan, otvara se kao arhiva), zatim atomski `rename` preko
   izvornika uz prijenos dozvola i xattr-a.
7. Alatna traka prikazuje „Spremljeno”. Kod greške izvornik ostaje netaknut, a poruka kaže što nije uspjelo.

### 4.4 Uvoz `.doc`
`textutil` pretvara `.doc` u privremeni `.docx` **(provjeriti:** `man textutil`**)**, dalje kao 4.2.
Prvo spremanje stvara `Ime.docx` pored izvornika; ako već postoji, pita za ime.

### 4.5 Pregled
Autocmd na promjenu teksta javlja Go-u; Go nakon debouncea (oko 300 ms) uzima sadržaj buffera preko
RPC-a, pandoc ga pretvara u HTML, a frontend ga prikazuje u sandboxed iframeu bez skripti.

### 4.6 Tražilica
`␣f` u Neovimu (mapiranje koje javlja Go-u) ili `⌘P` otvara panel. Fokus prelazi na polje za upit;
`Esc` ga vraća terminalu. Pretraga je po imenu datoteke kroz sve projekte; `⌃p` sužava na aktivni projekt.

### 4.7 Gašenje
Aplikacija preko RPC-a pita Neovim ima li izmijenjenih buffera. Ako ima, potvrda Spremi / Odbaci /
Odustani. Zatim gasi Neovim i provjerava da proces ne ostane živ.

## 5. Greške

| Situacija | Ponašanje |
|---|---|
| `nvim` ili `pandoc` nije instaliran | zaslon s porukom i naredbom za instalaciju; aplikacija ne pada |
| pandoc ne uspije ili istekne vrijeme | izvornik netaknut, radna kopija sačuvana, poruka s izlazom greške |
| izvornik promijenjen izvana | dijalog konflikta (4.3, korak 3) |
| Neovim se ugasi (`:q` na zadnjem prozoru) | prazno stanje „Otvori dokument”; proces se pokreće ponovno pri sljedećem otvaranju |
| aplikacija se srušila s nespremljenom radnom kopijom | pri sljedećem otvaranju istog dokumenta ponudi oporavak |
| datoteka obrisana ili premještena izvana | stablo se osvježi; otvoreni buffer dobije upozorenje |

## 6. Testiranje

- **Go, jedinično:** table-driven; sve što piše radi u `t.TempDir()`.
- **Golden round-trip** (`.docx → .md → .docx → .md`) na fixtureima: naslovi, bold/italic, liste,
  ugniježđene liste, tablica, slika, fusnota, poveznica, hrvatski dijakritici. Poznati gubici su
  popisani u `docs/known-losses.md`; sve izvan popisa je bug.
- **`docsafe`:** prekid usred zapisa ne smije oštetiti izvornik; konflikt se otkriva; backup postoji
  prije prepisivanja; hard link i datoteka samo za čitanje imaju definirano ponašanje.
- **Integracija s Neovimom:** test pokreće pravi `nvim` bez sučelja i provjerava otvaranje datoteke i
  prijem događaja preko RPC-a. Preskače se ako `nvim` nije instaliran.
- **Ručno [R]:** tipkovnica, fokus i izgled; popis u svakoj fazi.

## 7. Faze i kriteriji prihvaćanja

Sve faze idu na effort high (vidi `PROMPT.md`).

### Faza 0 — Spike (rizik)
Minimalna Wails aplikacija: xterm.js + PTY + `nvim`, bez ostatka sučelja. Kod je za bacanje (`spike/`).

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

Odluku idemo / ne idemo donosim ja.

### Faza 1 — Kostur, projekti i stablo
- [ ] struktura repozitorija iz `CLAUDE.md`, `task dev/build/test/lint`
- [ ] dodavanje i uklanjanje projekta (odabir foldera); konfiguracija preživljava ponovno pokretanje
- [ ] stablo prikazuje samo podržane vrste, s oznakom vrste; skrivene datoteke i folderi su izostavljeni
- [ ] nova datoteka, preimenovanje, brisanje u Trash
- [ ] stablo se osvježava kad se datoteke promijene izvana
- [ ] `theme/tokens.ts` postoji i iz njega izlaze CSS varijable

### Faza 2 — `.md` i `.txt`
- [ ] klik ili `↵` u stablu otvara dokument u Neovimu
- [ ] aktivni dokument je označen u stablu i kad se buffer promijeni iz Neovima
- [ ] alatna traka prikazuje „Izmijenjeno” / „Spremljeno” prema stanju buffera
- [ ] gašenje s nespremljenim izmjenama traži potvrdu
- [ ] xterm.js tema i Neovim colorscheme generirani su iz tokena
- [ ] **[R]** fokus: tipke iz terminala ne aktiviraju stablo, i obratno

### Faza 3 — `.docx` i `.doc`
- [ ] otvaranje `.docx`-a kroz radnu kopiju; slike se izdvajaju i vraćaju
- [ ] spremanje prema toku 4.3, svaki korak pokriven testom
- [ ] golden round-trip prolazi; `docs/known-losses.md` napisan iz stvarnih rezultata
- [ ] `.docx` s komentarima ili praćenim izmjenama otvara se samo za čitanje, s razlogom
- [ ] „Spremi kao kopiju” stvara novu datoteku i ne dira izvornik
- [ ] `.doc` se uvozi i sprema kao novi `.docx`
- [ ] simulirani prekid usred zapisa ostavlja izvornik netaknut
- [ ] oporavak radne kopije nakon rušenja
- [ ] **[R]** spremljeni `.docx` otvara se u Wordu ili Pagesu i izgleda kao izvornik

### Faza 4 — Pregled i tražilica
- [ ] pregled se pali i gasi (`␣p`, gumb), osvježava se nakon debouncea
- [ ] pregled je u sandboxed iframeu; skripta u dokumentu se ne izvršava (test)
- [ ] tražilica: `␣f` i `⌘P`, pretraga kroz sve projekte, `⌃p` sužava, `↵` otvara, `Esc` vraća fokus
- [ ] **[R]** izgled odgovara `docs/design/DESIGN.md`

### Faza 5 — Udobnost pisanja
- [ ] `init.lua`: prelamanje po riječima, kretanje po prikazanim recima, bez brojeva redaka za prozu (prekidač)
- [ ] provjera pravopisa hr + en **(provjeriti** odakle dolazi hrvatski rječnik i može li se isporučiti s aplikacijom**)**
- [ ] prečaci za naslov, bold, italic, listu (umeću Markdown oznake)
- [ ] neobavezni `user.lua` učitava se nakon ugrađene konfiguracije

### Faza 6 — Pakiranje
- [ ] `.app` se gradi i pokreće izvan razvojnog okruženja
- [ ] provjera `nvim` i `pandoc` pri pokretanju, s jasnom porukom
- [ ] ikona, naziv, verzija; potpisivanje za lokalnu upotrebu

## 8. Izvan opsega

WYSIWYG uređivanje, uređivanje tablica mišem, praćenje izmjena, komentari, PDF izvoz, pretraga
sadržaja (to radi Trag), sinkronizacija, Windows/Linux, AI značajke, tamna tema u prvoj verziji
(tokeni su imenovani tako da se doda kasnije).

## 9. Otvoreno / za provjeru

- Stanje Wails v3 (beta ili stabilno) i verzija koja se fiksira: uzeti istu kao u Tragu.
- Propusnost Wails događaja za izlaz terminala: mjeri se u fazi 0.
- Točne pandoc zastavice za izdvajanje slika, referentni dokument i prelamanje redaka: `pandoc --help`.
- Hrvatski rječnik za Neovim provjeru pravopisa.
