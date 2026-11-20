// Template document-v1: one template for an invoice, a corrected
// invoice and an offer, in German and in English.
//
// Every text comes from data.json, written by internal/pdf. A payload
// string reaches this file only as a JSON string value and is placed as
// text: nothing here evaluates it as markup or code. Keep it that way:
// never pass a data value to a function that reads a file or evaluates
// code.
//
// The layout is exact by construction: every column has a fixed width,
// only the item description takes the rest, and internal/pdf measures
// every word against its cell with the fonts' real advances before this
// file runs (layout.go holds the same widths). Every cell clips as a
// last resort, so text can never paint outside it.
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
// punctuation hangs past the end of a line (overhang), so a word ends
// where its measured width ends.
#set text(
  font: "Go",
  size: 9pt,
  lang: d.lang,
  top-edge: "ascender",
  bottom-edge: "descender",
  overhang: false,
)
#set par(leading: 0.15em, spacing: 0.9em)

#let lines(arr) = arr.join(linebreak())
// clipped sets body into the full width of its cell and cuts anything
// that would paint outside it.
#let clipped(body) = block(width: 100%, clip: true, body)

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
    ..d.footer.map(col => clipped(lines(col))),
  )
  if d.footer_text.len() > 0 {
    v(0.5mm)
    clipped(text(fill: muted, lines(d.footer_text)))
  }
}
#let page-number = text(size: 7pt, align(right, context [#d.page_label #counter(page).display() #d.page_of_label #counter(
    page,
  ).final().first()]))

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
    background: if d.draft {
      rotate(-40deg, text(size: 120pt, weight: "bold", fill: luma(232), d.watermark))
    },
  )
  body
}

// Letterhead.
#clipped(align(right, text(size: 14pt, weight: "bold", d.seller_name)))

#v(18mm)

// Sender line, then the address field on the left and the document data
// on the right.
#clipped(text(size: 6.5pt, fill: muted, underline(stroke: 0.3pt, d.sender_line)))
#v(1mm)
#grid(
  columns: (1fr, 88mm),
  column-gutter: 8mm,
  clipped(text(size: 10pt, lines(d.recipient_lines))),
  grid(
    columns: (32mm, 1fr),
    column-gutter: 3mm,
    row-gutter: 1.6mm,
    align: (left, right),
    ..d.meta.map(m => (clipped(text(fill: muted, m.label)), clipped(align(right, m.value)))).flatten(),
  ),
)

#v(14mm)

#text(size: 16pt, weight: "bold", d.title)

#if d.corrects_line != "" {
  clipped(par(d.corrects_line))
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

#{
  set text(size: 8pt)
  table(
    columns: (10mm, 1fr, 17mm, 18mm, 25mm) + vat-col(13mm) + (30mm,),
    align: (x, y) => aligns.at(x) + top,
    stroke: (x, y) => if y == 0 { (bottom: 0.6pt) } else { (bottom: 0.3pt + rule) },
    inset: (x: 1.5mm, y: 1.6mm),
    table.header(..head.enumerate().map(((i, h)) => clipped(align(aligns.at(i), text(weight: "bold", h))))),
    ..d
      .lines
      .map(l => {
        let desc = lines(l.description)
        if l.discount != "" {
          desc = [#desc \ #text(size: 7pt, fill: muted, l.discount)]
        }
        let cells = (l.position, desc, l.quantity, l.unit, l.unit_price) + vat-col(l.vat_rate) + (l.amount,)
        cells.enumerate().map(((i, c)) => clipped(align(aligns.at(i), c)))
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
          grid.cell(inset: (top: 1.8mm, bottom: 0.9mm), clipped(text(size: 10pt, weight: "bold", t.label))),
          grid.cell(inset: (top: 1.8mm, bottom: 0.9mm), clipped(align(right, text(
            size: 10pt,
            weight: "bold",
            t.value,
          )))),
        )
      } else {
        (clipped(t.label), clipped(align(right, t.value)))
      }
    })
    .flatten(),
)))

// Tax note, payment terms and notes, each kept together on one page.
#for s in d.sections {
  v(3mm)
  block(breakable: false, clipped({
    if s.title != "" {
      text(weight: "bold", s.title)
      linebreak()
    }
    lines(s.lines)
  }))
}
