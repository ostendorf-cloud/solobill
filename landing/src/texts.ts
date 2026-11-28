import type { LocaleCode } from '@ostendorf-cloud/govue'

// The texts of the shell around the pages: the brand, the navigation, the
// placeholders. The marketing copy is a later issue (#156) and the legal
// texts are the owner's. govue ships the texts of its own components.

export interface Texts {
  brand: string
  nav: { home: string; login: string }
  home: { title: string; lead: string; note: string }
  legalPlaceholder: { heading: string; text: string }
  notFound: { title: string; lead: string; back: string }
}

export const texts: Record<LocaleCode, Texts> = {
  de: {
    brand: 'solobill',
    nav: { home: 'Start', login: 'Anmelden' },
    home: {
      title: 'Buchhaltung für Einzelunternehmer',
      lead: 'Rechnungen, Belege und die EÜR an einem Ort.',
      note: 'Diese Seite ist ein Platzhalter und wird noch mit Inhalt gefüllt.',
    },
    legalPlaceholder: {
      heading: 'Platzhalter',
      text: 'Dies ist ein Platzhalter und kein rechtlicher Text. Der Betreiber ergänzt hier seinen eigenen Text.',
    },
    notFound: {
      title: 'Seite nicht gefunden',
      lead: 'Diese Adresse gibt es nicht.',
      back: 'Zur Startseite',
    },
  },
  en: {
    brand: 'solobill',
    nav: { home: 'Home', login: 'Log in' },
    home: {
      title: 'Bookkeeping for sole traders',
      lead: 'Invoices, receipts and the annual profit statement in one place.',
      note: 'This page is a placeholder and its content follows.',
    },
    legalPlaceholder: {
      heading: 'Placeholder',
      text: 'This is a placeholder and no legal text. The operator adds their own text here.',
    },
    notFound: {
      title: 'Page not found',
      lead: 'This address does not exist.',
      back: 'Back to the start page',
    },
  },
}
