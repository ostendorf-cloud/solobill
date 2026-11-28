# Datenschutzerklärung

Entwurf für solobill.de. Kein Rechtsrat. Der Eigentümer prüft den Text vor der Veröffentlichung.
Verbindlich ist die deutsche Fassung.

Diese Erklärung gilt für die Website solobill.de. Die Anwendung unter app.solobill.de hat eine
eigene Datenschutzerklärung.

Markierungen im Entwurf:

- `[OFFEN: ...]` ist eine Angabe, die der Eigentümer liefern muss.
- `[NICHT GEPRÜFT: ...]` ist eine Aussage, die der Code von solobill.de noch nicht belegt.

## 1. Kurzfassung

- Diese Website setzt keine Cookies.
- Sie lädt keine Dienste von Dritten und zeigt keine Werbung.
- Wenn Sie die Farbdarstellung umstellen, merkt sich Ihr Browser die Wahl auf Ihrem Gerät.
- Seitenaufrufe zählen wir auf dem Server mit Umami, ohne Skript und ohne Cookie.
- Wenn Sie sich in die Warteliste eintragen, speichern wir Ihre E-Mail-Adresse.
  Das tun wir nur nach Ihrer Bestätigung per E-Mail.

`[NICHT GEPRÜFT: Die Aussage "keine Dienste von Dritten" gilt, sobald der Code der Website
(Ordner landing/) geprüft ist. Er existiert noch nicht.]`

## 2. Verantwortlicher

Verantwortlich für die Datenverarbeitung auf dieser Website ist:

Simon Ostendorf
Frankenstr. 8
52070 Aachen

Telefon: +49 1573 0971784
E-Mail: `info@solobill.de`

## 3. Hosting

Wir betreiben die Website selbst, auf eigener Hardware in Deutschland. Es gibt keinen
Hosting-Anbieter und keinen Auftragsverarbeiter für das Hosting. Die Daten, die beim Aufruf der
Website anfallen, verarbeiten wir auf dieser Hardware.

Rechtsgrundlage ist Art. 6 Abs. 1 lit. f DSGVO. Unser berechtigtes Interesse ist ein
zuverlässiger und sicherer Betrieb der Website.

`[OFFEN: Bestätigen, dass solobill.de auf derselben eigenen Hardware läuft wie ostendorf.tech.]`

## 4. Cookies und lokaler Speicher

Diese Website setzt keine Cookies. Wir zeigen deshalb kein Cookie-Banner an.

Wenn Sie über den Schalter in der Kopfzeile die Farbdarstellung wählen (hell, dunkel oder wie
das System), speichert Ihr Browser die Wahl im lokalen Speicher (localStorage) unter dem
Schlüssel `govue-theme`. Die Wahl bleibt auf Ihrem Gerät und geht nicht an unseren Server. Sie
können sie in den Browser-Einstellungen löschen. Sie dient allein dazu, Ihre Auswahl beim
nächsten Besuch wiederherzustellen. Darüber hinaus lesen wir nichts von Ihrem Gerät aus.

## 5. Server-Logs

Beim Aufruf der Website schreibt unsere Anwendung eine Protokollzeile. Sie enthält:

- den Zeitpunkt der Anfrage
- die IP-Adresse
- die Anfragemethode, zum Beispiel GET oder POST
- das Routenmuster der Anfrage, nicht die vollständige Adresse mit Parametern
- den HTTP-Statuscode
- die übertragene Datenmenge
- die Bearbeitungsdauer

Die Anwendung protokolliert den Browsertyp (User-Agent) nicht.

`[NICHT GEPRÜFT: Die Felder des Zugriffsprotokolls der Website. Die Liste folgt dem
Zugriffsprotokoll von govue und ist am Code von solobill.de zu prüfen.]`

Der vorgeschaltete Ingress (der Reverse-Proxy) führt ein eigenes Zugriffsprotokoll. Es enthält die
IP-Adresse, Datum und Uhrzeit, die Anfragezeile, den HTTP-Statuscode, die übertragene
Datenmenge, den Browsertyp (User-Agent) und, falls Ihr Browser ihn sendet, den Referrer.

`[NICHT GEPRÜFT: Inhalt des Ingress-Protokolls für solobill.de. Die Angabe stammt aus der
Datenschutzerklärung von ostendorf.tech.]`

Wir führen diese Daten nicht mit anderen Datenquellen zusammen.

Rechtsgrundlage ist Art. 6 Abs. 1 lit. f DSGVO. Wir haben ein berechtigtes Interesse an einem
fehlerfreien und sicheren Betrieb und an der Abwehr von Missbrauch.

Speicherdauer: `[OFFEN: Aufbewahrungsdauer der Logs für solobill.de. Bei ostendorf.tech sind es
30 Tage. Der Eigentümer bestätigt, dass dieselbe Frist gilt.]`

## 6. Schutz vor Überlastung

Wir begrenzen, wie viele Anfragen eine IP-Adresse in kurzer Zeit stellen darf. Dazu zählt der
Server die Anfragen je Adresse (bei IPv6 je Netz der ersten 64 Bit). Der Zähler liegt nur im
Arbeitsspeicher. Er wird nicht auf Festplatte geschrieben und nicht an Dritte gegeben. Wenn eine
Adresse nicht mehr anfragt, wird der Zähler verworfen. Rechtsgrundlage ist Art. 6 Abs. 1 lit. f
DSGVO. Unser berechtigtes Interesse ist der Schutz vor Überlastung und Missbrauch.

## 7. Besucherstatistik mit Umami

Wir zählen Seitenaufrufe mit der Open-Source-Software Umami. Sie läuft auf unserer eigenen
Hardware in Deutschland. Es läuft kein Skript in Ihrem Browser, es wird kein Cookie gesetzt und
nichts auf Ihrem Gerät gespeichert oder ausgelesen. Stattdessen meldet unser Server den Aufruf
an Umami. Gemeldet werden:

- die aufgerufene Seite als Routenmuster, ohne Parameter
- die Herkunftsseite (Referrer) ohne Pfad und Parameter, wenn Ihr Browser sie sendet
- die bevorzugte Sprache Ihres Browsers
- Browsertyp, Browserversion und Betriebssystem (User-Agent)
- Ihre IP-Adresse, gekürzt auf das Netz /24 (IPv4) beziehungsweise /48 (IPv6)

Nicht gezählt werden Aufrufe mit dem Signal "Do Not Track" oder "Global Privacy Control",
Vorabrufe, Bots und Aufrufe aus privaten Adressbereichen.

`[NICHT GEPRÜFT: Dass die Website govue analytics so einsetzt, ist am Code von solobill.de zu
prüfen. Die Angaben folgen der Paketdokumentation von govue.]`

Speicherdauer der Statistikdaten: `[OFFEN: Bei ostendorf.tech sind es 14 Monate. Der
Eigentümer legt die Frist für solobill.de fest.]`

Rechtsgrundlage ist Art. 6 Abs. 1 lit. f DSGVO. Unser berechtigtes Interesse ist eine
statistische Auswertung der Nutzung, ohne Daten an Dritte zu geben. Sie können widersprechen,
indem Sie in Ihrem Browser "Do Not Track" oder "Global Privacy Control" einschalten oder uns
kontaktieren.

## 8. Warteliste

Sie können sich in eine Warteliste eintragen, um über den Start von solobill informiert zu
werden. Die Warteliste läuft mit doppelter Bestätigung (Double-Opt-in).

### Ablauf

1. Sie geben Ihre E-Mail-Adresse ein, setzen das Häkchen für die Einwilligung und senden das
   Formular ab.
2. Wir senden Ihnen eine einzige Bestätigungs-E-Mail. Sie enthält keine Werbung.
3. Erst wenn Sie den Link in dieser E-Mail öffnen und die Bestätigung abschicken, tragen wir Sie
   als bestätigt ein. Das bloße Öffnen des Links ändert nichts.
4. Nach Ihrer Bestätigung dürfen wir Ihnen die Start-Nachricht und Neuigkeiten zum Produkt
   senden. Das ist Werbung. Jede dieser E-Mails enthält einen Link zum Abmelden.

### Gespeicherte Daten

- Ihre E-Mail-Adresse, so wie Sie sie eingegeben haben
- der Zustand des Eintrags (unbestätigt oder bestätigt)
- der Zeitpunkt Ihrer Anfrage und der Zeitpunkt Ihrer Bestätigung
- die Version des Einwilligungstexts, den Sie gesehen haben
- ein Hinweis auf die Sprache der Seite
- solange der Eintrag unbestätigt ist, eine Prüfsumme (Hash) des Bestätigungslinks
- für jedes Postfach einen Zähler gesendeter Bestätigungs-E-Mails, der unter einem Schlüssel-Hash
  (HMAC) steht und keine Adresse enthält, aber pseudonym ist

`[NICHT GEPRÜFT: govue speichert keinen Zeitpunkt der Start-Nachricht. Die Löschung "12 Monate
nach der Start-Nachricht" muss die Website selbst umsetzen, zum Beispiel über den
Löschzeitpunkt. Der Code existiert noch nicht.]`

Wir speichern keine IP-Adresse, keinen User-Agent, keinen Namen und keinen Referrer. Wir
protokollieren keine Adresse und keinen Link-Schlüssel. Die Einwilligung weisen wir mit
Anfragezeit, Bestätigungszeit und Version des Einwilligungstexts nach.

### Speicherdauer und Löschung

- Ein unbestätigter Eintrag wird 48 Stunden nach der Anfrage gelöscht. Ein Löschlauf prüft das
  in kurzen Abständen. `[NICHT GEPRÜFT: Intervall des Löschlaufs, Vorgabe von govue: stündlich.]`
- Ein bestätigter Eintrag wird 12 Monate nach der Start-Nachricht gelöscht.
- Wenn Sie sich abmelden, löschen wir Ihren Eintrag sofort. Wir behalten nichts zurück, auch
  keinen Hash Ihrer Adresse. Tragen Sie sich danach erneut ein, beginnt der Eintrag neu.
- Der Zähler für Bestätigungs-E-Mails endet mit seinem Zählzeitraum (48 Stunden, danach höchstens
  10 Minuten). `[NICHT GEPRÜFT: Zählzeitraum und Abstand nach den Vorgaben von govue.]`

### Rechtsgrundlage

Wir verarbeiten Ihre Adresse auf Grundlage Ihrer Einwilligung nach Art. 6 Abs. 1 lit. a DSGVO.
Die Einwilligung für Werbe-E-Mails folgt zusätzlich aus § 7 Abs. 2 Nr. 3 UWG. Sie können sie
jederzeit mit Wirkung für die Zukunft widerrufen, über den Link in jeder E-Mail oder per
E-Mail an `info@solobill.de`. Die Rechtmäßigkeit der bisherigen Verarbeitung bleibt unberührt.

`[OFFEN: Rechtsgrundlage Einwilligung ist angenommen und vom Eigentümer nicht bestätigt.]`

### Schutz vor Missbrauch des Formulars

Ihr Browser löst beim Absenden eine kleine Rechenaufgabe (Proof of Work). Der Server speichert
die Aufgabe nicht. Er merkt sich nur eine Prüfsumme der gelösten Aufgabe im Arbeitsspeicher,
damit eine Lösung nur einmal gilt. Sie lässt sich keiner Person zuordnen. Es wird kein Cookie
gesetzt, nichts im Browser gespeichert und kein Dritter aufgerufen. Außerdem begrenzt der Server
im Arbeitsspeicher die Anfragen je IP-Adresse (bei IPv6 je Netz der ersten 64 Bit) und die
Zahl der E-Mails je Postfach. Rechtsgrundlage ist Art. 6 Abs. 1 lit. f DSGVO. Unser berechtigtes
Interesse ist der Schutz vor Spam und davor, dass Dritte unerwünschte E-Mails erhalten.

## 9. Versand von E-Mails

Die Bestätigungs-E-Mail und die späteren Nachrichten senden wir von `noreply@solobill.de`. Dazu
übergibt unsere Anwendung die E-Mail an den SMTP-Dienst, den wir auch für ostendorf.tech nutzen.
Dieser Dienst erhält Ihre Adresse und den Text der E-Mail.

`[OFFEN: Name und Betreiber des SMTP-Dienstes. Bei ostendorf.tech heißt es, der Mailserver wird
selbst betrieben. Der Eigentümer bestätigt, ob das auch für diesen Dienst gilt und ob ein
Auftragsverarbeitungsvertrag nötig ist.]`

`[NICHT GEPRÜFT: Ob der Dienst E-Mails für solobill.de signiert (DKIM) und ob SPF und DMARC
gesetzt sind.]`

## 10. Anmelden in der Anwendung

Der Knopf "Anmelden" führt zu app.solobill.de. Ein Klick darauf ist ein gewöhnlicher Link.
Diese Website sendet dabei keine Daten an die Anwendung. Dort gilt die Datenschutzerklärung der
Anwendung.

## 11. Ihre Rechte

Sie haben gegenüber uns die folgenden Rechte:

- Auskunft über Ihre gespeicherten Daten (Art. 15 DSGVO)
- Berichtigung (Art. 16 DSGVO)
- Löschung (Art. 17 DSGVO)
- Einschränkung der Verarbeitung (Art. 18 DSGVO)
- Datenübertragbarkeit (Art. 20 DSGVO)
- Widerspruch gegen Verarbeitungen auf Grundlage von Art. 6 Abs. 1 lit. f DSGVO, aus Gründen,
  die sich aus Ihrer besonderen Situation ergeben (Art. 21 DSGVO)
- Widerruf einer Einwilligung mit Wirkung für die Zukunft (Art. 7 Abs. 3 DSGVO)

Wenden Sie sich dazu an `info@solobill.de`.

Sie haben außerdem das Recht, sich bei einer Datenschutz-Aufsichtsbehörde zu beschweren.
`[OFFEN: Zuständige Aufsichtsbehörde mit Anschrift. Vorschlag: die Landesbeauftragte für
Datenschutz und Informationsfreiheit Nordrhein-Westfalen. Der Eigentümer bestätigt.]`

Eine Pflicht, Daten bereitzustellen, besteht nicht. Ohne Ihre E-Mail-Adresse können wir Sie nicht
auf die Warteliste setzen. Eine automatisierte Entscheidungsfindung einschließlich Profiling
findet nicht statt.

## 12. Verschlüsselung

Die Verbindung zu dieser Website ist per TLS verschlüsselt. Das erkennen Sie an "https://" und
am Schloss-Symbol in Ihrem Browser.

`[NICHT GEPRÜFT: TLS am Ingress für solobill.de, das Zertifikat kommt aus dem Cluster.]`

## 13. Änderungen

Wir passen diese Erklärung an, wenn sich die Website oder die Rechtslage ändert.

Stand: `[OFFEN: Datum der Veröffentlichung]`
