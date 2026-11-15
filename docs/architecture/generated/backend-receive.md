# Backend-Receive architecture

Generated from code evidence; not production acceptance.

```mermaid
%% Generated from source evidence. Do not hand edit.
%% Import / declaration edges are not proof of runtime activation.
flowchart LR
  n64a37929fb11["Fenced ACK chunk"]
  n46a913aef7a3["Sequence commit only (not network ACK)"]
  n25757315eb44["Scoped LajiaoLiao text projection"]
  n555198e49ed0["Bounded contiguous sequence pull"]
  n4f6eb32b845c["Push commit without cursor advance"]
  n6ad5e0f9ce9a["Fenced bounded zero-sequence push"]
  n82b09ab38f45["LajiaoLiao push hint / recovery sweep"]
  n62dbbd16239f["Fenced sealed events and cursor transaction"]
  n9505cacb7c71["Durable batch commit"]
  n713a1417c769["Application decode / persisted result"]
  nc9fbd9152b7f["Independent bounded decoder workers"]
  n1e22560cee2c["Sealed raw event insert"]
  n6cd6f41455d7["Nonblocking decoder notification"]
  n1c61425b1ba9["Persist frames / fenced transaction"]
  n95100519be03["Receive consumer / bounded batch"]
  n10fd874b68da["ACK socket write"]
  n64a37929fb11 -->|"ACK written_ not business completion"| n10fd874b68da
  n46a913aef7a3 -->|"nonblocking shared decoder wake"| n6cd6f41455d7
  n555198e49ed0 -->|"persist verified contiguous range"| n62dbbd16239f
  n4f6eb32b845c -->|"wake after confirmed push commit"| n6cd6f41455d7
  n6ad5e0f9ce9a -->|"idempotent sealed receive commit"| n4f6eb32b845c
  n82b09ab38f45 -->|"single existing account owner"| n555198e49ed0
  n82b09ab38f45 -->|"same owner_ stable native identity"| n6ad5e0f9ce9a
  n62dbbd16239f -->|"raw events and cursor commit together"| n46a913aef7a3
  n9505cacb7c71 -->|"after successful commit"| n64a37929fb11
  n9505cacb7c71 -->|"after successful commit"| n6cd6f41455d7
  nc9fbd9152b7f -->|"platform dispatch in same worker pool"| n25757315eb44
  nc9fbd9152b7f -->|"worker process call"| n713a1417c769
  n1e22560cee2c -->|"commit before fan-out"| n9505cacb7c71
  n6cd6f41455d7 -->|"wake wiring_ independent of ACK"| nc9fbd9152b7f
  n1c61425b1ba9 -->|"store raw event"| n1e22560cee2c
  n95100519be03 -->|"bounded batch call"| n1c61425b1ba9
```
