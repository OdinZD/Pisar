---
lang: hr-HR
---

# Dokument s izravnim oblikovanjem

Običan odlomak, bez izravnog oblikovanja.

Odlomak s **podebljanim**, *nagnutim* i `neproporcionalnim` tekstom te [poveznicom](https://example.com/primjer).

```{=openxml}
<w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:r><w:t>Centrirani odlomak.</w:t></w:r></w:p>
<w:p><w:pPr><w:jc w:val="right"/></w:pPr><w:r><w:t>Desno poravnati odlomak.</w:t></w:r></w:p>
<w:p><w:r><w:rPr><w:color w:val="FF0000"/></w:rPr><w:t xml:space="preserve">Crveni tekst </w:t></w:r><w:r><w:rPr><w:sz w:val="40"/></w:rPr><w:t xml:space="preserve">veliki tekst </w:t></w:r><w:r><w:rPr><w:rFonts w:ascii="Courier New" w:hAnsi="Courier New"/></w:rPr><w:t>drugi font</w:t></w:r></w:p>
<w:p><w:r><w:rPr><w:highlight w:val="yellow"/></w:rPr><w:t xml:space="preserve">istaknuti tekst </w:t></w:r><w:r><w:rPr><w:u w:val="single"/></w:rPr><w:t xml:space="preserve">podcrtano </w:t></w:r><w:r><w:rPr><w:strike/></w:rPr><w:t xml:space="preserve">precrtano </w:t></w:r><w:r><w:rPr><w:vertAlign w:val="superscript"/></w:rPr><w:t>gore</w:t></w:r><w:r><w:rPr><w:smallCaps/></w:rPr><w:t xml:space="preserve"> kapitalke</w:t></w:r></w:p>
<w:p><w:pPr><w:ind w:left="1440"/></w:pPr><w:r><w:t>Uvučeni odlomak.</w:t></w:r></w:p>
<w:p><w:pPr><w:spacing w:line="480" w:lineRule="auto"/></w:pPr><w:r><w:t>Odlomak s dvostrukim proredom.</w:t></w:r></w:p>
<w:p><w:r><w:t>Prije prijeloma stranice.</w:t></w:r><w:r><w:br w:type="page"/></w:r><w:r><w:t>Nakon prijeloma stranice.</w:t></w:r></w:p>
<w:p><w:pPr><w:pageBreakBefore/></w:pPr><w:r><w:t>Odlomak s prijelomom stranice ispred.</w:t></w:r></w:p>
<w:p><w:r><w:t>Stupac A</w:t></w:r><w:r><w:tab/></w:r><w:r><w:t>nakon tabulatora</w:t></w:r></w:p>
<w:p><w:r><w:t xml:space="preserve">Stranica </w:t></w:r><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText>PAGE</w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>1</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>
<w:tbl><w:tblPr><w:tblW w:w="5000" w:type="pct"/><w:tblBorders><w:top w:val="single" w:sz="12" w:color="FF0000"/></w:tblBorders></w:tblPr><w:tr><w:tc><w:tcPr><w:shd w:val="clear" w:fill="FFFF00"/></w:tcPr><w:p><w:r><w:t>žuta ćelija</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>obična ćelija</w:t></w:r></w:p></w:tc></w:tr></w:tbl>
```

Završni odlomak.
