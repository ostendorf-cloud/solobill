// Template document-v1: one template for an invoice, a corrected
// invoice and an offer, in German and in English.
//
// Every text comes from data.json, written by internal/pdf. A payload
// string reaches this file only as a JSON string value and is placed as
// text: nothing here evaluates it as markup or code. Keep it that way:
// never pass a data value to a function that reads a file or evaluates
// code.
//
// A shipped template version never changes its output again. A layout
// change is a new file with a new version name in render.go. The limits
// in printable.go are part of this version's contract.

#let d = json("data.json")

#let muted = luma(90)
#let rule = luma(190)
#let text-width = 165mm

#set document(title: d.document_title, author: d.seller_name, keywords: (d.template_version,))
#set text(font: "Go", size: 9pt, lang: d.lang)
#set par(leading: 0.55em, spacing: 0.9em)

#let lines(arr) = arr.join(linebreak())

// Break opportunities for narrow cells. A run of more than 32 characters
// without a space is longer than any ordinary word; it gets a zero width
// space after every `every` characters, so it wraps inside its cell
// instead of running over the next one. A run holding "@" or "://" (an
// e-mail address or a URL) is never broken: Typst breaks a URL after "/"
// and a word after "-" on its own. The text stays extractable; a copied
// broken run carries the invisible zero width spaces. The legal notes,
// the letterhead and the footer are never broken: printable.go bounds
// their words so they fit.
#let brk(s, every) = s.replace(regex("\S{33,}"), m => {
  if m.text.contains("@") or m.text.contains("://") {
    m.text
  } else {
    m.text.clusters().chunks(every).map(c => c.join()).join("\u{200B}")
  }
})
// The description cell, the address field and the meta values are about
// 60 mm wide: 16 characters of the widest glyph fit at 10 pt, 32 do not.
#let narrow(s) = brk(s, 16)

// The footer: seller address, contact, tax identity and bank, two
// columns wide, then the footer text. Its height is measured and the
// bottom margin grows with it, so no seller data is ever clipped and the
// body area is the same on every page.
#let footer-body = {
  set text(size: 7pt)
  line(length: 100%, stroke: 0.4pt + rule)
  grid(
    columns: (1fr, 1fr),
    column-gutter: 6mm,
    row-gutter: 1.5mm,
    ..d.footer.map(col => lines(col)),
  )
  if d.footer_text.len() > 0 {
    v(0.5mm)
    text(fill: muted, lines(d.footer_text))
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
#align(right, text(size: 14pt, weight: "bold", d.seller_name))

#v(18mm)

// Sender line, then the address field on the left and the document data
// on the right.
#text(size: 6.5pt, fill: muted, underline(stroke: 0.3pt, d.sender_line))
#v(1mm)
#grid(
  columns: (1fr, 88mm),
  column-gutter: 8mm,
  text(size: 10pt, lines(d.recipient_lines.map(narrow))),
  grid(
    columns: (auto, 1fr),
    column-gutter: 3mm,
    row-gutter: 1.6mm,
    align: (left, right),
    ..d.meta.map(m => (text(fill: muted, m.label), narrow(m.value))).flatten(),
  ),
)

#v(14mm)

#text(size: 16pt, weight: "bold", d.title)

#if d.corrects_line != "" {
  par(brk(d.corrects_line, 32))
}

#v(3mm)

// Items. The header row repeats on every page the table continues on.
#let vat-col(x) = if d.show_vat { (x,) } else { () }
#let head = (
  (d.columns.position, d.columns.description, d.columns.quantity, d.columns.unit, d.columns.unit_price)
    + vat-col(d.columns.vat_rate)
    + (d.columns.amount,)
)
#let aligns = (right, left, right, left, right) + vat-col(right) + (right,)

#table(
  columns: (auto, 1fr, auto, auto, auto) + vat-col(auto) + (auto,),
  align: (x, y) => aligns.at(x) + top,
  stroke: (x, y) => if y == 0 { (bottom: 0.6pt) } else { (bottom: 0.3pt + rule) },
  inset: (x: 1.5mm, y: 1.6mm),
  table.header(..head.map(h => text(weight: "bold", h))),
  ..d
    .lines
    .map(l => {
      let desc = lines(l.description.map(narrow))
      if l.discount != "" {
        desc = [#desc \ #text(size: 7.5pt, fill: muted, l.discount)]
      }
      (l.position, desc, l.quantity, l.unit, l.unit_price) + vat-col(l.vat_rate) + (l.amount,)
    })
    .flatten(),
)

// Totals, kept together on one page.
#v(2mm)
#align(right, block(breakable: false, width: 100mm, grid(
  columns: (1fr, auto),
  column-gutter: 4mm,
  inset: (y: 0.9mm),
  align: (left, right),
  ..d
    .totals
    .map(t => {
      if t.strong {
        (
          grid.hline(stroke: 0.6pt),
          grid.cell(inset: (top: 1.8mm, bottom: 0.9mm), text(size: 10pt, weight: "bold", t.label)),
          grid.cell(inset: (top: 1.8mm, bottom: 0.9mm), text(size: 10pt, weight: "bold", t.value)),
        )
      } else {
        (t.label, t.value)
      }
    })
    .flatten(),
)))

// Tax note, payment terms and notes, each kept together on one page.
#for s in d.sections {
  v(3mm)
  block(breakable: false, {
    if s.title != "" {
      text(weight: "bold", s.title)
      linebreak()
    }
    lines(s.lines)
  })
}
