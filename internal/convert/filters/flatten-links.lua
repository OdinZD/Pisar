-- A link inside a link is not Markdown: the inner one comes back as literal text.
function Link(el)
  el.content = el.content:walk({ Link = function(inner) return inner.content end })
  return el
end
