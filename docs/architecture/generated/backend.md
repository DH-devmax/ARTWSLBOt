# Backend architecture

Generated from code evidence; not production acceptance.

```mermaid
%% Generated from source evidence. Do not hand edit.
%% Import / declaration edges are not proof of runtime activation.
flowchart LR
  n558456ae4637["cmd/heatbot"]
  ne63393abce37["cmd/wsl"]
  nb1e3735ba7b9["cmd/wslctl"]
  nd67e6ca6e5fb["internal/aigateway"]
  n21dc7a36c455["internal/apiclient"]
  n2e1435d52cdd["internal/assistant"]
  n1a7368cd1fbd["internal/assistantadapter"]
  ne97322f31c4c["internal/control"]
  n439f5e85ca1b["internal/database"]
  ne2d94d3013e7["internal/features"]
  nc77ef6feb62c["internal/gateway"]
  n7e589f1f8a4a["internal/heatbot"]
  n1e51f7ae8228["internal/httpapi"]
  n48aedc05e598["internal/lajiaoliao"]
  n9aa322b91af0["internal/metricsproc"]
  n1ab7003e42f1["internal/monitor"]
  nfb6349ce1fd1["internal/nativeauth"]
  na6745c306747["internal/nativewire"]
  n8a0ad28d6722["internal/platform"]
  n0c3062428a9c["internal/scopecache"]
  n710ad8df84a9["internal/security"]
  n80c81db124d4["internal/webcaptcha"]
  n558456ae4637 -->|"imports"| n7e589f1f8a4a
  n558456ae4637 -->|"imports"| n9aa322b91af0
  n558456ae4637 -->|"imports"| n1ab7003e42f1
  ne63393abce37 -->|"imports"| ne97322f31c4c
  ne63393abce37 -->|"imports"| n439f5e85ca1b
  ne63393abce37 -->|"imports"| n1e51f7ae8228
  ne63393abce37 -->|"imports"| n9aa322b91af0
  ne63393abce37 -->|"imports"| n1ab7003e42f1
  ne63393abce37 -->|"imports"| n710ad8df84a9
  nb1e3735ba7b9 -->|"imports"| n21dc7a36c455
  n1a7368cd1fbd -->|"imports"| n2e1435d52cdd
  ne97322f31c4c -->|"imports"| nd67e6ca6e5fb
  ne97322f31c4c -->|"imports"| n2e1435d52cdd
  ne97322f31c4c -->|"imports"| n439f5e85ca1b
  ne97322f31c4c -->|"imports"| ne2d94d3013e7
  ne97322f31c4c -->|"imports"| n7e589f1f8a4a
  ne97322f31c4c -->|"imports"| n48aedc05e598
  ne97322f31c4c -->|"imports"| nfb6349ce1fd1
  ne97322f31c4c -->|"imports"| na6745c306747
  ne97322f31c4c -->|"imports"| n8a0ad28d6722
  ne97322f31c4c -->|"imports"| n0c3062428a9c
  ne97322f31c4c -->|"imports"| n710ad8df84a9
  nc77ef6feb62c -->|"imports"| n48aedc05e598
  nc77ef6feb62c -->|"imports"| nfb6349ce1fd1
  nc77ef6feb62c -->|"imports"| na6745c306747
  nc77ef6feb62c -->|"imports"| n8a0ad28d6722
  nc77ef6feb62c -->|"imports"| n0c3062428a9c
  nc77ef6feb62c -->|"imports"| n710ad8df84a9
  n1e51f7ae8228 -->|"imports"| nd67e6ca6e5fb
  n1e51f7ae8228 -->|"imports"| n2e1435d52cdd
  n1e51f7ae8228 -->|"imports"| n1a7368cd1fbd
  n1e51f7ae8228 -->|"imports"| ne97322f31c4c
  n1e51f7ae8228 -->|"imports"| n439f5e85ca1b
  n1e51f7ae8228 -->|"imports"| ne2d94d3013e7
  n1e51f7ae8228 -->|"imports"| nc77ef6feb62c
  n1e51f7ae8228 -->|"imports"| n7e589f1f8a4a
  n1e51f7ae8228 -->|"imports"| n48aedc05e598
  n1e51f7ae8228 -->|"imports"| n1ab7003e42f1
  n1e51f7ae8228 -->|"imports"| nfb6349ce1fd1
  n1e51f7ae8228 -->|"imports"| na6745c306747
  n1e51f7ae8228 -->|"imports"| n0c3062428a9c
  n1e51f7ae8228 -->|"imports"| n710ad8df84a9
  n1e51f7ae8228 -->|"imports"| n80c81db124d4
  n48aedc05e598 -->|"imports"| nfb6349ce1fd1
  n48aedc05e598 -->|"imports"| na6745c306747
  n9aa322b91af0 -->|"imports"| n1ab7003e42f1
  nfb6349ce1fd1 -->|"imports"| na6745c306747
  nfb6349ce1fd1 -->|"imports"| n710ad8df84a9
```
