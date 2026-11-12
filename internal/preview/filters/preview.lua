-- Makes pandoc's HTML safe to show in a frame. The Markdown reader is run
-- without raw TeX and raw attributes, but WITH raw HTML (a document converted
-- from .docx has "<!-- -->" between two lists, and that must not be shown as
-- text). So this filter is the layer the guarantee rests on, and it does what no
-- reader switch can:
--   * raw HTML, TeX and attribute content is dropped: a <script>, <style>,
--     <iframe>, comment or any other tag leaves no trace, not even as text;
--   * no link survives (a link becomes a span), so nothing can be followed;
--   * an image survives only when it is a local file of a known image type;
--   * every attribute is dropped except class (and a plain width and height on
--     an image). Pandoc copies any {key=value} of the Markdown, and the
--     attributes of a <div> or <span> written in HTML, to the element: onclick,
--     style="background:url(http://...)", data-include;
--   * a task list's checkbox stays a character and does not become an <input>.
--
-- Pisar passes two metadata values on the command line, and a value given there
-- overrides one in the document's own metadata block: "pisar-images" ("on" or
-- "off") and, when it is on, "pisar-resource-dir", the one folder images are
-- read from.
--
-- A picture whose file cannot be read there is replaced too. Left alone, pandoc
-- keeps the relative address in src, and a frame would then ask its own origin
-- for it. The same check keeps pandoc from finding a file in its working
-- directory, where it also looks.

local image_types = { png = true, jpg = true, jpeg = true, gif = true, webp = true, svg = true }

local function unescape(s)
  return (s:gsub("%%(%x%x)", function(h) return string.char(tonumber(h, 16)) end))
end

-- extension returns the lower-case extension of the last path component, or nil.
local function extension(s)
  local name = s:match("([^/\\]*)$")
  local ext = name:match("%.(%w+)$")
  return ext and ext:lower() or nil
end

-- Pandoc reads a local file named by the percent-decoded src, so a check of the
-- spelling alone is not enough: "%2e%2e/x.png" is "../x.png" and "%2fetc%2fhosts"
-- is "/etc/hosts". Every test is therefore applied to both forms.
local function is_local_image(src)
  if src == "" then return false end
  for _, s in ipairs({ src, unescape(src) }) do
    if s:find("%c") then return false end
    if s:match("^%a[%w+.-]*:") or s:match("^[/\\~]") then return false end
    for part in s:gmatch("[^/\\]+") do
      if part == ".." then return false end
    end
    -- Pandoc picks <img>, <video>, <audio> or <embed> by the extension; only
    -- <img> is wanted, and anything else is not a picture.
    if not image_types[extension(s)] then return false end
  end
  return true
end

-- readable: a regular, non-empty file that can be opened, which is the only kind
-- pandoc embeds. A folder opens too, but reading it fails.
local function readable(path)
  local f = io.open(path, "rb")
  if not f then return false end
  local data = f:read(1)
  f:close()
  return data ~= nil
end

local function plain_size(v)
  return v ~= nil and v:match("^[%d.]+[%a%%]*$") ~= nil
end

local function bare_attr(el)
  el.attr = pandoc.Attr("", el.attr.classes, {})
  return el
end

local images_on = false
local resource_dir = ""

local function Image(el)
  if not images_on or not is_local_image(el.src)
      or not readable(pandoc.path.join({ resource_dir, unescape(el.src) })) then
    return pandoc.Span(el.caption, { class = "missing-image" })
  end
  local kv = {}
  for _, k in ipairs({ "width", "height" }) do
    local v = el.attributes[k]
    if plain_size(v) then kv[#kv + 1] = { k, v } end
  end
  el.attr = pandoc.Attr("", el.attr.classes, kv)
  return el
end

local function Link(el)
  return pandoc.Span(el.content, { class = "link" })
end

local function drop() return {} end

-- The reader turns "- [ ] task" into a list item that starts with a ballot box
-- character, and the writer turns that into an <input type="checkbox">, a form
-- control. A box inside a span is not recognised, so it stays a character.
local function Str(el)
  if el.text == "\u{2610}" or el.text == "\u{2612}" then
    return pandoc.Span({ el }, { class = "task-box" })
  end
end

-- A picture alone in a paragraph is a figure whose caption repeats the
-- description. Once the picture is gone the description would show twice, so
-- the figure becomes an ordinary paragraph.
local function Figure(el)
  local c = el.content
  if #c == 1 and c[1].t == "Plain" and #c[1].content == 1 then
    local only = c[1].content[1]
    if only.t == "Span" and only.classes:includes("missing-image") then
      return pandoc.Para(c[1].content)
    end
  end
  return bare_attr(el)
end

local clean = {
  Str = Str,
  Image = Image,
  Link = Link,
  RawInline = drop,
  RawBlock = drop,
  Span = bare_attr,
  Div = bare_attr,
  Header = bare_attr,
  CodeBlock = bare_attr,
  Code = bare_attr,
  Figure = Figure,
  Table = bare_attr,
  TableHead = bare_attr,
  TableBody = bare_attr,
  TableFoot = bare_attr,
  Row = bare_attr,
  Cell = bare_attr,
}

function Pandoc(doc)
  local v = doc.meta["pisar-images"]
  images_on = v ~= nil and pandoc.utils.stringify(v) == "on"
  local d = doc.meta["pisar-resource-dir"]
  resource_dir = d ~= nil and pandoc.utils.stringify(d) or ""
  if resource_dir == "" then images_on = false end
  return doc:walk(clean)
end
