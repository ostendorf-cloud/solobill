# Datenschutzerklärung der Anwendung solobill

Entwurf. Kein Rechtsrat. Der Eigentümer prüft den Text vor jeder öffentlichen Registrierung.
Verbindlich ist die deutsche Fassung.

Diese Erklärung gilt für die Anwendung unter `app.solobill.de`. Sie beschreibt, was solobill über
seine Nutzer selbst verarbeitet. Für die Daten, die ein Nutzer über seine eigenen Kunden im Dienst
speichert, ist der Nutzer verantwortlich, und solobill ist sein Auftragsverarbeiter, siehe die
Vereinbarung zur Auftragsverarbeitung. Für die Website solobill.de gilt eine eigene Erklärung.

Markierungen im Entwurf:

- `[OFFEN: ...]` ist eine Angabe, die der Eigentümer liefern muss.
- `[NICHT GEPRÜFT: ...]` ist eine Aussage, die der Code oder das Chart nicht belegt.

## 1. Kurzfassung

- Wir speichern Ihre Kontodaten, um Sie anzumelden und den Dienst zu erbringen.
- Wir setzen nur Cookies, die für die Anmeldung nötig sind. Es gibt keine Werbe- und keine
  Analyse-Cookies.
- Wir betreiben den Dienst auf eigener Hardware in Deutschland, ohne Hosting-Anbieter.
- Wir nutzen keine Analyse-Dienste in der Anwendung. `[NICHT GEPRÜFT: Eine Suche im Code
  fand keine. Der Eigentümer bestätigt.]`

## 2. Verantwortlicher

Simon Ostendorf
Frankenstr. 8
52070 Aachen

E-Mail: `info@solobill.de`

`[OFFEN: Ansprechperson für Betroffene und, falls erforderlich, Datenschutzbeauftragter.]`

## 3. Welche Daten wir verarbeiten

### Konto

Wenn Sie ein Konto haben, speichern wir:

- Ihre E-Mail-Adresse
- Ihre Rolle (Inhaber oder Mitglied) und den Status des Kontos
- Ihr Passwort als Argon2id-Hash, nicht im Klartext, wenn Sie sich mit Passwort anmelden
- Ihr TOTP-Geheimnis für die zweite Stufe und den Zähler des zuletzt benutzten Codes
- Ihre Passkeys: Kennung, Name, öffentlicher Schlüssel, Zähler, Zeitpunkte der Nutzung
- die Kennung (Subjekt) Ihres Kontos beim Identitätsanbieter, wenn Sie sich über OIDC anmelden
- Zeitpunkte der Anlage und der Änderung des Kontos

`[NICHT GEPRÜFT: Die Liste folgt einer Durchsicht der Migrationen für Konten, Einladungen und
Passkeys. Sie wird vor der Veröffentlichung gegen die Schemata abgeglichen.]`

Das TOTP-Geheimnis liegt im Klartext in der Datenbank. Wir benennen das offen, weil es für
Ihre Risikoabwägung zählt.

### Einladung

Wenn jemand Sie einlädt, speichern wir Ihre E-Mail-Adresse, die Rolle, einen Hash des
Einladungs-Tokens, die Ablaufzeit und den Zeitpunkt der Einlösung. Eine Einladung gilt
standardmäßig sieben Tage.

### Verkäuferprofil und Buchungen

Ihr Verkäuferprofil (Name, Anschrift, Steuernummer, USt-IdNr., Bankverbindung, E-Mail, Telefon,
Website) und Ihre Buchungen sind Ihre Daten als Nutzer. Wir verarbeiten sie, um den Dienst zu
erbringen. Sie enthalten möglicherweise Daten Ihrer Kunden. Dafür gilt die Vereinbarung zur
Auftragsverarbeitung.

### Registrierung

`[OFFEN: Die Registrierung ist im Code noch nicht freigeschaltet (Issue 175). Der Text nennt
nach dem Konzept: die Version der Nutzungsbedingungen und den Zeitpunkt der Annahme. Weitere
Daten bei der Registrierung, etwa eine Bestätigungs-E-Mail, legt der Eigentümer fest.]`

### Server-Logs

Die Anwendung schreibt für jede Anfrage eine Protokollzeile mit Zeitpunkt, IP-Adresse, Methode,
Routenmuster, Statuscode, Datenmenge und Dauer. Sie enthält weder Ihre Eingaben noch den
Browsertyp.

`[NICHT GEPRÜFT: Die Felder folgen dem Zugriffsprotokoll von govue (middleware.AccessLog) und
sind gegen die Ausgabe der Anwendung zu prüfen.]`

Der Ingress führt ein eigenes Zugriffsprotokoll.
`[NICHT GEPRÜFT: Inhalt und Aufbewahrung des Ingress-Protokolls. Beides steht im
Infrastruktur-Repository.]`

Speicherdauer: `[OFFEN: Aufbewahrungsdauer der Logs. Bei ostendorf.tech sind es 30 Tage. Der
Eigentümer legt die Dauer für solobill fest.]`

### Schutz vor Überlastung und Missbrauch

Wir begrenzen Anmeldeversuche je IP-Adresse und je Konto. Die Zähler liegen im Arbeitsspeicher
und verfallen von selbst. Bei einer Anfrage wird die Adresse für die Begrenzung ausgewertet.
Bei IPv6 zählt das Netz der ersten 64 Bit.

## 4. Cookies und lokaler Speicher

Wir setzen nur technisch notwendige Cookies. Ein Einwilligungsbanner ist dafür nicht nötig
(§ 25 Abs. 2 Nr. 2 TDDDG).

| Cookie | Zweck | Dauer |
| --- | --- | --- |
| `__Host-session` | hält Ihre Sitzung nach der Anmeldung | bis zur Abmeldung oder zum Ablauf |
| `__Host-login` | speichert den Zustand einer laufenden Anmeldung über OIDC | kurz, nur für diesen Ablauf |
| `solobill_invite_oidc` | speichert eine laufende Einladung über OIDC | kurz, nur für diesen Ablauf |
| `__Host-govue-passkey` | speichert den Zustand einer laufenden Passkey-Anmeldung | kurz, nur für diesen Ablauf |

`[NICHT GEPRÜFT: Die Namen der ersten, zweiten und vierten Zeile stammen aus der
govue-Dokumentation und sind im laufenden Dienst zu prüfen. Ohne TLS (nur Entwicklung) fehlt
das Cookie-Präfix. Die Sitzungsdauer von govue ist 2 Stunden ohne Aktivität und höchstens
12 Stunden. solobill setzt eigene Werte nicht, das ist noch zu prüfen.]`

Der CSRF-Schutz nutzt kein eigenes Cookie. Das Token wird aus der Sitzung abgeleitet.

Im lokalen Speicher Ihres Browsers (localStorage) liegen Ihre Wahl der Farbdarstellung
(`govue-theme`) und Ihre Sprachwahl. Beides bleibt auf Ihrem Gerät und dient nur dazu, Ihre
Auswahl wiederherzustellen. `[NICHT GEPRÜFT: Name des Schlüssels für die Sprachwahl.]`

## 5. Rechtsgrundlagen

- Konto, Anmeldung, Sitzung und Erbringung des Dienstes: Vertrag, Art. 6 Abs. 1 lit. b DSGVO.
- Server-Logs und Schutz vor Missbrauch: berechtigtes Interesse, Art. 6 Abs. 1 lit. f DSGVO.
  Unser Interesse ist ein sicherer und stabiler Betrieb.
- Aufbewahrung von Buchungsdaten, soweit wir selbst gesetzlich verpflichtet sind:
  Art. 6 Abs. 1 lit. c DSGVO. `[OFFEN: Ob und welche Pflichten den Anbieter selbst treffen,
  prüft eine Rechtsanwältin oder ein Rechtsanwalt. Die Aufbewahrungspflichten des Nutzers
  (§ 147 AO, § 14b UStG) treffen den Nutzer, nicht den Anbieter.]`

## 6. Empfänger

- Wir betreiben den Dienst selbst auf eigener Hardware in Deutschland. Es gibt keinen
  Hosting-Anbieter.
- E-Mail: Die Anwendung versendet im Stand des Repositorys keine E-Mail. Die geplante
  Registrierung bringt sie. `[OFFEN: Name und Betreiber des SMTP-Dienstes, siehe Anlage 3 der
  Vereinbarung zur Auftragsverarbeitung.]`
- Identitätsanbieter: Melden Sie sich über OIDC an, nutzt die Anmeldung den Identitätsanbieter
  des Dienstes (derzeit Keycloak). `[OFFEN: Wer ihn betreibt und ob er für Nutzer der Anwendung
  offen ist. Der Eigentümer legt fest, ob die Anmeldung über OIDC für öffentliche Nutzer gilt.]`
- Weitere Empfänger gibt es nicht. `[OFFEN: Der Eigentümer bestätigt.]`

Eine Übermittlung in ein Drittland findet nicht statt. `[OFFEN: gilt auch für Sicherungen und
den E-Mail-Dienst.]`

## 7. Speicherdauer

- Konto und Buchungen: bis zum Ende des Vertrags. Ausgestellte Dokumente, gebuchte Zahlungen und
  Ausgaben und Belege sind unveränderlich. Die Anwendung löscht sie nicht.
  `[NICHT GEPRÜFT: Es gibt keinen Weg, ein Konto oder dessen Daten zu löschen. Der Eigentümer
  legt fest, wann und wie nach dem Vertragsende gelöscht wird, siehe Vereinbarung zur
  Auftragsverarbeitung, Ziffer 9.]`
- Einladungen: bis zur Einlösung oder zum Ablauf. `[NICHT GEPRÜFT: ob abgelaufene Einladungen
  gelöscht werden.]`
- Logs: `[OFFEN: Dauer]`
- Sicherungen: `[OFFEN: Rhythmus und Aufbewahrung]`

## 8. Ihre Rechte

Sie haben das Recht auf:

- Auskunft (Art. 15 DSGVO)
- Berichtigung (Art. 16 DSGVO)
- Löschung (Art. 17 DSGVO), soweit keine Aufbewahrungspflicht entgegensteht
- Einschränkung der Verarbeitung (Art. 18 DSGVO)
- Datenübertragbarkeit (Art. 20 DSGVO)
- Widerspruch gegen Verarbeitungen auf Grundlage von Art. 6 Abs. 1 lit. f DSGVO (Art. 21 DSGVO)

Wenden Sie sich an `info@solobill.de`. Daten Ihrer Kunden im Dienst betreffen den Nutzer als
Verantwortlichen. Wenn Sie als Kunde eines Nutzers Auskunft wünschen, wenden Sie sich an diesen
Nutzer.

Sie haben das Recht, sich bei einer Aufsichtsbehörde zu beschweren.
`[OFFEN: Zuständige Aufsichtsbehörde mit Anschrift. Vorschlag: die Landesbeauftragte für
Datenschutz und Informationsfreiheit Nordrhein-Westfalen. Der Eigentümer bestätigt.]`

Eine Pflicht, Daten bereitzustellen, besteht vertraglich: Ohne E-Mail-Adresse und
Zugangsdaten können wir kein Konto führen. Eine automatisierte Entscheidungsfindung einschließlich
Profiling findet nicht statt.

## 9. Sicherheit

Die Verbindung zur Anwendung ist per TLS verschlüsselt. `[NICHT GEPRÜFT: TLS am Ingress.]`
Weitere Maßnahmen stehen in Anlage 2 der Vereinbarung zur Auftragsverarbeitung.

## 10. Änderungen

Wir passen diese Erklärung an, wenn sich der Dienst oder die Rechtslage ändert.

Stand: `[OFFEN: Datum der Veröffentlichung]`
