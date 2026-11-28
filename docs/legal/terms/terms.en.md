# Terms of use for solobill

Draft. Not legal advice. The owner and a lawyer review the text before any public registration.
The German version is binding. This English text is a convenience translation.

Markers in the draft:

- `[OPEN: ...]` is a decision or fact the owner must supply.
- `[NOT VERIFIED: ...]` is a statement the code does not prove.

## 1. Scope and contract parties

1. These terms apply to the use of the web application solobill at `app.solobill.de` (the
   "Service").
2. The provider and contract party is Simon Ostendorf, Frankenstr. 8, 52070 Aachen, Germany,
   email `info@solobill.de` (the "Provider").
3. A user is anyone who has an account in the Service. `[OPEN: The Service targets sole traders
   (business users under Section 14 of the German Civil Code). The owner decides whether
   consumers are admitted too, see issue 28. Withdrawal rights, liability and price statements
   depend on it.]`
4. Terms of the user that deviate from these do not apply, even if the Provider does not
   expressly object.
5. The terms of ostendorf.tech do not apply to the Service.

## 2. The Service

1. solobill supports the bookkeeping of a sole trader. The Service creates offers and invoices,
   records payments, expenses and other income, manages receipts and computes a cash basis
   profit statement (EÜR) from the user's entries.
2. The Service is software that the Provider offers over the internet. The user gets no right
   to source code or software. The user gets a simple, non transferable right to use the Service
   for their own purposes while the contract lasts.
3. The scope follows the description of the Service at the time of registration. The Provider
   may develop the Service and change features if the core of the performance stays and the
   change is reasonable for the user.
4. The Provider owes no particular result, for example no particular tax treatment.

## 3. No tax or legal advice

1. The Service supports bookkeeping. It does not replace tax advice or legal advice. The
   Provider gives no advice.
2. The reports, in particular the EÜR, are computed from the user's entries. The user checks
   every figure before using it in a tax return or a document.
3. The duties of proper bookkeeping, of retention, of filing tax and VAT returns and of correct
   invoice details stay with the user. The user is responsible for the content and correctness
   of their invoices and entries.

## 4. Registration and account

1. Using the Service requires an account. `[OPEN: Who may register and how depends on issue 28
   and issue 175. Registration is not enabled in the code yet.]`
2. The user gives truthful details at registration and keeps them current.
3. Each user has their own account. The credentials stay secret and are not passed to third
   parties. The user uses a strong password and, where offered, a second factor (TOTP) or a
   passkey.
4. The user tells the Provider without delay if they suspect misuse of their account.
5. By registering, the user confirms these terms. The Provider records the version of the terms
   and the time of confirmation. `[NOT VERIFIED: Recording version and time is planned in the
   registration concept but not implemented.]`

## 5. Price and payment

`[OPEN: The pricing model is not set. The owner decides: free or paid, price, billing period,
payment method, VAT, default and a trial period. Until then the draft has no payment clause.]`

## 6. Availability and maintenance

1. The Provider aims for trouble free operation. It owes no particular availability.
   `[OPEN: Whether the owner promises an availability in percent.]`
2. Maintenance, updates and disruptions outside the Provider's control can interrupt the
   Service for a time. The Provider announces planned maintenance where possible.
3. The Service currently runs as a single instance of the application. Sessions live in working
   memory. A restart, for example for an update, signs all users out. Stored data stays.
   `[NOT VERIFIED: The statement follows charts/solobill/README.md and changes when operation
   moves to several instances.]`

## 7. User data, export, retention

1. The data the user records in the Service remains the user's data. The Provider uses it only
   to provide the Service. The privacy notice and the data processing agreement of the Service
   regulate the details.
2. Issued invoices, booked expenses and payments and uploaded receipts are immutable, so that
   the records meet the principles of proper bookkeeping. The user corrects them with a new
   entry, for example a correction invoice, not by editing or deleting. The user therefore
   cannot have issued documents and receipts deleted on request while the statutory retention
   period runs.
3. The user is responsible for retaining their records (Section 147 of the German Fiscal Code:
   ten years for books, eight years for accounting vouchers; Section 14b of the German VAT Act:
   eight years for invoices). The Provider recommends keeping your own regular copies of
   invoices and reports.
4. Export: the user can download invoices as PDF and export the EÜR of a year as CSV.
   `[NOT VERIFIED: The code has no complete export of all data of an account. The owner decides
   whether to build it before registration opens or to promise an export on request.]`
5. The Provider backs up the database regularly. `[NOT VERIFIED: This repository holds no
   evidence of backups. The owner names the schedule, the retention of backups and the
   restore. The draft makes no promise about restoring.]`
6. The user does not upload unlawful content or malware. Receipts are images or PDF documents
   the user needs for bookkeeping.

## 8. User duties, permitted use

1. The user uses the Service only within the law and these terms.
2. The following is not permitted:
   - attacks on the Service, bypassing protections and overloading the Service
   - automated bulk requests outside the documented interfaces
   - access to the data of other users or attempts to gain it
   - passing the access to third parties
   - use for invoices that break the law
3. In case of a breach the Provider may block the account, after a prior warning where
   possible.

## 9. Liability

`[OPEN: The owner and a lawyer set the wording. The draft names the usual frame for contracts
with business users. Consumers get narrower limits, see section 1 paragraph 3.]`

1. The Provider is liable without limit for intent and gross negligence, for injury to life,
   body and health, under the Product Liability Act and where it gave a guarantee.
2. For slight negligence the Provider is liable only for the breach of an essential contractual
   duty. These are duties whose fulfilment makes the proper performance of the contract
   possible and on which the user can regularly rely. Liability is then limited to the
   foreseeable damage typical for this kind of contract.
3. `[OPEN: Liability cap, for example the fee of one year. Cannot be set without a pricing
   model.]`
4. The Provider is not liable for tax consequences that result from the user's entries or from
   using reports without checking them, except in the cases of paragraph 1.
5. For loss of data the Provider is liable only if the user regularly backed up their data
   themselves and the damage would not have occurred with that backup. `[OPEN: Check this
   paragraph with particular care. It must not undermine the duties in section 7.]`
6. The personal liability of employees and agents of the Provider follows paragraphs 1 to 5.

## 10. Term and termination

1. `[OPEN: Term and notice depend on the pricing model. Proposal for the draft: the contract
   runs for an indefinite time. The user can terminate at any time, with a pricing model at the
   end of the current billing period. The Provider can terminate with a notice of [OPEN:
   period].]`
2. The right to terminate for good cause stays unaffected.
3. After the contract ends, the user can export their data for [OPEN: period, for example
   30 days]. After that the Provider deletes the account and the data, unless the law requires
   it to keep them. `[NOT VERIFIED: The code has no account deletion and no way to delete
   receipts in the object store. Retention of bookkeeping data and deletion after the duty
   ends need a decision of the owner, see the privacy notice of the Service.]`

## 11. Changes to the terms

1. The Provider may change these terms for an objective reason, for example a change in the law,
   new features or a gap in the text.
2. It announces the change by email at least `[OPEN: period, for example six weeks]` before it
   takes effect and points to the right to object and its consequences.
3. If the user does not object within the period, the new terms apply. If the user objects,
   either side can terminate the contract when the change takes effect. `[OPEN: A lawyer checks
   this deemed consent. It is only limitedly valid for consumers.]`

## 12. Final provisions

1. German law applies, excluding the UN Convention on Contracts for the International Sale of
   Goods. Towards consumers this choice of law applies only as far as it does not remove the
   protection of mandatory provisions of their country of residence. `[OPEN: A lawyer checks
   the sentence about consumers. If consumers are excluded, it goes.]`
2. The place of jurisdiction is Aachen where the user is a merchant, a legal entity under
   public law or a special fund under public law. `[OPEN: The owner confirms.]`
3. If a provision is invalid, the rest stays valid. The statutory rule replaces the invalid
   provision.
4. The contract language is German. The English version serves understanding. If they differ,
   the German version applies.

Date: `[OPEN: Date and version of the terms]`
