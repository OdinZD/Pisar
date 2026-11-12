---
lang: hr-HR
---

# Dokument s grafom

Odlomak prije grafa.

```{=openxml}
<w:p><w:r><w:t xml:space="preserve">Graf slijedi: </w:t></w:r><w:r><w:drawing><wp:inline xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"><wp:extent cx="406400" cy="406400"/><wp:docPr id="77" name="Chart 1"/><a:graphic xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/chart"><c:chart xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" r:id="rIdChart1"/></a:graphicData></a:graphic></wp:inline></w:drawing></w:r><w:r><w:t xml:space="preserve"> i tekst iza njega.</w:t></w:r></w:p>
```

Odlomak poslije grafa. Veza `rIdChart1` ne pokazuje ni na što: bitan je element, a ne dio `word/charts/`.
