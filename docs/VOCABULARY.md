# FamilyBoard Canonical Product Vocabulary

To maintain cognitive clarity and consistency across the touchscreen kiosk UI, mobile PWA, administrative console, and developer documentation, FamilyBoard standardizes on the following terminology:

---

## 1. Core Household Domain

| Term | Canonical Definition | Non-Standard / Deprecated Terms |
| :--- | :--- | :--- |
| **Household** | The sovereign family unit living together, owning the appliance and all stored private calendar data. | *Tenant, organization, workspace, group* |
| **Person** | A family member belonging to the household (e.g. "Alex", "Jordan"). Each person has a display name, an email address, and optional avatar color. | *User, member, profile, contact* |
| **Connected Account** | An authenticated external identity (Google Workspace/Gmail, Microsoft 365/Exchange, or Apple ID) linked to a specific Person. | *Login, link, OAuth profile, session* |
| **Calendar** | A single schedule feed belonging to an account (e.g. "Family", "Work", "School", "Soccer"). Can be sovereign local or provider-backed. | *Feed, stream, category, agenda* |
| **Provider** | An external cloud calendar service (Google, Microsoft 365, Apple iCloud/CalDAV) or the built-in sovereign local calendar engine. | *Integration, platform, vendor* |

---

## 2. Hardware & Client Domain

| Term | Canonical Definition | Non-Standard / Deprecated Terms |
| :--- | :--- | :--- |
| **Appliance** | The dedicated physical hardware (Radxa Dragon or Raspberry Pi) running the FamilyBoard operating system and local SQLite database. | *Server, box, host, device, node* |
| **Kiosk** | The full-screen touchscreen display interface rendered on the appliance display in the kitchen or entryway. | *Dashboard, terminal, display unit* |
| **Paired Device** | A family member's phone or tablet authenticated via QR-code camera pairing, holding a revocable capability token. | *Client, phone, remote session* |
| **FotoCube** | The ambient digital photo frame mode that activates during screen blanking or idle schedule hours. | *Screensaver, gallery, slideshow* |

---

## 3. Synchronization & Workflow Domain

| Term | Canonical Definition | Non-Standard / Deprecated Terms |
| :--- | :--- | :--- |
| **Event Candidate** | A calendar invitation or schedule change automatically parsed from incoming family email, awaiting one-tap human approval. | *Draft, proposal, suggestion, pend* |
| **Mutation** | A local calendar write action (create, edit, delete) queued for asynchronous push to upstream cloud providers. | *Outbox item, pending write, task* |
| **Conflict** | A simultaneous modification where the same calendar event was edited locally and remotely while offline. | *Collision, sync error, discrepancy* |
