# Changelog

## v0.1.0 (2026-11-21)

### Features

- **web:** add the document editor, issue, correction and payment views ([#122](https://github.com/ostendorf-cloud/solobill/pull/122))
- **web:** add the asset register and the depreciation schedule ([#118](https://github.com/ostendorf-cloud/solobill/pull/118))
- **web:** add the expense capture and list views ([#115](https://github.com/ostendorf-cloud/solobill/pull/115))
- **assets:** add the asset register and the depreciation schedule ([#116](https://github.com/ostendorf-cloud/solobill/pull/116))
- **payments:** record payments and split their VAT share ([#117](https://github.com/ostendorf-cloud/solobill/pull/117))
- **web:** add app shell, customers and catalogue views ([#98](https://github.com/ostendorf-cloud/solobill/pull/98))
- **documents:** draft, preview, issue and correct documents ([#100](https://github.com/ostendorf-cloud/solobill/pull/100))
- **expenses:** add expenses, other income and categories backend ([#99](https://github.com/ostendorf-cloud/solobill/pull/99))
- **pdf:** render frozen payloads to PDF/A-3b with typst ([#91](https://github.com/ostendorf-cloud/solobill/pull/91))
- **files:** store receipts on S3 with a server side sha256 check ([#92](https://github.com/ostendorf-cloud/solobill/pull/92))
- **profile:** add seller profile and VAT regime history ([#93](https://github.com/ostendorf-cloud/solobill/pull/93))
- **api:** add customers and catalogue items endpoints ([#90](https://github.com/ostendorf-cloud/solobill/pull/90))
- **auth:** move the login onto govue v0.5.0 account, session and passkeys ([#87](https://github.com/ostendorf-cloud/solobill/pull/87))
- year end contract for the milestone (schema plan, API, payload v1, layout) ([#83](https://github.com/ostendorf-cloud/solobill/pull/83))
- **chart:** isolate the admin port with a NetworkPolicy ([#33](https://github.com/ostendorf-cloud/solobill/pull/33))
- **storage:** documents, lines and gapless numbering ([#26](https://github.com/ostendorf-cloud/solobill/pull/26))
- **storage:** customers and catalogue items ([#23](https://github.com/ostendorf-cloud/solobill/pull/23))
- **chart:** add a Gateway API HTTPRoute alongside the Ingress ([#18](https://github.com/ostendorf-cloud/solobill/pull/18))
- the solobill foundation, tenancy, auth and deployment ([#3](https://github.com/ostendorf-cloud/solobill/pull/3))

### Bug Fixes

- **chart:** keep URLs out of chart errors and name the endpoint key ([#111](https://github.com/ostendorf-cloud/solobill/pull/111))
- **chart:** compare ports as integers in the mismatch guards ([#110](https://github.com/ostendorf-cloud/solobill/pull/110))
- **storage:** keep foreign key errors raw in the expense write paths ([#114](https://github.com/ostendorf-cloud/solobill/pull/114))
- **chart:** cover the last three guards and name a bad URL port ([#37](https://github.com/ostendorf-cloud/solobill/pull/37))
- **deps:** update module golang.org/x/crypto to v0.59.0 ([#85](https://github.com/ostendorf-cloud/solobill/pull/85))
- **chart:** make validate-chart.sh prove its own fail guards fire ([#32](https://github.com/ostendorf-cloud/solobill/pull/32))
- install corepack in the web build stage ([#24](https://github.com/ostendorf-cloud/solobill/pull/24))
- **httpapi:** move metrics and probes onto an admin listener ([#19](https://github.com/ostendorf-cloud/solobill/pull/19))
- **renovate:** pinDigests is a config key, not a preset ([#7](https://github.com/ostendorf-cloud/solobill/pull/7))
