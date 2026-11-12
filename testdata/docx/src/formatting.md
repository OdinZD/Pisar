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
<w:p><w:r><w:t xml:space="preserve">Crta slijedi: </w:t></w:r><w:r><mc:AlternateContent xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006"><mc:Choice Requires="wps"><w:drawing><wp:inline xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"><wp:extent cx="1440000" cy="0"/><wp:docPr id="78" name="Straight Connector 1"/><a:graphic xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingShape"><wps:wsp xmlns:wps="http://schemas.microsoft.com/office/word/2010/wordprocessingShape"><wps:cNvCnPr/><wps:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="1440000" cy="0"/></a:xfrm><a:prstGeom prst="line"><a:avLst/></a:prstGeom></wps:spPr><wps:bodyPr/></wps:wsp></a:graphicData></a:graphic></wp:inline></w:drawing></mc:Choice><mc:Fallback><w:pict><v:line id="Straight Connector 1" o:spid="_x0000_s1026" style="position:absolute" from="0,0" to="113pt,0"/></w:pict></mc:Fallback></mc:AlternateContent></w:r><w:r><w:t xml:space="preserve"> i tekst iza nje.</w:t></w:r></w:p>
```

Završni odlomak.
