// One file per feature and locale, merged here under the feature's own
// namespace: locales/de/login.json becomes messages.de.login. A feature
// issue edits only its own two files; this index is not edited again by
// a feature issue (see AGENTS.md's conflict zone rule).
import deAcceptInvite from './de/acceptInvite.json'
import deAssets from './de/assets.json'
import deCatalogue from './de/catalogue.json'
import deCommon from './de/common.json'
import deCustomers from './de/customers.json'
import deDocuments from './de/documents.json'
import deExpenses from './de/expenses.json'
import deHome from './de/home.json'
import deImports from './de/imports.json'
import deLogin from './de/login.json'
import deProfile from './de/profile.json'
import deReports from './de/reports.json'
import enAcceptInvite from './en/acceptInvite.json'
import enAssets from './en/assets.json'
import enCatalogue from './en/catalogue.json'
import enCommon from './en/common.json'
import enCustomers from './en/customers.json'
import enDocuments from './en/documents.json'
import enExpenses from './en/expenses.json'
import enHome from './en/home.json'
import enImports from './en/imports.json'
import enLogin from './en/login.json'
import enProfile from './en/profile.json'
import enReports from './en/reports.json'

export const de = {
  common: deCommon,
  login: deLogin,
  home: deHome,
  acceptInvite: deAcceptInvite,
  profile: deProfile,
  customers: deCustomers,
  catalogue: deCatalogue,
  documents: deDocuments,
  expenses: deExpenses,
  assets: deAssets,
  reports: deReports,
  imports: deImports,
}

export const en = {
  common: enCommon,
  login: enLogin,
  home: enHome,
  acceptInvite: enAcceptInvite,
  profile: enProfile,
  customers: enCustomers,
  catalogue: enCatalogue,
  documents: enDocuments,
  expenses: enExpenses,
  assets: enAssets,
  reports: enReports,
  imports: enImports,
}
