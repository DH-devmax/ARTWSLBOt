# Agents-Approval architecture

Generated from code evidence; not production acceptance.

```mermaid
%% Generated from source evidence. Do not hand edit.
%% Import / declaration edges are not proof of runtime activation.
flowchart LR
  n5bc11618c1ea["Self approval / explicit action grant / production gate"]
  n9b43c2b9988b["Trusted fixed adapter (not model-callable)"]
  nd98a1740f5ad["Sealed single-group action suggestion / 10 minutes"]
  n74e21680eac7["Approve immutable stored plan"]
  n4a2dbd905287["Approval permission / epoch / role checks"]
  n1624d25a752a["Private business admission (no test window / native_debug)"]
  n20f65c28671b["Server session / scope revalidation"]
  n7ace20631b72["Existing Chili owner / string IDs / OpenIM recall"]
  n9505cacb7c71["Commit config / execution / audit"]
  n4ea5ee68fea0["Shared AI gateway / fair bounded queue"]
  n3492ad65d05a["Single-use control grant / live scope check"]
  n5a2a0ed9aa3a["Fixed private heatbot client (generation gated)"]
  n79a5478768d2["Separate website approval POST"]
  n88ee5f1a825a["Durable platform identity / shared WSL nonce allocator"]
  n9372c470eead["Assistant scoped gateway adapter"]
  nccd626a67aec["Shared durable native operation"]
  nc5ccec15fdd2["Current scope / epoch / package / role / final fence"]
  ne75d83d83e27["Read-only platform assistant (one call / 45 seconds)"]
  n7210bde489e8["Separate platform-assistant month budget / current settings"]
  nad79f7b77d1a["Fixed aggregate platform_health projection"]
  n35e2c793b979["Superadmin website session / independent platform conversation"]
  n12df7f0ee89f["Suggestion-only plan"]
  na8b771920b83["Authorized safe query"]
  n6df24c372c04["Shared gateway budget boundary"]
  nd92c6a81b2ff["Eino ChatModelAgent (3 calls / 6 sequential tools)"]
  n157dca92e425["Shared configuration transaction"]
  n6868e83de35c["Idempotent settlement or frozen unknown usage"]
  nf9d35d43770d["Allowlisted Host.Call"]
  n630ba09448af["Sealed provider / pinned rates"]
  ne8d44050873d["Atomic period wallet and sub-limit reservation"]
  n65e8bf7fd279["Existing WSL owner / text or scoped recall"]
  n5bc11618c1ea -->|"only after approved-plan commit"| n9b43c2b9988b
  n5bc11618c1ea -->|"recheck live approval and group permissions"| n4a2dbd905287
  n9b43c2b9988b -->|"fixed native settings adapter"| n1624d25a752a
  n9b43c2b9988b -->|"fixed trusted port_ model cannot invoke it"| n7ace20631b72
  n9b43c2b9988b -->|"fixed trusted port_ model cannot invoke it"| n65e8bf7fd279
  n74e21680eac7 -->|"recheck current authorization"| n4a2dbd905287
  n74e21680eac7 -->|"CAS / content-only validation"| n157dca92e425
  n1624d25a752a -->|"shared persistence_ separate admission"| nccd626a67aec
  n4ea5ee68fea0 -->|"reserve and submit revalidate the consumer"| n7210bde489e8
  n4ea5ee68fea0 -->|"conservative finalization"| n6868e83de35c
  n4ea5ee68fea0 -->|"one paid submission_ no automatic retries"| n630ba09448af
  n4ea5ee68fea0 -->|"short transaction_ no model wait"| ne8d44050873d
  n3492ad65d05a -->|"resolve tenant / group from stored grant"| n4ea5ee68fea0
  n5a2a0ed9aa3a -->|"fixed Compose endpoint and token"| n3492ad65d05a
  n79a5478768d2 -->|"separate website confirmation"| n5bc11618c1ea
  n79a5478768d2 -->|"explicit approval handler"| n74e21680eac7
  n88ee5f1a825a -->|"current connection identity and immutable target"| nc5ccec15fdd2
  n9372c470eead -->|"trusted AssistantHost identity only"| n4ea5ee68fea0
  nccd626a67aec -->|"reserve identity in intent transaction"| n88ee5f1a825a
  nccd626a67aec -->|"repeat control authorization before submission"| nc5ccec15fdd2
  ne75d83d83e27 -->|"fixed purpose / no tenant wallet / no native execution"| n4ea5ee68fea0
  ne75d83d83e27 -->|"counts and gates only_ no callable tools"| nad79f7b77d1a
  n35e2c793b979 -->|"separate admin endpoint_ no management session IDs"| ne75d83d83e27
  nd92c6a81b2ff -->|"revalidate each round / delivery"| n20f65c28671b
  nd92c6a81b2ff -->|"text / tool proposals only"| n9372c470eead
  nd92c6a81b2ff -->|"reserve before model request"| n6df24c372c04
  nd92c6a81b2ff -->|"bounded approved tool"| nf9d35d43770d
  n157dca92e425 -->|"configuration saved_ effect unverified"| n9505cacb7c71
  nf9d35d43770d -->|"suggestion only_ no transport capability"| nd98a1740f5ad
  nf9d35d43770d -->|"revalidate before tool"| n20f65c28671b
  nf9d35d43770d -->|"no configuration mutation"| n12df7f0ee89f
  nf9d35d43770d -->|"safe projection"| na8b771920b83
```
