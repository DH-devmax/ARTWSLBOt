# Backend architecture

Generated from code evidence; not production acceptance.

```mermaid
%% Generated from source evidence. Do not hand edit.
%% Import / declaration edges are not proof of runtime activation.
flowchart LR
  n558456ae4637["cmd/heatbot"]
  ne63393abce37["cmd/wsl"]
  nb1e3735ba7b9["cmd/wslctl"]
  n21dc7a36c455["internal/apiclient"]
  ne97322f31c4c["internal/control"]
  n439f5e85ca1b["internal/database"]
  nc77ef6feb62c["internal/gateway"]
  n7e589f1f8a4a["internal/heatbot"]
  n1e51f7ae8228["internal/httpapi"]
  n1ab7003e42f1["internal/monitor"]
  nfb6349ce1fd1["internal/nativeauth"]
  na6745c306747["internal/nativewire"]
  n710ad8df84a9["internal/security"]
  n558456ae4637 -->|"imports"| n7e589f1f8a4a
  ne63393abce37 -->|"imports"| n439f5e85ca1b
  ne63393abce37 -->|"imports"| n1e51f7ae8228
  ne63393abce37 -->|"imports"| n710ad8df84a9
  nb1e3735ba7b9 -->|"imports"| n21dc7a36c455
  ne97322f31c4c -->|"imports"| n710ad8df84a9
  nc77ef6feb62c -->|"imports"| nfb6349ce1fd1
  nc77ef6feb62c -->|"imports"| na6745c306747
  nc77ef6feb62c -->|"imports"| n710ad8df84a9
  n1e51f7ae8228 -->|"imports"| ne97322f31c4c
  n1e51f7ae8228 -->|"imports"| n439f5e85ca1b
  n1e51f7ae8228 -->|"imports"| nc77ef6feb62c
  n1e51f7ae8228 -->|"imports"| n1ab7003e42f1
  n1e51f7ae8228 -->|"imports"| nfb6349ce1fd1
  n1e51f7ae8228 -->|"imports"| na6745c306747
  n1e51f7ae8228 -->|"imports"| n710ad8df84a9
  nfb6349ce1fd1 -->|"imports"| na6745c306747
  nfb6349ce1fd1 -->|"imports"| n710ad8df84a9
```
