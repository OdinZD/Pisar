# Pisar — izgled („Radni stol + tražilica”)

Ovaj tekst je mjerodavan. `pisar.png` (izvoz artboarda „5 · Odabrano” s dizajn platna) je ilustracija.

## Raspored

```
┌───────────────────────────────────────────────────────────────────────┐
│ ● ● ●  Naziv dokumenta   Projekt / folder        Stanje [Novi] [Pregled] │ 48 px
├────────────┬──────────────────────────────┬───────────────────────────┤
│ PROJEKTI   │ Neovim (xterm.js)            │ Pregled                    │
│ ▾ Projekt  │                              │  ┌─────────────────────┐   │
│   ▾ folder │                              │  │ stranica            │   │
│     dok ▪  │──────────────────────────────│  │                     │   │
│   ▸ folder │ Tražilica (kad je otvorena)  │  └─────────────────────┘   │
│ ▸ Projekt  │──────────────────────────────│                            │
│ + Dodaj    │ statusna traka Neovima       │                            │
└────────────┴──────────────────────────────┴───────────────────────────┘
   240 px          rastezljivo, min 460 px        oko 400 px, pali/gasi
```

- Najmanja veličina prozora: 1000 × 600. Ispod 1100 px širine pregled se sam gasi.
- Kad je pregled ugašen, editor zauzima njegov prostor.
- Tražilica je panel na dnu stupca editora, iznad statusne trake. Ne prekriva stablo ni pregled.

## Tokeni

Jedini izvor je `frontend/src/theme/tokens.ts`. Imena su semantička, da se tamna tema doda kao drugi skup.

| Token | Vrijednost | Gdje |
|---|---|---|
| `bg.app` | `#F5F6F7` | pozadina, panel tražilice |
| `bg.chrome` | `#ECEEF0` | alatna traka, stablo |
| `bg.editor` | `#FFFFFF` | Neovim, stranica pregleda |
| `bg.desk` | `#E3E6EA` | podloga pregleda, statusna traka |
| `bg.cursorline` | `#EEF3FC` | redak kursora |
| `bg.selected` | `#DCE7FA` | odabrani red u tražilici |
| `border` | `#D5D9DE` | razdjelnici |
| `border.strong` | `#C3C8CE` | rubovi gumba i polja |
| `text` | `#1D2125` | glavni tekst |
| `text.muted` | `#5B6470` | sporedni tekst, oznake vrste |
| `text.status` | `#3C444D` | statusna traka |
| `text.linenr` | `#6B7480` | brojevi redaka |
| `accent` | `#1F5FD1` | odabir u stablu, primarni gumb, naslovi u editoru, oznaka moda |
| `accent.text` | `#FFFFFF` | tekst na akcentu |
| `syntax.list` | `#B45309` | oznake lista |
| `syntax.code` | `#0F6B3C` | kod u retku |
| `danger` | `#B42318` | poruke o grešci |

Kontrast teksta prema podlozi mora biti najmanje 4.5:1. Ako promijeniš boju, provjeri omjer.

### Tri potrošača tokena

| Potrošač | Kako |
|---|---|
| CSS | varijable generirane iz tokena pri pokretanju |
| xterm.js | objekt teme (pozadina, tekst, kursor, odabir, 16 ANSI boja) izveden iz tokena |
| Neovim | colorscheme generiran iz tokena; aplikacija ga zapisuje u svoju konfiguracijsku mapu pri pokretanju |

Statusnu traku i oznaku moda crta Neovim, ne Svelte.

## Tipografija

| Uloga | Font | Veličina / prored |
|---|---|---|
| Sučelje | Inter | 13 / 20 px; naslovi odjeljaka 11 px, 600, razmak slova 0.06em |
| Editor | JetBrains Mono | 13 / 21 px |
| Pregled | Source Serif 4 | tekst 14 / 22 px; h1 26 / 32; h2 18 / 24 |

Fontovi su lokalni (woff2). Font editora mora biti učitan prije otvaranja terminala, inače xterm.js
krivo izmjeri ćeliju.

## Komponente

**Alatna traka.** Lijevo naziv dokumenta (600) i putanja unutar projekta (prigušeno). Desno stanje
(„Spremljeno”, „Izmijenjeno”, „Samo za čitanje”), gumb „Novi dokument” (sekundarni) i „Pregled”
(prekidač; uključen je ispunjen akcentom). Gumbi su visoki 32 px, radijus 6 px. Kad je dokument samo
za čitanje, uz stanje stoje razlog i gumb „Spremi kao kopiju”.

**Stablo.** Projekti su podebljani redovi koji se šire i skupljaju. Folderi su prigušeni. Dokument
je red visine 28 px s nazivom bez ekstenzije i oznakom vrste desno (DOCX, MD, TXT, DOC; mono, 10 px).
Odabrani dokument ispunjen je akcentom. Na dnu je „+ Dodaj projekt”.

**Tražilica.** Gore oznaka „Dokumenti”, polje za upit i opseg („svi projekti” ili naziv projekta).
Rezultati: naziv s istaknutim podudaranjem (akcent, 600) i projekt desno. Najviše 8 redova, ostalo
se pomiče. Dolje su natuknice tipki.

**Pregled.** Podloga `bg.desk`, stranica bijela, najviše 420 px široka, unutarnji razmak 40 px,
blaga sjena. Sadržaj je u sandboxed iframeu sa stilom iz tokena.

**Prazno stanje.** Bez otvorenog dokumenta stupac editora prikazuje „Otvori dokument” i tipke
`␣f` / `⌘P`.

## Tipkovnica i fokus

| Tipka | Radnja | Tko je obrađuje |
|---|---|---|
| `␣f` | tražilica | Neovim mapiranje → Go → Svelte |
| `⌘P` | tražilica | aplikacija |
| `␣p` | pregled pali/gasi | Neovim mapiranje → Go → Svelte |
| `␣e` | fokus na stablo | Neovim mapiranje → Go → Svelte |
| `⌘S` | spremi | aplikacija → RPC |
| `⌘N` | novi dokument | aplikacija |
| `Esc` u tražilici ili stablu | fokus natrag u Neovim | Svelte |
| `j` / `k` / `↵` u stablu | kretanje i otvaranje | Svelte |

- Terminal ima fokus po zadanome i dobiva sve tipke osim prečaca s `⌘`.
- `Esc` u normal modu nikad ne zatvara panele ni prozor.
- Fokus je uvijek vidljiv: prsten u boji akcenta na elementima sučelja.

## Stanja koja treba nacrtati

Izmijenjeno / spremljeno · samo za čitanje s razlogom · konverzija u tijeku · greška konverzije ·
dijalog konflikta · prazno stanje · nedostaje `nvim` ili `pandoc` · tražilica bez rezultata.
