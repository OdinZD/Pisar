# Poznati gubici pri pretvorbi

Pisar Wordov dokument uređuje kao Markdown: `.docx` se pri otvaranju pretvara u radnu kopiju
(`pandoc`), a pri spremanju natrag. `.doc` se prije toga pretvara u `.docx` (`textutil`). Markdown ne
može opisati sve što Word može, pa se dio toga pri spremanju gubi. Ovdje je popis svega što je
izmjereno; **sve što se gubi, a nije na ovom popisu, je greška.**

Izmjereno s pandocom 3.13 i `textutil`-om iz macOS-a 27.2. Dokaz uz svaku stavku je probni dokument u
`testdata/docx/` (nastaje skriptom `scripts/make-fixtures.sh`), njegov golden Markdown u
`testdata/docx/golden/` i testovi u `internal/convert/`.

Prije svakog prvog prepisivanja u sesiji Pisar sprema sigurnosnu kopiju izvornika u
`~/Library/Application Support/Pisar/backups/` (zadnjih 10 po dokumentu), pa se svaki gubitak s ovog
popisa može vratiti.

## 1. Dokument se otvara samo za čitanje

Ovo bi pri spremanju nestalo iz dokumenta bez traga, pa Pisar takav dokument ne prepisuje. Alatna
traka kaže razlog i nudi „Spremi kao kopiju” (kopija nastaje iz Markdowna, dakle bez toga sadržaja;
izvornik ostaje netaknut).

| Sadržaj | Što pandoc napravi | Probni dokument |
|---|---|---|
| komentari | tekst komentara i oznaka raspona nestaju | `comments.docx` |
| praćene izmjene | umetnuti tekst se prihvati, obrisani nestane; zapis izmjene nestaje | `tracked-changes.docx` |
| tekstualni okvir | tekst okvira postaje običan odlomak, okvir nestaje | `textbox.docx` |
| graf, SmartArt, ugrađeni objekt | nestaje, ostaje samo okolni tekst | `chart.docx` |
| tablica koju Markdown ne može zapisati | izlazi kao sirovi HTML, koji se pri spremanju gubi | zaštita u kodu; pandoc 3.13 u grid tablicama podnosi i spojene ćelije, pa se nije dogodilo ni na jednom dokumentu |

Samo za čitanje su i dokumenti koje se ne smije prepisati iz drugih razloga: datoteka bez dozvole
pisanja, zaključana datoteka, datoteka s više imena (tvrde poveznice; atomski zapis bi ih razdvojio).

## 2. Gubi se pri spremanju (uz upozorenje u alatnoj traci)

Izravno oblikovanje. Dokument se uređuje i sprema, a alatna traka do prvog spremanja nabraja što je
u njemu nađeno. Probni dokument za sve stavke: `formatting.docx`.

| Oblikovanje | Nakon spremanja |
|---|---|
| poravnanje odlomka (centrirano, desno, obostrano) | lijevo |
| boja teksta | zadana |
| vrsta i veličina fonta | iz stila |
| prored i razmak prije/poslije odlomka | iz stila |
| uvlaka odlomka | u Markdownu je citat; u dokumentu dobiva stil „Block Text”, a ako ga dokument nema, postaje običan odlomak bez uvlake |
| prijelom stranice | postaje prijelom retka; „prijelom ispred odlomka” i prijelom odjeljka nestaju |
| tabulator | razmak |
| polja u tijelu (broj stranice, datum) | običan tekst s vrijednošću koju su imala |
| sadržaj (polje TOC) | običan tekst s poveznicama na naslove; brojevi stranica su zamrznuti i više se ne osvježavaju; naslov sadržaja gubi stil „TOC Heading” |
| crtani oblici bez teksta (linije, pravokutnici) | nestaju |
| sjenčanje i obrubi tablice | iz stila tablice |
| stilovi odlomka izvan osnovnih (npr. „Title”, „Quote”) | običan odlomak (nije u upozorenju: ne može se pouzdano otkriti) |

Alternativa koja čuva imenovane stilove (`docx+styles`) je odbačena: svaki odlomak omota u
`::: {custom-style="…"}`, što nije tekst za pisanje.

## 3. Mijenja se, a sadržaj ostaje

| Što | Promjena | Probni dokument |
|---|---|---|
| širine stupaca tablice | jednostavna tablica dobiva automatske širine; tablici sa složenim ćelijama širine se promijene jednom, pri prvom spremanju, i zatim miruju | `basic.docx`, `complex-table.docx` |
| slika | u Markdownu dobiva izričitu širinu i visinu; datoteka slike u dokumentu dobiva novo ime; položaj je uvijek „u retku s tekstom” | `basic.docx` |
| numerirana lista | u Markdownu je `1.  ` (dva razmaka); u dokumentu ista | `basic.docx` |
| jezik dokumenta u metapodacima | ne vidi se u Markdownu; ostaje preko referentnog dokumenta | `basic.docx` |
| zaglavlje i podnožje (i slika u njima) | ne vide se u Markdownu i ne mogu se uređivati; ostaju sačuvani preko referentnog dokumenta | `header-image.docx` |
| imena knjižnih oznaka (sidra) | pandoc im dodijeli nova imena; veze unutar dokumenta i dalje rade | mjereno ručno |
| fusnote | u Markdownu su definicije na kraju `doc.md`; redak dopisan odmah iza zadnje, bez praznog retka, postaje nastavak fusnote | `basic.docx`, `scripts/smoke.sh` |
| tipografski navodnici i crtice | ostaju doslovni (`„ ”`, `—`) | `basic.docx` |

## 4. Uvoz `.doc`

`textutil` iz starog formata zadrži tekst, a strukturu izgubi. Izvorni `.doc` se nikad ne prepisuje;
prvo spremanje stvara novi `.docx` pored njega, a alatna traka upozorava da je uvoz pojednostavio
strukturu. Probni dokument: `legacy.doc`.

| Što | Nakon uvoza |
|---|---|
| naslovi | običan tekst |
| poveznice | samo tekst poveznice |
| fusnote | oznaka i tekst fusnote nestaju |
| liste | odlomci s „•” (kao citat) |
| tablice | svaka ćelija zaseban odlomak |

`textutil` vraća uspjeh i kad ne uspije, pa Pisar uspjeh provjerava po izlaznoj datoteci. Datoteku
koja nije `.doc` (npr. običan tekst s tom ekstenzijom) `textutil` pretvori bez greške.

## 5. Svojstva datoteke

Spremanje zamjenjuje datoteku novom (privremena datoteka u istom folderu, pa preimenovanje). Prenose
se dozvole i prošireni atributi (oznake Findera, karantena, podaci o podrijetlu preuzimanja). Ne
prenose se:

- ACL-ovi, vlasnik i grupa (nova datoteka pripada korisniku koji sprema)
- vrijeme stvaranja
- identitet datoteke (inode): alias ili zabilješka koja pokazuje na staru datoteku može puknuti
- BSD zastavice osim zaključanosti (npr. „skriveno”)
- atribut `com.apple.provenance`, koji sustav postavlja sam

## 6. Što ostaje sačuvano

Naslovi, odlomci, podebljano, kurziv, isticanje (marker), podcrtano, precrtano, eksponent i indeks,
kapitalke, liste i ugniježđene liste, tablice (i sa spojenim ćelijama, više odlomaka i listama u
ćeliji), slike u tijelu (bajt-identične), fusnote, poveznice, formule, hrvatski dijakritici,
tipografski navodnici i crtice, zaglavlje i podnožje sa slikama, margine i veličina stranice, stilovi
dokumenta (preko referentnog dokumenta).

## 7. Mjerenje na stvarnim dokumentima

Pet dokumenata nastalih u Wordu (od 250 do 5300 riječi; dva imaju sadržaj, podnožje s brojem
stranice, fusnote i crtane linije). Mjereno 2026-11-12 na kopijama; dokumenti nisu u repozitoriju.

- Tekst je nakon kruga `.docx → .md → .docx` u svih pet isti, riječ po riječ.
- Podnožje s brojem stranice (polje `PAGE`), fusnote, veličina stranice i margine su sačuvani.
- Izgubljeno je točno ono iz odjeljka 2: obostrano poravnanje, fontovi i veličine, boje, prored,
  uvlake dugih citata, tabulatori, polje sadržaja, četiri crtane linije.
- **Nalaz koji je ispravljen:** stavka Wordovog sadržaja je poveznica koja sadrži drugu poveznicu
  (broj stranice). pandoc je to zapisao kao poveznicu u poveznici, što Markdown ne poznaje, pa je
  nakon spremanja u dokumentu pisalo npr. „Uvod [3](#uvod)”. Pisar zato pri čitanju ugniježđene
  poveznice svodi na jednu (Lua filter); probni dokument `toc.docx`.
- Nisu bili u uzorku: komentari, praćene izmjene, tekstualni okviri, tablice, slike, `.doc`. Za njih
  vrijede samo mjerenja na generiranim dokumentima.

## 8. Nije izmjereno

- Izgled spremljenog dokumenta u Wordu i Pagesu (ručna provjera).
- Krajnje bilješke, natpisi slika i tablica, unakrsne reference, višestupčani odjeljci, vodeni žig,
  slike s obtjecanjem teksta, oblici i crteži koji nisu slika ni tekstualni okvir.
