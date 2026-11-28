# Nutzungsbedingungen für solobill

Entwurf. Kein Rechtsrat. Der Eigentümer und eine Rechtsanwältin oder ein Rechtsanwalt prüfen den
Text vor jeder öffentlichen Registrierung. Verbindlich ist die deutsche Fassung.

Markierungen im Entwurf:

- `[OFFEN: ...]` ist eine Entscheidung oder Angabe des Eigentümers.
- `[NICHT GEPRÜFT: ...]` ist eine Aussage, die der Code nicht belegt.

## 1. Geltungsbereich und Vertragspartner

1. Diese Bedingungen gelten für die Nutzung der Webanwendung solobill unter `app.solobill.de`
   (der "Dienst").
2. Anbieter und Vertragspartner ist Simon Ostendorf, Frankenstr. 8, 52070 Aachen,
   E-Mail `info@solobill.de` (der "Anbieter").
3. Nutzer ist, wer ein Konto im Dienst hat. `[OFFEN: Der Dienst richtet sich an Selbstständige
   und Einzelunternehmer (Unternehmer im Sinne von § 14 BGB). Der Eigentümer entscheidet, ob
   auch Verbraucher zugelassen sind, siehe Issue 28. Davon hängen Widerrufsrecht, Haftung und
   Preisangaben ab.]`
4. Abweichende Bedingungen des Nutzers gelten nicht, auch wenn der Anbieter ihnen nicht
   ausdrücklich widerspricht.
5. Die Bedingungen von ostendorf.tech gelten für den Dienst nicht.

## 2. Leistung

1. solobill unterstützt die Buchführung eines Einzelunternehmers. Der Dienst erstellt Angebote
   und Rechnungen, erfasst Zahlungen, Ausgaben und sonstige Einnahmen, verwaltet Belege und
   rechnet eine Einnahmen-Überschuss-Rechnung (EÜR) aus den Buchungen des Nutzers.
2. Der Dienst ist Software, die der Anbieter über das Internet bereitstellt. Der Nutzer
   erhält kein Recht an Quelltext oder Software. Er erhält ein einfaches, nicht übertragbares
   Recht, den Dienst für seine eigenen Zwecke zu nutzen, solange der Vertrag besteht.
3. Der Umfang folgt der Beschreibung des Dienstes zum Zeitpunkt der Registrierung. Der Anbieter
   darf den Dienst weiterentwickeln und Funktionen ändern, wenn der Kern der Leistung erhalten
   bleibt und die Änderung dem Nutzer zumutbar ist.
4. Der Anbieter schuldet keinen bestimmten Erfolg, zum Beispiel keine bestimmte steuerliche
   Behandlung.

## 3. Keine Steuer- und Rechtsberatung

1. Der Dienst unterstützt die Buchführung. Er ersetzt keine Steuerberatung und keine
   Rechtsberatung. Der Anbieter berät nicht.
2. Die Auswertungen, insbesondere die EÜR, werden aus den Eingaben des Nutzers berechnet. Der
   Nutzer prüft jeden Wert, bevor er ihn in eine Steuererklärung oder einen Beleg übernimmt.
3. Die Pflichten zur ordnungsmäßigen Buchführung, zur Aufbewahrung, zur Abgabe von Steuer- und
   Umsatzsteuererklärungen und zur Richtigkeit von Rechnungsangaben bleiben beim Nutzer. Der
   Nutzer ist für Inhalt und Richtigkeit seiner Rechnungen und Buchungen selbst verantwortlich.

## 4. Registrierung und Konto

1. Die Nutzung setzt ein Konto voraus. `[OFFEN: Wer sich registrieren darf und wie, hängt von
   Issue 28 und Issue 175 ab. Die Registrierung ist im Code noch nicht freigeschaltet.]`
2. Der Nutzer macht bei der Registrierung wahrheitsgemäße Angaben und hält sie aktuell.
3. Jeder Nutzer hat ein eigenes Konto. Die Zugangsdaten sind geheim zu halten und dürfen nicht an
   Dritte gegeben werden. Der Nutzer verwendet ein starkes Passwort und, soweit angeboten, eine
   zweite Stufe (TOTP) oder einen Passkey.
4. Der Nutzer informiert den Anbieter unverzüglich, wenn er einen Missbrauch seines Kontos
   vermutet.
5. Mit der Registrierung bestätigt der Nutzer diese Bedingungen. Der Anbieter hält die Version
   der Bedingungen und den Zeitpunkt der Bestätigung fest. `[NICHT GEPRÜFT: Das Speichern von
   Version und Zeitpunkt ist im Konzept für die Registrierung vorgesehen, aber nicht
   umgesetzt.]`

## 5. Preis und Zahlung

`[OFFEN: Das Preismodell steht nicht fest. Der Eigentümer legt fest: kostenlos oder
kostenpflichtig, Preis, Abrechnungszeitraum, Zahlungsart, Umsatzsteuer, Verzug und eine
Testphase. Bis dahin sieht der Entwurf keine Entgeltklausel vor.]`

## 6. Verfügbarkeit und Wartung

1. Der Anbieter bemüht sich um einen störungsfreien Betrieb. Eine bestimmte Verfügbarkeit
   schuldet er nicht. `[OFFEN: Ob der Eigentümer eine Verfügbarkeit in Prozent zusagt.]`
2. Wartungsarbeiten, Updates und Störungen außerhalb des Einflussbereichs des Anbieters können
   den Dienst zeitweise unterbrechen. Der Anbieter kündigt geplante Wartung nach Möglichkeit an.
3. Der Dienst läuft derzeit mit einer einzigen Instanz der Anwendung. Sitzungen liegen im
   Arbeitsspeicher. Ein Neustart, zum Beispiel für ein Update, meldet alle Nutzer ab. Gespeicherte
   Daten bleiben erhalten. `[NICHT GEPRÜFT: Der Stand folgt charts/solobill/README.md und
   ändert sich, wenn der Betrieb auf mehrere Instanzen wechselt.]`

## 7. Daten des Nutzers, Export, Aufbewahrung

1. Die Daten, die der Nutzer im Dienst erfasst, bleiben seine Daten. Der Anbieter nutzt sie nur,
   um den Dienst zu erbringen. Die Datenschutzerklärung und der Auftragsverarbeitungsvertrag des
   Dienstes regeln die Einzelheiten.
2. Ausgestellte Rechnungen, gebuchte Ausgaben und Zahlungen sowie hochgeladene Belege sind
   unveränderlich, damit die Unterlagen den Grundsätzen ordnungsmäßiger Buchführung entsprechen.
   Der Nutzer korrigiert sie durch eine neue Buchung, zum Beispiel eine Korrekturrechnung, nicht
   durch Änderung oder Löschung. Der Nutzer kann deshalb ausgestellte Dokumente und Belege nicht
   auf Verlangen löschen, solange die gesetzliche Aufbewahrungsfrist läuft.
3. Der Nutzer ist für die Aufbewahrung seiner Unterlagen selbst verantwortlich (§ 147 AO:
   zehn Jahre für Bücher, acht Jahre für Buchungsbelege, § 14b UStG: acht Jahre für
   Rechnungen). Der Anbieter empfiehlt, Rechnungen und Auswertungen regelmäßig selbst zu
   sichern.
4. Export: Der Nutzer kann Rechnungen als PDF abrufen und die EÜR eines Jahres als CSV
   exportieren. `[NICHT GEPRÜFT: Einen vollständigen Export aller Daten eines Kontos gibt es im
   Code nicht. Der Eigentümer entscheidet, ob und wie er ihn bis zur Registrierung baut oder
   den Export auf Anfrage zusagt.]`
5. Der Anbieter sichert die Datenbank regelmäßig. `[NICHT GEPRÜFT: Es gibt in diesem
   Repository keinen Nachweis über Sicherungen. Der Eigentümer nennt Rhythmus, Aufbewahrung der
   Sicherungen und Wiederherstellung. Eine Zusage zur Wiederherstellung gibt der Entwurf
   nicht.]`
6. Der Nutzer darf keine rechtswidrigen Inhalte hochladen und keine Schadsoftware. Belege sind
   Bilder oder PDF-Dokumente, die der Nutzer zur Buchführung braucht.

## 8. Pflichten des Nutzers, zulässige Nutzung

1. Der Nutzer nutzt den Dienst nur im Rahmen der Gesetze und dieser Bedingungen.
2. Nicht erlaubt sind:
   - Angriffe auf den Dienst, das Umgehen von Schutzmaßnahmen und das Überlasten des Dienstes
   - automatisierte Massenabfragen außerhalb der dokumentierten Schnittstellen
   - der Zugriff auf Daten anderer Nutzer oder der Versuch dazu
   - die Weitergabe des Zugangs an Dritte
   - die Nutzung für Rechnungen, die gegen Gesetze verstoßen
3. Bei einem Verstoß darf der Anbieter das Konto sperren, nach Möglichkeit nach vorheriger
   Abmahnung.

## 9. Haftung

`[OFFEN: Der Eigentümer und eine Rechtsanwältin oder ein Rechtsanwalt legen den Wortlaut fest.
Der Entwurf nennt den üblichen Rahmen für Verträge mit Unternehmern. Für Verbraucher gelten
engere Grenzen, siehe § 1 Absatz 3.]`

1. Der Anbieter haftet unbeschränkt bei Vorsatz und grober Fahrlässigkeit, bei Verletzung von
   Leben, Körper und Gesundheit, nach dem Produkthaftungsgesetz und soweit er eine Garantie
   übernommen hat.
2. Bei leichter Fahrlässigkeit haftet der Anbieter nur für die Verletzung einer wesentlichen
   Vertragspflicht. Das sind Pflichten, deren Erfüllung die ordnungsgemäße Durchführung des
   Vertrags erst möglich macht und auf die der Nutzer regelmäßig vertrauen darf. Die Haftung ist
   dann auf den vorhersehbaren, vertragstypischen Schaden begrenzt.
3. `[OFFEN: Höchstbetrag der Haftung, zum Beispiel das Entgelt eines Jahres. Ohne Preismodell
   nicht bestimmbar.]`
4. Der Anbieter haftet nicht für steuerliche Folgen, die sich aus Eingaben des Nutzers oder
   aus der Übernahme von Auswertungen ohne Prüfung ergeben, außer in den Fällen von Absatz 1.
5. Für den Verlust von Daten haftet der Anbieter nur, wenn der Nutzer seine Daten regelmäßig
   selbst gesichert hat und der Schaden bei dieser Sicherung nicht eingetreten wäre. `[OFFEN:
   Dieser Absatz ist besonders genau zu prüfen. Er soll nicht die Pflichten aus § 7 aushöhlen.]`
6. Die persönliche Haftung der Mitarbeiter und Erfüllungsgehilfen des Anbieters folgt Absatz 1
   bis 5.

## 10. Laufzeit und Kündigung

1. `[OFFEN: Laufzeit und Kündigungsfrist hängen vom Preismodell ab. Vorschlag für den Entwurf:
   Der Vertrag läuft auf unbestimmte Zeit. Der Nutzer kann jederzeit zum Ende des laufenden
   Abrechnungszeitraums, ohne Preismodell jederzeit, kündigen. Der Anbieter kann mit einer Frist
   von [OFFEN: Frist] kündigen.]`
2. Das Recht zur Kündigung aus wichtigem Grund bleibt unberührt.
3. Nach dem Ende des Vertrags kann der Nutzer seine Daten [OFFEN: Frist, zum Beispiel
   30 Tage] exportieren. Danach löscht der Anbieter das Konto und die Daten, soweit er sie nicht
   nach Gesetz aufbewahren muss. `[NICHT GEPRÜFT: Der Code kennt keine Löschung eines Kontos und
   keinen Löschweg für Belege im Objektspeicher. Die Aufbewahrung der Buchführungsdaten und
   die Löschung nach Ablauf der Pflicht bedürfen einer Entscheidung des Eigentümers, siehe
   Datenschutzerklärung des Dienstes.]`

## 11. Änderungen der Bedingungen

1. Der Anbieter darf diese Bedingungen ändern, wenn ein sachlicher Grund besteht, zum Beispiel
   eine geänderte Rechtslage, neue Funktionen oder eine Lücke im Text.
2. Er teilt die Änderung mindestens `[OFFEN: Frist, zum Beispiel sechs Wochen]` vor ihrem
   Inkrafttreten per E-Mail mit und weist auf das Recht zum Widerspruch und die Folgen hin.
3. Widerspricht der Nutzer nicht innerhalb der Frist, gelten die neuen Bedingungen. Widerspricht
   er, können beide Seiten den Vertrag zum Inkrafttreten kündigen.
   `[OFFEN: Eine Rechtsanwältin oder ein Rechtsanwalt prüft diese Zustimmungsfiktion. Sie ist
   bei Verbrauchern nur eingeschränkt zulässig.]`

## 12. Schlussbestimmungen

1. Es gilt deutsches Recht unter Ausschluss des UN-Kaufrechts. Gegenüber Verbrauchern gilt diese
   Rechtswahl nur, soweit ihnen nicht der Schutz zwingender Vorschriften ihres Aufenthaltsstaats
   entzogen wird. `[OFFEN: Eine Rechtsanwältin oder ein Rechtsanwalt prüft den Satz zu
   Verbrauchern, falls Verbraucher ausgeschlossen sind, entfällt er.]`
2. Gerichtsstand ist, soweit der Nutzer Kaufmann, juristische Person des öffentlichen Rechts oder
   öffentlich-rechtliches Sondervermögen ist, Aachen. `[OFFEN: Der Eigentümer bestätigt.]`
3. Sollte eine Bestimmung unwirksam sein, bleibt der Rest wirksam. An die Stelle der
   unwirksamen Bestimmung tritt die gesetzliche Regelung.
4. Die Vertragssprache ist Deutsch. Die englische Fassung dient der Verständlichkeit. Bei
   Abweichungen gilt die deutsche Fassung.

Stand: `[OFFEN: Datum und Version der Bedingungen]`
