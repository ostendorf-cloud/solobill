// Template document-v2: one template for an invoice, a corrected
// invoice and an offer, in German and in English. It is document-v1
// plus the seller logo.
//
// The logo is the file "logo" next to this template, written by
// internal/pdf only after it checked the bytes against the SHA-256 frozen
// in the payload and found a PNG or a JPEG. Its name is fixed here and
// never comes from data. It sits in the top margin of the first page,
// left aligned with the text, at most 60 mm wide and 11 mm high. It is
// page background, outside the flow: every cell, every line break and
// every page break is the same with and without a logo.
//
// Every text comes from data.json, written by internal/pdf. A payload
// string reaches this file only as a JSON string value and is placed as
// text: nothing here evaluates it as markup or code. Keep it that way:
// never pass a data value to a function that reads a file or evaluates
// code.
//
// Typst is the authority on the layout. Every column has a fixed width
// (only the item description takes the rest) and every text sits in a
// `fit` cell. A fit cell records where it is and marks every character
// it prints; at the end of the document one check compares every mark
// with its cell and fails the compile with "overflow: <field path>" when
// a character lies outside. So an issued PDF never has text cut or
// running into a neighbour cell. The cell also clips, as a last resort.
// internal/pdf checks the same widths before Typst runs, conservatively,
// to name the field early.
//
// A shipped template version never changes its output again. A layout
// change is a new file with a new version name in render.go.

#let d = json("data.json")

#let muted = luma(90)
#let rule = luma(190)
#let text-width = 165mm

#set document(title: d.document_title, author: d.seller_name, keywords: (d.template_version,))
// A line's box spans the font's ascender to its descender, so a clipping
// cell never cuts a glyph; the leading is smaller to match. No
// punctuation hangs past the end of a line (overhang), so a character
// ends where its advance ends.
#set text(
  font: "Go",
  size: 9pt,
  lang: d.lang,
  top-edge: "ascender",
  bottom-edge: "descender",
  overhang: false,
)
#set par(leading: 0.15em, spacing: 0.9em)

// fit-debug adds each cell's height to its record, for the tests' clip
// check. The renderer never sets it.
#let fit-debug = false
#let fit-counter = counter("fit")

// marked prints s with a mark after every character, naming its cell and
// field. A mark is a zero width metadata element. The closing empty box
// keeps the last mark off the paragraph's end, where Typst places a
// trailing element at the line start instead of after the last glyph.
#let marked(id, path, s) = {
  let mark = metadata((id: id, path: path))
  [#s.clusters().map(c => if c == " " { c } else { [#c#mark<fit-char>] }).join()#box()]
}

// fit sets the content body(mark) builds into the full width of its cell.
// mark(path, text) prints a data string. The visible text carries no
// mark. A hidden, left aligned copy of it with a mark after every
// character lies on top: its line breaks are the visible text's, and
// overflow, whatever the alignment, shows as a mark right of the cell.
// (Typst positions marks inside an overflowing right aligned line
// wrongly, so the copy is left aligned.)
#let fit(flush-right: false, body) = {
  fit-counter.step()
  context {
    let id = fit-counter.get().first()
    let visible = body((path, s) => s)
    block(width: 100%, clip: true, {
      place(top + left, layout(size => context {
        let p = here().position()
        let h = if fit-debug { measure(block(width: size.width, visible)).height } else { 0pt }
        [#metadata((id: id, page: p.page, x: p.x, y: p.y, w: size.width, h: h))<fit-cell>]
      }))
      place(top + left, hide(block(width: 100%, body((path, s) => marked(id, path, s)))))
      if flush-right { align(right, visible) } else { visible }
    })
  }
}

// lines-of marks each line with its own field path.
#let lines-of(mark, paths, arr) = arr.enumerate().map(((i, s)) => mark(paths.at(i), s)).join(linebreak())

// The footer: seller address, contact, tax identity and bank in two
// columns, then the footer text. Its height is measured and the bottom
// margin grows with it, so no seller data is ever cut and the body area
// is the same on every page.
#let footer-body = {
  set text(size: 7pt)
  line(length: 100%, stroke: 0.4pt + rule)
  grid(
    columns: (1fr, 1fr),
    column-gutter: 6mm,
    row-gutter: 1.5mm,
    ..d.footer.enumerate().map(((i, col)) => fit(mark => lines-of(mark, d.footer_paths.at(i), col))),
  )
  if d.footer_text.len() > 0 {
    v(0.5mm)
    fit(mark => text(fill: muted, d.footer_text.map(s => mark("seller.footer_text", s)).join(linebreak())))
  }
}
#let page-number = text(size: 7pt, align(right, context [#d.page_label #counter(page).display() #d.page_of_label #counter(
    page,
  ).final().first()]))

// The logo scaled to fit 60 mm by 11 mm, keeping its aspect ratio.
#let logo = context {
  let img = image("logo")
  let natural = measure(img)
  let k = calc.min(60mm / natural.width, 11mm / natural.height)
  scale(x: k * 100%, y: k * 100%, reflow: true, img)
}

#show: body => context {
  let footer-height = measure(block(width: text-width, footer-body)).height
  set page(
    paper: "a4",
    margin: (left: 25mm, right: 20mm, top: 18mm, bottom: footer-height + 18mm),
    footer: {
      page-number
      v(-1mm)
      footer-body
    },
    footer-descent: 5mm,
    background: context {
      if d.draft {
        rotate(-40deg, text(size: 120pt, weight: "bold", fill: luma(232), d.watermark))
      }
      if d.logo and counter(page).get().first() == 1 {
        place(top + left, dx: 25mm, dy: 5mm, logo)
      }
    },
  )
  body
}

// Letterhead.
#fit(flush-right:true, mark => text(size: 14pt, weight: "bold", mark("seller.legal_name", d.seller_name)))

#v(18mm)

// Sender line, then the address field on the left and the document data
// on the right.
#fit(mark => text(size: 6.5pt, fill: muted, underline(stroke: 0.3pt, mark("seller.address", d.sender_line))))
#v(1mm)
#grid(
  columns: (1fr, 88mm),
  column-gutter: 8mm,
  fit(mark => text(size: 10pt, lines-of(mark, d.recipient_paths, d.recipient_lines))),
  grid(
    columns: (32mm, 1fr),
    column-gutter: 3mm,
    row-gutter: 1.6mm,
    align: (left, right),
    ..d
      .meta
      .map(m => (
        fit(mark => text(fill: muted, mark("template.meta", m.label))),
        fit(flush-right:true, mark => mark(m.path, m.value)),
      ))
      .flatten(),
  ),
)

#v(14mm)

#text(size: 16pt, weight: "bold", d.title)

#if d.corrects_line != "" {
  fit(mark => par(mark("corrects.number", d.corrects_line)))
}

#v(3mm)

// Items. Every column but the description has a fixed width. The header
// row repeats on every page the table continues on.
#let vat-col(x) = if d.show_vat { (x,) } else { () }
#let head = (
  (d.columns.position, d.columns.description, d.columns.quantity, d.columns.unit, d.columns.unit_price)
    + vat-col(d.columns.vat_rate)
    + (d.columns.amount,)
)
#let aligns = (right, left, right, left, right) + vat-col(right) + (right,)
#let fields = ("position", "description", "quantity_thousandths", "unit", "unit_price_cents") + vat-col(
  "vat_rate_permyriad",
) + ("net_after_line_discount_cents",)

#{
  set text(size: 8pt)
  table(
    columns: (10mm, 1fr, 16.5mm, 17.5mm, 25mm) + vat-col(12.5mm) + (27.5mm,),
    align: (x, y) => aligns.at(x) + top,
    stroke: (x, y) => if y == 0 { (bottom: 0.6pt) } else { (bottom: 0.3pt + rule) },
    inset: (x: 1.5mm, y: 1.6mm),
    table.header(
      ..head
        .enumerate()
        .map(((i, h)) => fit(flush-right:aligns.at(i) == right, mark => text(weight: "bold", mark("template.columns", h)))),
    ),
    ..d
      .lines
      .enumerate()
      .map(((n, l)) => {
        let path(f) = "lines[" + str(n) + "]." + f
        let values = (l.position, none, l.quantity, l.unit, l.unit_price) + vat-col(l.vat_rate) + (l.amount,)
        values
          .enumerate()
          .map(((i, v)) => fit(flush-right:aligns.at(i) == right, mark => {
            if i == 1 {
              l.description.map(s => mark(path("description"), s)).join(linebreak())
              if l.discount != "" {
                linebreak()
                text(size: 8pt, fill: muted, mark(path("line_discount"), l.discount))
              }
            } else {
              mark(path(fields.at(i)), v)
            }
          }))
      })
      .flatten(),
  )
}

// Totals, kept together on one page.
#v(2mm)
#align(right, block(breakable: false, width: 100mm, grid(
  columns: (1fr, 40mm),
  column-gutter: 4mm,
  inset: (y: 0.9mm),
  align: (left, right),
  ..d
    .totals
    .map(t => {
      if t.strong {
        (
          grid.hline(stroke: 0.6pt),
          grid.cell(inset: (top: 1.8mm, bottom: 0.9mm), fit(mark => text(
            size: 10pt,
            weight: "bold",
            mark(t.path, t.label),
          ))),
          grid.cell(inset: (top: 1.8mm, bottom: 0.9mm), fit(flush-right:true, mark => text(
            size: 10pt,
            weight: "bold",
            mark(t.path, t.value),
          ))),
        )
      } else {
        (fit(mark => mark(t.path, t.label)), fit(flush-right:true, mark => mark(t.path, t.value)))
      }
    })
    .flatten(),
)))

// Tax note, payment terms and notes, each kept together on one page.
#for s in d.sections {
  v(3mm)
  block(breakable: false, fit(mark => {
    if s.title != "" {
      text(weight: "bold", mark("template.sections", s.title))
      linebreak()
    }
    s.lines.map(line => mark(s.path, line)).join(linebreak())
  }))
}

// The check: every marked character lies inside its cell, else the
// compile fails and names the field.
#context {
  let cells = (:)
  for c in query(<fit-cell>) {
    cells.insert(str(c.value.id), c.value)
  }
  for m in query(<fit-char>) {
    let c = cells.at(str(m.value.id))
    let x = m.location().position().x
    assert(x <= c.x + c.w + 0.05pt and x >= c.x - 0.05pt, message: "overflow: " + m.value.path)
  }
}
