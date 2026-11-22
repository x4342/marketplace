# Marketplace policy review

This is an owner review draft for a simulator. The application already offers reports for listings, seller accounts, chats and reviews, plus order disputes, case history, and appeals. Before user-facing publication, the owner must approve the rules below with the appropriate jurisdiction and legal review. The open decisions remain in [the decision register](open-decisions.md).

## Content, support and disputes

| Topic | Sandbox behavior | Owner decisions before publication |
| --- | --- | --- |
| Goods and sellers | Categories cover physical and digital test items. The app accepts reports and supports listing delisting and account restrictions. | Define permitted lawful goods, prohibited categories and conduct, jurisdictions, seller eligibility, and how abuse reports and takedowns are handled. X01. |
| Buyer support | Order participants can open a typed dispute and submit text evidence. An independent moderator can record a simulated outcome and a different moderator can review an appeal. | Define evidence standards, contact and response expectations, return/refund windows, appeal deadlines and referral resolution, notice obligations, and seller protection. X01 and X03. |
| Payment and payouts | Prices are in simulation USD/EUR, provider assets use TEST, and payout approval is separate from settlement. | Approve provider, assets and networks, fees, tax, FX, limits, confirmation rules, and payout operations. X01, X02, X04 and X05. |
| Access and accessibility | Server-rendered web workflows run on loopback. Accessibility checks target the documented responsive workflows. | Decide Onion and/or web availability, branding, support channels, and the accessibility sign-off owner. X07. |

## Retention decisions

The emulator stores the minimum data it currently needs for its workflows. Dates, conditions, legal exceptions, role-specific access, backup expiry and deletion authority in the table below require owner approval. No automatic deletion schedule was activated by M9.

| Record | Restricted use today | Retention and deletion decisions |
| --- | --- | --- |
| Shipping addresses and handoff data | Physical fulfillment. Shipping addresses stay outside finance and moderator dispute views. | Determine how soon fulfillment details can be removed after delivery, how disputes affect expiry, and when a backup copy expires. X03 and X06. |
| Chat and content reports | Participant conversation, reported reason in moderator queue, and append-only action history. | Determine retention for ordinary messages, appeal evidence, and action reasons, including restricted legal holds and user requests. X01, X03 and X06. |
| Digital files, entitlements and case evidence | Test files are private, buyer entitlements are access checked, and case text is visible to authorized participants and moderators. | Confirm upload formats, approved malware checks, file version retention, entitlement expiry, and evidence access after case closure. X03 and X06. |
| Accounts, sessions and recovery codes | Auth records and digests support revocation and account recovery. Sessions expire and consumed codes cannot be reused. | Determine cleanup of expired digests, account closure handling, recovery incident evidence, and security log retention. X01 and X06. |
| Orders, payouts, journals and audit events | Orders and simulated accounting retain immutable history. Corrections use compensating entries. | Determine statutory retention, privacy handling, audit access, backup retention, and approved correction or redaction procedure that preserves required accounting integrity. X01, X03, X05 and X06. |
