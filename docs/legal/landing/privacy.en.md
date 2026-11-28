# Privacy policy

Draft for solobill.de. Not legal advice. The owner reviews the text before it goes live.
The German version is binding.

This policy covers the website solobill.de. The application at app.solobill.de has its own
privacy notice.

Markers in the draft:

- `[OPEN: ...]` is a fact the owner must supply.
- `[NOT VERIFIED: ...]` is a statement the code of solobill.de does not prove yet.

## 1. Summary

- This website sets no cookies.
- It loads no third party services and shows no advertising.
- If you change the colour mode, your browser remembers the choice on your device.
- We count page views on the server with Umami, without a script and without a cookie.
- If you join the waitlist, we store your email address. We do this only after you confirm by
  email.

`[NOT VERIFIED: "No third party services" holds once the code of the website (folder landing/)
is checked. It does not exist yet.]`

## 2. Controller

The controller for the processing on this website is:

Simon Ostendorf
Frankenstr. 8
52070 Aachen, Germany

Phone: +49 1573 0971784
Email: `info@solobill.de`

## 3. Hosting

We run the website ourselves, on our own hardware in Germany. There is no hosting provider and
no processor for hosting. We process the data that arises when you open the website on this
hardware.

The legal basis is Art. 6(1)(f) GDPR. Our legitimate interest is a reliable and secure
operation of the website.

`[OPEN: Confirm that solobill.de runs on the same own hardware as ostendorf.tech.]`

## 4. Cookies and local storage

This website sets no cookies. We therefore show no cookie banner.

If you choose the colour mode with the switch in the header (light, dark or like the system),
your browser stores the choice in local storage (localStorage) under the key `govue-theme`. The
choice stays on your device and does not go to our server. You can delete it in your browser
settings. It only restores your choice on your next visit. We read nothing else from your
device.

## 5. Server logs

When you open the website, our application writes one log line. It contains:

- the time of the request
- the IP address
- the request method, for example GET or POST
- the route pattern of the request, not the full address with parameters
- the HTTP status code
- the number of bytes sent
- the processing time

The application does not log the browser type (user agent).

`[NOT VERIFIED: The fields of the website's access log. The list follows the govue access log
and must be checked against the code of solobill.de.]`

The ingress in front of it (the reverse proxy) keeps its own access log. It contains the IP
address, date and time, the request line, the HTTP status code, the number of bytes sent, the
browser type (user agent) and, if your browser sends it, the referrer.

`[NOT VERIFIED: Content of the ingress log for solobill.de. The statement comes from the privacy
policy of ostendorf.tech.]`

We do not merge this data with other data sources.

The legal basis is Art. 6(1)(f) GDPR. We have a legitimate interest in error free and secure
operation and in defending against abuse.

Retention: `[OPEN: Retention of the logs for solobill.de. For ostendorf.tech it is 30 days. The
owner confirms that the same period applies.]`

## 6. Protection against overload

We limit how many requests one IP address may send in a short time. For this the server counts
the requests per address (for IPv6 per network of the first 64 bits). The counter lives only in
working memory. It is not written to disk and not passed to third parties. When an address stops
sending requests, the counter is dropped. The legal basis is Art. 6(1)(f) GDPR. Our legitimate
interest is protection against overload and abuse.

## 7. Visitor statistics with Umami

We count page views with the open source software Umami. It runs on our own hardware in
Germany. No script runs in your browser, no cookie is set, and nothing is stored on or read
from your device. Instead our server reports the page view to Umami. It reports:

- the page viewed, as a route pattern without parameters
- the referring page (referrer) without path and parameters, if your browser sends it
- the preferred language of your browser
- browser type, browser version and operating system (user agent)
- your IP address, shortened to the /24 network (IPv4) or the /48 network (IPv6)

We do not count requests with the signal "Do Not Track" or "Global Privacy Control",
prefetches, bots and requests from private address ranges.

`[NOT VERIFIED: That the website uses govue analytics this way must be checked against the code
of solobill.de. The statements follow the package documentation of govue.]`

Retention of the statistics: `[OPEN: For ostendorf.tech it is 14 months. The owner sets the
period for solobill.de.]`

The legal basis is Art. 6(1)(f) GDPR. Our legitimate interest is a statistical evaluation of
use without passing data to third parties. You can object by switching on "Do Not Track" or
"Global Privacy Control" in your browser or by contacting us.

## 8. Waitlist

You can join a waitlist to learn when solobill launches. The waitlist uses double opt-in.

### Flow

1. You enter your email address, tick the consent box and submit the form.
2. We send you one confirmation email. It contains no advertising.
3. Only when you open the link in that email and submit the confirmation do we mark you as
   confirmed. Merely opening the link changes nothing.
4. After your confirmation we may send you the launch message and product news. This is
   advertising. Every such email contains a link to unsubscribe.

### Stored data

- your email address as you typed it
- the state of the entry (unconfirmed or confirmed)
- the time of your request and the time of your confirmation
- the version of the consent text you saw
- a hint about the language of the page
- while the entry is unconfirmed, a hash of the confirmation link
- per mailbox, a counter of confirmation emails sent. It is stored under a keyed hash (HMAC)
  and holds no address, but it is pseudonymous

`[NOT VERIFIED: govue stores no time of the launch message. The deletion "12 months after the
launch message" must be implemented by the website itself, for example through a deletion time.
The code does not exist yet.]`

We store no IP address, no user agent, no name and no referrer. We log no address and no link
key. We prove the consent with the request time, the confirmation time and the version of the
consent text.

### Retention and deletion

- An unconfirmed entry is deleted 48 hours after the request. A purge job checks this at short
  intervals. `[NOT VERIFIED: Interval of the purge job, govue default: hourly.]`
- A confirmed entry is deleted 12 months after the launch message.
- When you unsubscribe, we delete your entry at once. We keep nothing, not even a hash of your
  address. If you sign up again later, the entry starts anew.
- The counter for confirmation emails ends with its counting period (48 hours, then at most
  10 minutes after the last email). `[NOT VERIFIED: Counting period and gap follow the govue
  defaults.]`

### Legal basis

We process your address on the basis of your consent under Art. 6(1)(a) GDPR. Consent to
advertising emails also follows from Section 7(2) no. 3 of the German Act against Unfair
Competition (UWG). You can withdraw it at any time with effect for the future, through the link
in every email or by email to `info@solobill.de`. The lawfulness of the processing before the
withdrawal stays unaffected.

`[OPEN: The consent basis is assumed and not confirmed by the owner.]`

### Protection against abuse of the form

When you submit, your browser solves a small computing task (proof of work). The server does
not store the task. It only keeps a checksum of the solved task in working memory, so that a
solution counts once. It cannot be linked to a person. No cookie is set, nothing is stored in
the browser and no third party is called. The server also limits requests per IP address (for
IPv6 per network of the first 64 bits) and the number of emails per mailbox, in working memory.
The legal basis is Art. 6(1)(f) GDPR. Our legitimate interest is protection against spam and
against third parties receiving unwanted emails.

## 9. Sending emails

We send the confirmation email and later messages from `noreply@solobill.de`. Our application
hands the email to the SMTP service we also use for ostendorf.tech. That service receives your
address and the text of the email.

`[OPEN: Name and operator of the SMTP service. The ostendorf.tech policy says the mail server is
self hosted. The owner confirms whether this holds for this service and whether a data
processing agreement is needed.]`

`[NOT VERIFIED: Whether the service signs emails for solobill.de (DKIM) and whether SPF and
DMARC are set.]`

## 10. Logging in to the application

The button "Log in" leads to app.solobill.de. Clicking it is an ordinary link. This website
sends no data to the application when you do. The privacy notice of the application applies
there.

## 11. Your rights

You have the following rights against us:

- access to your stored data (Art. 15 GDPR)
- rectification (Art. 16 GDPR)
- erasure (Art. 17 GDPR)
- restriction of processing (Art. 18 GDPR)
- data portability (Art. 20 GDPR)
- objection to processing based on Art. 6(1)(f) GDPR, on grounds relating to your particular
  situation (Art. 21 GDPR)
- withdrawal of consent with effect for the future (Art. 7(3) GDPR)

Contact `info@solobill.de` for this.

You also have the right to lodge a complaint with a data protection supervisory authority.
`[OPEN: The competent supervisory authority with its address. Proposal: the State Commissioner
for Data Protection and Freedom of Information of North Rhine-Westphalia. The owner confirms.]`

You are not obliged to provide data. Without your email address we cannot put you on the
waitlist. No automated decision making including profiling takes place.

## 12. Encryption

The connection to this website is encrypted with TLS. You can tell by "https://" and the lock
symbol in your browser.

`[NOT VERIFIED: TLS at the ingress for solobill.de. The certificate comes from the cluster.]`

## 13. Changes

We adapt this policy when the website or the law changes.

Date: `[OPEN: Date of publication]`
